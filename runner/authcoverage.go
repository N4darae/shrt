package runner

import (
	"cmp"
	"fmt"
	"strings"

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

func authHeader(spec *transport.AuthSpec) string {
	header, _ := (&transport.AuthProfile{Spec: *spec}).HeaderScheme()
	return header
}

func (r *Runner) checkHandWrittenAuth(c *chain.Chain) error {
	if len(r.Auth) == 0 {
		return nil
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
			if name, ok := headerIn(step.Headers, "Authorization"); ok {
				return fmt.Errorf("step %q (step %d) writes %q by hand with skip_auth, so nothing was sent: that pins one "+
					"principal into one step with no refresh, and chain lint rejects it. Declare the principal as a "+
					"profile in .shrt/config.yaml and name it with auth: <profile>", step.ID, i+1, name)
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
		if name, ok := headerIn(step.Headers, headers[profile]); ok {
			return fmt.Errorf("step %q (step %d) writes %q by hand, and auth profile %q covers this call, so nothing was "+
				"sent: the auth middleware would overwrite the header with %q's token and the step would run as %q's "+
				"principal while reading as another's, which chain lint rejects. To call as a different principal, "+
				"declare it as a profile in .shrt/config.yaml and name it with auth: <profile>",
				step.ID, i+1, name, profileLabel(profile), profile, profile)
		}
	}
	return nil
}

func headerIn(headers map[string]string, want string) (string, bool) {
	for name := range headers {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(want)) {
			return name, true
		}
	}
	return "", false
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
