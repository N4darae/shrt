package contract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
)

var (
	badSecretWhen = lazyRegexp(`(?i)\bpassword|\bpassphrase|\bcredential|\bsecret\b|\bdoes not match\b|\bwrong\b|\bincorrect\b`)
	unknownUser   = lazyRegexp(`(?i)\b(?:unknown|unregistered|non-?existent|no such|not (?:in|found|known|registered)|(?:does not|doesn't|do not|don't) exist|no (?:account|user|login|member|one)s?\b[^.;]*?\b(?:has|have|with|named|called|matches|match|uses|use|by|for)\b|no (?:account|user)s? (?:exists?|found)|(?:account|user|username|login)\b[^.;]*?\b(?:missing|absent|not on file))`)
	unknownReason = lazyRegexp(`(?i)(?:unknown|nosuch|no)(?:user|account|login)|(?:user|account|login)(?:notfound|unknown|missing)`)
	roleWord      = lazyRegexp(`\b[A-Z][A-Z_]{2,}\b`)
)

func (p *Plan) probeLogin(lib *Library, isTarget func(*chain.Step) bool) {
	for st, m := range p.withMethods(p.targets(isTarget, func(st *chain.Step) bool { return !p.isLogin(st.Call) })) {
		c, _ := lib.Get(m.FullName)
		said := []string{}
		f, declared := credentialFailure(lib, m.FullName)
		if declared {
			said = append(said, p.badLogins(st, m, f)...)
		}
		if padded := p.paddedSecretLogin(st, m, f, declared); padded != "" {
			said = append(said, padded)
		}
		said = append(said, p.loginRoles(lib, st, m, c)...)
		if len(said) > 0 {
			p.note("step %s: the login's own contract is probed apart from the config's auth, which only ever logs in "+
				"successfully: %s", st.ID, strings.Join(said, "; "))
		}
	}
}

func credentialFailure(lib *Library, rpc string) (Failure, bool) {
	for _, f := range lib.AllFailures(rpc) {
		if isUnauthenticated(f) || f.Unreachable != "" || f.ConnectCode == invalidArgCode {
			continue
		}
		if badSecretWhen().MatchString(f.When) || badSecretWhen().MatchString(f.Reason) || strings.Contains(strings.ToLower(f.Reason), "credential") {
			return f, true
		}
	}
	return Failure{}, false
}

