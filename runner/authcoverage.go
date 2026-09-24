package runner

import (
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
		m, err := cat.Lookup(s.Call)
		if err != nil {
			return "", "", false
		}
		p, err := router.Resolve(&transport.Call{
			Procedure: m.Procedure(),
			Meta:      map[string]any{"skip_auth": s.SkipAuth, "auth": s.Auth},
		})
		if err != nil || p == nil || p.Source == nil {
			return "", "", false
		}
		header, _ := p.HeaderScheme()
		return p.Name, header, true
	}, nil
}
