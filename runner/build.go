package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/transport"
)

type Deps struct {
	Catalog  *catalog.Catalog
	Client   *transport.Client
	Auth     *transport.LoginTokenSource
	Sources  map[string]*transport.LoginTokenSource
	Bindings AuthBindings
	Profiles []string
}

func Build(ctx context.Context, cfg *config.Config, cat *catalog.Catalog, obs func(transport.Event)) (*Deps, error) {
	timeout := 30 * time.Second
	if cfg.Target.Timeout != "" {
		d, err := time.ParseDuration(cfg.Target.Timeout)
		if err != nil {
			return nil, fmt.Errorf("target.timeout: %w", err)
		}
		timeout = d
	}

	base := transport.New(transport.Options{
		BaseURL:      cfg.Target.BaseURL,
		HostOverride: cfg.Target.HostOverride,
		Timeout:      timeout,
		Headers:      cfg.Target.Headers,
	})

	deps := &Deps{Catalog: cat, Client: base}
	mws := []transport.Middleware{}
	if obs != nil {
		mws = append(mws, transport.WithObserver(obs))
	}

	if cfg.Auth != nil {
		router, err := authRouter(cfg, cat, base.Raw, deps)
		if err != nil {
			return nil, err
		}
		mws = append(mws, transport.WithAuthRouter(*router))
	}

	deps.Client = transport.New(transport.Options{
		BaseURL:      cfg.Target.BaseURL,
		HostOverride: cfg.Target.HostOverride,
		Timeout:      timeout,
		Headers:      cfg.Target.Headers,
		Middlewares:  mws,
	})
	return deps, nil
}

func authRouter(cfg *config.Config, cat *catalog.Catalog, invoke transport.Handler, deps *Deps) (*transport.AuthRouter, error) {
	profiles := cfg.AuthProfiles()
	envelopePath := strings.TrimSpace(cfg.Conventions.EnvelopePath)
	if envelopePath == "" {
		envelopePath = chain.DefaultEnvelopePath
	}
	router := &transport.AuthRouter{
		Default:  config.DefaultAuthProfile,
		Envelope: transport.EnvelopeCodeReader(envelopePath),
	}
	deps.Sources = map[string]*transport.LoginTokenSource{}
	deps.Profiles = cfg.AuthProfileNames()

	logins := []string{}
	for _, name := range deps.Profiles {
		auth := profiles[name]
		spec, err := authSpec(name, auth, cat)
		if err != nil {
			return nil, err
		}
		src := transport.NewLoginTokenSource(*spec, invoke)
		if cfg.Root != "" && os.Getenv("SHRT_TOKEN_CACHE") != "0" {
			src.UseCache(filepath.Join(cfg.Root, config.DirName, config.TokensFile), name)
		}
		deps.Sources[name] = src
		if name == config.DefaultAuthProfile {
			deps.Auth = src
		}
		deps.Bindings = append(deps.Bindings, &AuthBinding{
			Profile:     name,
			Procedure:   spec.Procedure,
			TokenPath:   auth.TokenPath,
			ExpiresPath: auth.ExpiresPath,
			Body:        spec.Body,
			Sink:        src,
		})
		owns, err := matcher(auth.Calls, cat)
		if err != nil {
			return nil, fmt.Errorf("auth profile %q: calls: %w", name, err)
		}
		router.Profiles = append(router.Profiles, &transport.AuthProfile{
			Name: name, Spec: *spec, Source: src, Owns: owns,
		})
		logins = append(logins, spec.Procedure)
	}

	skip, err := matcher(cfg.Auth.SkipCalls, cat)
	if err != nil {
		return nil, fmt.Errorf("auth.skip_calls: %w", err)
	}
	router.Skip = func(procedure string) bool {
		for _, login := range logins {
			if login == procedure {
				return true
			}
		}
		return skip != nil && skip(procedure)
	}
	return router, nil
}

func authSpec(profile string, auth *config.Auth, cat *catalog.Catalog) (*transport.AuthSpec, error) {
	where := "auth"
	if profile != config.DefaultAuthProfile {
		where = "auth.profiles." + profile
	}
	m, err := cat.Lookup(auth.Call)
	if err != nil {
		return nil, fmt.Errorf("%s.call: %w", where, err)
	}
	body := orEmpty(auth.Body)
	probe, err := json.Marshal(chain.Probe(body, catalog.DescribeMessage(m.Input())))
	if err != nil {
		return nil, fmt.Errorf("%s.body: %w", where, err)
	}
	if err := cat.ValidateInput(m, probe); err != nil {
		return nil, fmt.Errorf("%s.body: %w", where, err)
	}
	header, scheme := auth.HeaderScheme()
	return &transport.AuthSpec{
		Procedure: m.Procedure(),
		Canonicalize: func(raw []byte) ([]byte, error) {
			return cat.Canonicalize(m.Output(), raw)
		},
		Body: func() ([]byte, error) {
			resolved, err := chain.AuthBodyScope().ResolveValue(body)
			if err != nil {
				return nil, err
			}
			return json.Marshal(resolved)
		},
		TokenPath:   auth.TokenPath,
		ExpiresPath: auth.ExpiresPath,
		Header:      header,
		Scheme:      scheme,
		Leeway:      time.Duration(auth.LeewaySecs) * time.Second,
	}, nil
}

func matcher(patterns []string, cat *catalog.Catalog) (func(string) bool, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	exact := map[string]bool{}
	globs := []string{}
	for _, ref := range patterns {
		if strings.Contains(ref, "*") {
			globs = append(globs, strings.TrimPrefix(ref, "/"))
			continue
		}
		if m, err := cat.Lookup(ref); err == nil {
			exact[m.Procedure()] = true
			continue
		}
		if !strings.Contains(ref, "/") {
			return nil, fmt.Errorf("%q names no rpc and is not a procedure path or a glob", ref)
		}
		exact["/"+strings.TrimPrefix(ref, "/")] = true
	}
	return func(p string) bool {
		if exact[p] {
			return true
		}
		trimmed := strings.TrimPrefix(p, "/")
		for _, g := range globs {
			if globMatch(g, trimmed) {
				return true
			}
		}
		return false
	}, nil
}

func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}

func NewFromConfig(ctx context.Context, cfg *config.Config, cat *catalog.Catalog) (*Runner, Options, error) {
	chain.ApplyConventions(cfg.Conventions.ReadOnlyPrefixes, cfg.Conventions.EnvelopePath, cfg.Conventions.EnvelopeOK)
	chain.ApplyItemEnvelope(cfg.Conventions.ItemEnvelopePath)
	chain.ApplyCodeFields(cfg.Conventions.CodeFields)
	if err := chain.ValidateItemEnvelope(cat); err != nil {
		return nil, Options{}, err
	}
	deps, err := Build(ctx, cfg, cat, nil)
	if err != nil {
		return nil, Options{}, err
	}
	r := &Runner{
		Catalog:        deps.Catalog,
		Client:         deps.Client,
		ValidateInput:  true,
		ValidateOutput: cfg.Conventions.ValidateOutput,
		Auth:           deps.Bindings,
		BuildHeader:    strings.TrimSpace(cfg.Target.BuildHeader),
	}
	return r, Options{Volatile: cfg.Volatile, Redact: cfg.Redact}, nil
}
