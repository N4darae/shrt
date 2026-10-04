package contract

import (
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

type ProbeGap struct {
	RPC     string `json:"rpc"`
	Kind    string `json:"kind"`
	Roles   string `json:"roles,omitempty"`
	Profile string `json:"profile,omitempty"`
}

func AuthProbeGaps(chains []*chain.Chain, lib *Library, cat *catalog.Catalog, opts PlanOptions) []ProbeGap {
	if !opts.Auth {
		return nil
	}
	called, tokenProbed := map[string]bool{}, map[string]bool{}
	asProfile := map[string]map[string]bool{}
	for _, c := range chains {
		if c == nil {
			continue
		}
		for _, s := range c.Steps {
			if s == nil {
				continue
			}
			rpc := canonicalCall(cat, s.Call)
			called[rpc] = true
			if s.SkipAuth || s.Auth == invalidProfile {
				tokenProbed[rpc] = true
			}
			if s.Auth != "" && s.Auth != invalidProfile {
				if asProfile[rpc] == nil {
					asProfile[rpc] = map[string]bool{}
				}
				asProfile[rpc][s.Auth] = true
			}
		}
	}
	p := &Plan{cat: cat, opts: opts}
	out := []ProbeGap{}
	for _, m := range cat.Methods() {
		if m.StreamRefusal() != "" || p.isLogin(m.FullName) || !called[m.FullName] {
			continue
		}
		if c, ok := lib.Get(m.FullName); ok && !m.ServerStreaming && !IsTodo(strings.Join(c.RequiresRole, " ")) {
			for _, prof := range opts.Profiles {
				switch {
				case prof == "default" || asProfile[m.FullName][prof]:
				case !openToEveryRole(c) && !holdsRole(prof, c.RequiresRole):
					out = append(out, ProbeGap{RPC: m.FullName, Kind: "role", Roles: strings.Join(c.RequiresRole, " or "), Profile: prof})
				case openToEveryRole(c) && prof != invalidProfile:
					out = append(out, ProbeGap{RPC: m.FullName, Kind: "parity", Profile: prof})
				}
			}
		}
		if !tokenProbed[m.FullName] {
			out = append(out, ProbeGap{RPC: m.FullName, Kind: "token"})
		}
	}
	return out
}