func keyWhere(body map[string]any, match func(lower string) bool) string {
	for _, k := range chain.SortedKeys(body) {
		if match(strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func secretKey(body map[string]any) string {
	return keyWhere(body, func(low string) bool { return strings.Contains(low, "pass") || strings.Contains(low, "secret") })
}

func (p *Plan) badLogins(st *chain.Step, m *catalog.Method, f Failure) []string {
	out := []string{}
	add := func(suffix, key, value, what string) {
		probe := probeStep(st, p.freeStepID(st.ID+"_"+suffix))
		probe.Body[key] = value
		probe.Expect = refusalFor(m, f, true)
		probe.Description = fmt.Sprintf("%s: refused with %s, as the contract declares.", what, f.Label())
		p.Chain.Steps = append(p.Chain.Steps, probe)
		out = append(out, fmt.Sprintf("%s (%s) expects %s", probe.ID, what, f.Label()))
	}
	if key := secretKey(st.Body); key != "" {
		cur, _ := st.Body[key].(string)
		add("bad_password", key, cur+"-not-it", "the right account with a password that is not its own")
	}
	if key := keyWhere(st.Body, func(low string) bool {
		return strings.Contains(low, "user") || strings.Contains(low, "login") || strings.Contains(low, "email") || low == "name"
	}); key != "" {
		if uf, ok := p.unknownUserFailure(m.FullName, f); ok {
			f = uf
			add("unknown_user", key, "no-such-user-shrt", "an account name no one has")
		} else {
			p.gap("step %s: %s's when: does not read as an unknown account, so no %s_unknown_user probe was planned: "+
				"say \"the username is unknown\" in it, or declare the failure an unknown account gets", st.ID, f.Label(), st.ID)
		}
	}
	return out
}

func (p *Plan) secretField(body map[string]any) string {
	for _, k := range chain.SortedKeys(body) {
		for _, pat := range p.opts.Redact {
			if pathmask.Match(pat, k) {
				return k
			}
		}
	}
	return secretKey(body)
}

func (p *Plan) paddedSecretLogin(st *chain.Step, m *catalog.Method, f Failure, declared bool) string {
	key := p.secretField(st.Body)
	cur, ok := st.Body[key].(string)
	if key == "" || !ok || cur == "" {
		return ""
	}
	probe := probeStep(st, p.freeStepID(st.ID+"_padded_"+strings.ToLower(key)))
	probe.Body[key] = " " + cur + " "
	refused := "refused with " + f.Label() + ", as a wrong one is"
	if declared {
		probe.Expect = refusalFor(m, f, true)
	} else if CarriesEnvelope(m) {
		probe.Expect = []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}
		refused = "refused"
	} else {
		probe.Expect = []chain.Expectation{{Path: "transport.code", NotEqual: "ok"}}
		refused = "refused"
	}
	probe.Description = fmt.Sprintf("the working login with %s padded by a space on each side: %s, since a secret is compared exactly.", key, refused)
	p.Chain.Steps = append(p.Chain.Steps, probe)
	return fmt.Sprintf("%s sends %s with a leading and a trailing space and expects it %s", probe.ID, key, refused)
}

func (p *Plan) unknownUserFailure(rpc string, cred Failure) (Failure, bool) {
	if p.lib == nil {
		if unknownUser().MatchString(cred.When) {
			return cred, true
		}
		return Failure{}, false
	}
	candidates := []Failure{cred}
	for _, f := range p.lib.AllFailures(rpc) {
		if isUnauthenticated(f) || f.Unreachable != "" || f.ConnectCode == invalidArgCode || f.Label() == cred.Label() {
			continue
		}
		candidates = append(candidates, f)
	}
	for _, f := range candidates {
		if unknownUser().MatchString(f.When) || unknownReason().MatchString(namecase.Fold(f.Reason)) {
			return f, true
		}
	}
	return Failure{}, false
}

func roleField(m *catalog.Method) string {
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if !f.Repeated && f.Kind != "message" && strings.Contains(strings.ToLower(f.Name), "role") {
			return f.Name
		}
	}
	return ""
}

func (p *Plan) loginRoles(lib *Library, st *chain.Step, m *catalog.Method, c *RPCContract) []string {
	field := roleField(m)
	if field == "" || c == nil {
		return nil
	}
	text := strings.Join([]string{c.Exports[field], c.Terminal[field], c.SoftSignals[field], c.Summary}, " ")
	roles := []string{}
	for _, w := range roleWord().FindAllString(text, -1) {
		if !slices.Contains(roles, w) {
			roles = append(roles, w)
		}
	}
	out := []string{}
	if len(roles) == 0 {
		return nil
	}
	if r := defaultRole(lib, roles); r != "" && !hasExpectOn(st, field) {
		st.Expect = append(st.Expect, chain.Expectation{Path: field, Equals: r})
		out = append(out, fmt.Sprintf("%s asserts %s %s, the role every role-gated write the plans call as the default profile requires", st.ID, field, r))
	}
	for _, prof := range p.opts.Profiles {
		body := p.opts.ProfileBodies[prof]
		if len(body) == 0 {
			continue
		}
		role := ""
		for _, r := range roles {
			if namecase.Fold(r) == namecase.Fold(prof) {
				role = r
			}
		}
		if role == "" {
			out = append(out, fmt.Sprintf("profile %s names none of the roles the contract lists (%s), so its login role is not asserted", prof, strings.Join(roles, ", ")))
			continue
		}
		probe := probeStep(st, p.freeStepID(st.ID+"_as_"+profileSuffix(prof)))
		probe.Body = map[string]any{}
		for k, v := range body {
			key, ok := namecase.LookupKey(st.Body, k)
			if !ok {
				key = k
			}
			probe.Body[key] = cloneBody(v)
		}
		probe.Expect = append(SuccessExpectation(m), chain.Expectation{Path: field, Equals: role})
		for _, e := range st.Expect {
			if e.Within != nil {
				probe.Expect = append(probe.Expect, e)
			}
		}
		probe.Description = fmt.Sprintf("the login profile %s sends, from the config's auth: block: it succeeds as %s.", prof, role)
		p.Chain.Steps = append(p.Chain.Steps, probe)
		out = append(out, fmt.Sprintf("%s logs in as profile %s and asserts %s %s", probe.ID, prof, field, role))
	}
	return out
}

func defaultRole(lib *Library, roles []string) string {
	found := ""
	for _, rpc := range lib.RPCs() {
		c, ok := lib.Get(rpc)
		if !ok || c.DeclaresNoRole() || len(c.RequiresRole) != 1 || IsTodo(c.RequiresRole[0]) {
			continue
		}
		r := strings.TrimSpace(c.RequiresRole[0])
		if !slices.Contains(roles, r) {
			continue
		}
		if found != "" && found != r {
			return ""
		}
		found = r
	}
	return found
}

type LoginFailureGap struct {
	RPC     string `json:"rpc"`
	Failure string `json:"failure"`
}

func LoginFailureGaps(chains []*chain.Chain, lib *Library, cat *catalog.Catalog, logins []string) []LoginFailureGap {
	out := []LoginFailureGap{}
	for _, login := range logins {
		m, err := cat.Lookup(login)
		if err != nil {
			continue
		}
		for _, f := range lib.AllFailures(m.FullName) {
			if isUnauthenticated(f) || f.Unreachable != "" || (f.Code == 0 && f.Reason == "") {
				continue
			}
			if !pinnedByAChain(chains, cat, m.FullName, f) {
				out = append(out, LoginFailureGap{RPC: m.FullName, Failure: f.Label()})
			}
		}
	}
	return out
}

func pinnedByAChain(chains []*chain.Chain, cat *catalog.Catalog, rpc string, f Failure) bool {
	for _, s := range chainSteps(chains) {
		if m, err := cat.Lookup(s.Call); err != nil || m.FullName != rpc {
			continue
		}
		for _, e := range s.Expect {
			if e.Equals == nil {
				continue
			}
			v := fmt.Sprint(e.Equals)
			if (f.Reason != "" && v == f.Reason) || (f.Code != 0 && v == fmt.Sprint(f.Code)) || (f.ConnectCode != "" && f.Code == 0 && e.Path == "transport.code" && v == f.ConnectCode) {
				return true
			}
		}
	}
	return false
}
