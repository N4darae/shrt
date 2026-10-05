package contract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var (
	denialReason = lazyRegexp(`(?i)permission|forbidden|denied|notallowed|unauthori[sz]ed|role|privilege|notadmin`)
	denialWhen   = lazyRegexp(`(?i)(?:caller|user|principal|token|account)[^.;]*\b(?:role|permission|privilege|admin)|does not (?:hold|have)[^.;]*\b(?:role|permission)|\bnot allowed\b|\bforbidden\b`)
	unauthWord   = lazyRegexp(`(?i)unauthenticated|missing token|no token|invalid token|expired token|not logged in`)
	profileChars = lazyRegexp(`[^A-Za-z0-9_]+`)
)

const invalidProfile = "invalid"

func refusedWith(m *catalog.Method, f *Failure) ([]chain.Expectation, string) {
	if f == nil {
		return []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}, ""
	}
	return refusalFor(m, *f, true), " with " + f.Label()
}

func refusalFor(m *catalog.Method, f Failure, absentCarrier bool) []chain.Expectation {
	if f.ConnectCode != "" && f.Code == 0 {
		return []chain.Expectation{{Path: "transport.code", Equals: f.ConnectCode}}
	}
	out, _ := refusalExpectations(m, f, absentCarrier)
	return out
}

func denialFailure(lib *Library, rpc string) (Failure, bool) {
	failures := lib.AllFailures(rpc)
	for _, f := range failures {
		if !isUnauthenticated(f) && (denialReason().MatchString(f.Reason) || f.ConnectCode == "permission_denied") {
			return f, true
		}
	}
	for _, f := range failures {
		if !isUnauthenticated(f) && denialWhen().MatchString(f.When) {
			return f, true
		}
	}
	return Failure{}, false
}

func isUnauthenticated(f Failure) bool {
	return f.ConnectCode == "unauthenticated" || unauthWord().MatchString(f.Reason) || strings.EqualFold(f.Reason, "Unauthenticated")
}

func holdsRole(profile string, roles []string) bool {
	return slices.ContainsFunc(roles, func(r string) bool { return namecase.Fold(profile) == namecase.Fold(r) })
}

func (p *Plan) isLogin(call string) bool {
	full := canonicalCall(p.cat, call)
	return slices.ContainsFunc(p.opts.Logins, func(l string) bool { return canonicalCall(p.cat, l) == full })
}

func (p *Plan) probeDenials(lib *Library, isTarget func(*chain.Step) bool) {
	if !p.opts.Auth {
		return
	}
	tokenDone := map[string]bool{}
	for st, m := range p.withMethods(p.targets(isTarget, p.loginStep, skipsAuth)) {
		group := []*chain.Step{}
		said := []string{}
		if c, ok := lib.Get(st.Call); ok && !m.ServerStreaming && len(c.RequiresRole) > 0 && !c.DeclaresNoRole() && !IsTodo(strings.Join(c.RequiresRole, " ")) {
			f, found := denialFailure(lib, st.Call)
			for _, prof := range p.opts.Profiles {
				if prof == st.Auth || prof == invalidProfile || prof == "default" || holdsRole(prof, c.RequiresRole) {
					continue
				}
				probe := p.probeCopy(lib, st, "as_"+profileSuffix(prof))
				probe.Auth = prof
				roles := strings.Join(c.RequiresRole, " or ")
				if found {
					probe.Expect = refusalFor(m, f, true)
					probe.Description = fmt.Sprintf("as %s, who does not hold %s, refused with %s.", prof, roles, f.Label())
				} else {
					probe.Expect = []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}
					probe.Description = fmt.Sprintf("as %s, who does not hold %s, refused.", prof, roles)
					p.note("step %s: its contract requires %s but declares no failure for a caller without it (a reason such "+
						"as PermissionDenied, or a when: naming the role), so %s asserts only that it was refused: declare it and plan again",
						st.ID, roles, probe.ID)
				}
				group = append(group, probe)
				said = append(said, fmt.Sprintf("%s calls it as profile %s, assumed not to hold %s (its name is not the role's)", probe.ID, prof, roles))
			}
		}
		if rpc := canonicalCall(p.cat, st.Call); !tokenDone[rpc] {
			tokenDone[rpc] = true
			expect := []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}
			how := "Connect unauthenticated, which no failure in its contract declares (declare one with connect_code: unauthenticated in the domain-level failures:, or once with scope: all in any overlay to share it with every domain)"
			failures := lib.AllFailures(st.Call)
			if i := slices.IndexFunc(failures, isUnauthenticated); i >= 0 {
				expect = refusalFor(m, failures[i], true)
				how = failures[i].Label()
			}
			without := p.probeCopy(lib, st, "without_token")
			without.SkipAuth, without.Auth = true, ""
			without.Expect = append([]chain.Expectation{}, expect...)
			without.Description = "with no token at all, refused before the handler runs."
			bad := p.probeCopy(lib, st, "with_bad_token")
			bad.Auth = invalidProfile
			bad.Expect = append([]chain.Expectation{}, expect...)
			bad.Description = "with a token the backend never issued, refused before the handler runs."
			group = append(group, without, bad)
			said = append(said, fmt.Sprintf("%s and %s send no token and a token never issued, expecting %s; each target rpc "+
				"gets one pair, and 'shrt contract status -gaps' lists rpcs no chain probes this way", without.ID, bad.ID, how))
		}
		if len(group) == 0 {
			continue
		}
		if chain.IsReadOnlyCall(st.Call) || m.ServerStreaming {
			p.Chain.Steps = append(p.Chain.Steps, group...)
			p.note("step %s: %s", st.ID, strings.Join(said, "; "))
			continue
		}
		p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, group, st.ID+"_denied")...)
		p.note("step %s: %s; each is a write that must change nothing, so the reads around them assert the state it touches unchanged",
			st.ID, strings.Join(said, "; "))
	}
}

func (p *Plan) probeCopy(lib *Library, st *chain.Step, suffix string) *chain.Step {
	probe := probeStep(st, p.freeStepID(st.ID+"_"+suffix))
	if !chain.IsReadOnlyCall(st.Call) && !p.streams(st) {
		p.freshen(lib, probe)
	}
	return probe
}
