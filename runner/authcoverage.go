package runner

import (
	"cmp"
	"fmt"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/transport"
)

func AuthCoverage(cfg *config.Config, cat *catalog.Catalog) (func(*chain.Step) (string, string, bool), error) {
	if cfg == nil || cfg.Auth == nil {
		return func(*chain.Step) (string, string, bool) { return "", "", false }, nil
	}
	offline := *cfg
	offline.Root = ""
	router, err := authRouter(&offline, cat, nil, &Deps{})
	if err != nil {
		return nil, err
	}
	return func(s *chain.Step) (string, string, bool) {
		p, ok := routedProfile(router, cat, s)
		if !ok {
			return "", "", false
		}
		header, _ := p.HeaderScheme()
		return p.Name, header, true
	}, nil
}

func AuthEnv(cfg *config.Config) func(profile string) []string {
	return func(profile string) []string {
		if cfg == nil || cfg.Auth == nil {
			return nil
		}
		auth := cfg.AuthProfiles()[profile]
		if auth == nil {
			return nil
		}
		return chain.AuthBodyEnvNames(auth.Body)
	}
}

func routedProfile(router *transport.AuthRouter, cat *catalog.Catalog, s *chain.Step) (*transport.AuthProfile, bool) {
	m, err := cat.Lookup(s.Call)
	if err != nil {
		return nil, false
	}
	p, err := router.Resolve(&transport.Call{
		Procedure: m.Procedure(),
		Meta:      map[string]any{"skip_auth": s.SkipAuth, "auth": s.Auth},
	})
	if err != nil || p == nil || p.Source == nil {
		return nil, false
	}
	return p, true
}

func (r *Runner) handAuthProblems(c *chain.Chain) []string {
	out := []string{}
	if len(r.Auth) == 0 {
		return out
	}
	headers := map[string]string{}
	for _, b := range r.Auth {
		if b != nil {
			headers[b.Profile] = cmp.Or(b.Header, "Authorization")
		}
	}
	for i, step := range c.Steps {
		if step == nil {
			continue
		}
		if step.SkipAuth {
			if name, ok := chain.HeaderNamed(step.Headers, "Authorization"); ok {
				out = append(out, stepProblemf(step, i, "%s", chain.HandAuthSkipped(name)))
			}
			continue
		}
		if r.AuthRoute == nil {
			continue
		}
		profile, routed := r.AuthRoute(step)
		if !routed {
			continue
		}
		if name, ok := chain.HeaderNamed(step.Headers, headers[profile]); ok {
			out = append(out, stepProblemf(step, i, "%s", chain.HandAuthCovered(name, fmt.Sprintf("%q", profileLabel(profile)))))
		}
	}
	return out
}

func routeOf(router *transport.AuthRouter, cat *catalog.Catalog) func(*chain.Step) (string, bool) {
	return func(s *chain.Step) (string, bool) {
		if s.Auth == transport.InvalidTokenProfile {
			return "", false
		}
		p, ok := routedProfile(router, cat, s)
		if !ok {
			return "", false
		}
		return p.Name, true
	}
}
