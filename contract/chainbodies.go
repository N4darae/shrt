package contract

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func LintChainBodies(c *chain.Chain, lib *Library, cat *catalog.Catalog) []chain.Issue {
	if c == nil || lib == nil || cat == nil {
		return nil
	}
	var issues []chain.Issue
	add := func(s *chain.Step, severity, format string, args ...any) {
		issues = append(issues, chain.Issue{Step: s.ID, Severity: severity, Message: fmt.Sprintf(format, args...)})
	}
	for _, s := range c.Steps {
		if s == nil || !stepExpectsSuccess(s) {
			continue
		}
		m, err := cat.Lookup(s.Call)
		if err != nil {
			continue
		}
		rc, ok := lib.Get(strings.TrimPrefix(m.Procedure(), "/"))
		if unfilled := unfilledBodyValues(s, m, rc); len(unfilled) > 0 {
			remedy := "answer the field's TODO in the contract"
			if !ok {
				remedy = "say so in a contract ('shrt contract init')"
			}
			issues = append(issues, chain.Issue{Step: s.ID, Severity: chain.SeverityWarn, Message: fmt.Sprintf(
				"sends an empty string or placeholder enum for %s; fill it, or if empty is the point, %s",
				strings.Join(unfilled, ", "), remedy),
				Why: "a scaffolded body carries those until the test data is filled; a numeric zero is not reported, the plan header is"})
		}
		if !ok {
			continue
		}
		if facts := DeclaredFacts(rc); len(facts) > 0 && !s.AllowFail && AssertsOnlyVerdict(s) {
			issues = append(issues, chain.Issue{
				Step:     s.ID,
				Severity: chain.SeverityWarn,
				Kind:     chain.KindEnvelopeOnly,
				Message:  envelopeOnlyShort(s.Call, facts),
				Why:      envelopeOnlyWhy,
			})
		}
		for _, name := range rc.Required {
			if IsRequiredLiteral(name) {
				continue
			}
			if v, ok := bodyValue(s.Body, name); ok {
				if varName, empty := emptyDeclaredVar(c, v); empty {
					add(s, chain.SeverityError, "%s is required by the contract, and ${vars.%s} is empty under vars:; "+
						"give it a value, or drop it from vars: so run needs -var %s=...",
						name, varName, varName)
					continue
				}
			}
			if HasUsableValue(s.Body, name, AuthoredBody) {
				continue
			}
			issues = append(issues, chain.Issue{Step: s.ID, Severity: chain.SeverityError, Message: fmt.Sprintf(
				"%s is required by the contract and not sent; fill it, or assert the refusal on %s or transport.code",
				name, chain.EnvelopePath()),
				Why: fmt.Sprintf("a step is a probe only when an expect pins a refusal on %s or transport.code / "+
					"transport.http_status; an app_code alone does not", chain.EnvelopePath())})
		}
	}
	return issues
}

func unfilledBodyValues(s *chain.Step, m *catalog.Method, rc *RPCContract) []string {
	out := []string{}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		key, ok := namecase.LookupKey(s.Body, f.Name)
		if !ok {
			continue
		}
		text, ok := s.Body[key].(string)
		if !ok {
			continue
		}
		zero := text == "" || (len(f.EnumValues) > 0 && text == f.EnumValues[0] && IsPlaceholderEnumValue(text))
		if !zero || contractRequiresField(rc, f.Name) || contractExplainsField(rc, f.Name) {
			continue
		}
		out = append(out, f.Name)
	}
	return out
}

func contractRequiresField(rc *RPCContract, name string) bool {
	return rc != nil && slices.ContainsFunc(rc.Required, func(r string) bool { return !IsRequiredLiteral(r) && namecase.Equal(r, name) })
}

func contractExplainsField(rc *RPCContract, name string) bool {
	if rc == nil {
		return false
	}
	f := rc.Fields[name]
	return f != nil && (f.From != "" || f.SameAs != "" || f.Value != "" || strings.TrimSpace(f.Note) != "" && !rc.IsUnfilled("fields."+name+".note"))
}

func DeclaredFacts(rc *RPCContract) []string {
	if rc == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, section := range []map[string]string{rc.Exports, rc.Terminal, rc.SoftSignals} {
		for key := range section {
			seen[key] = true
		}
	}
	return chain.SortedKeys(seen)
}

func AssertsOnlyVerdict(s *chain.Step) bool {
	if len(s.Expect) == 0 {
		return false
	}
	for _, e := range s.Expect {
		head, _, _ := strings.Cut(e.Path, ".")
		paging := chain.IsPagingFieldName(head) && e.Equals == nil && e.NotEqual == nil && e.Contains == ""
		if !chain.IsVerdictPath(e.Path) && !chain.IsTransportPath(e.Path) && head != chain.EnvelopeField() && !paging {
			return false
		}
	}
	return stepExpectsSuccess(s)
}

const envelopeOnlyWhy = "the verdict says the call did not fail, not what it did (README rule 4); 'chain lint -strict' fails such a step"

func envelopeOnlyShort(rpc string, facts []string) string {
	return fmt.Sprintf("asserts only the verdict, though the contract for %s declares %s",
		chain.RPCName(rpc), strings.Join(clipList(facts, 4), ", "))
}

func stepExpectsSuccess(s *chain.Step) bool {
	for _, e := range s.Expect {
		if chain.ExpectsTransportRefusal(e) || chain.PinsVerdictCode(e) {
			return false
		}
		if e.Path != chain.EnvelopePath() {
			continue
		}
		if e.Equals != nil && fmt.Sprint(e.Equals) != chain.EnvelopeOK() {
			return false
		}
		if e.NotEqual != nil && fmt.Sprint(e.NotEqual) == chain.EnvelopeOK() {
			return false
		}
	}
	return true
}

func emptyDeclaredVar(c *chain.Chain, v any) (string, bool) {
	text, ok := v.(string)
	if !ok {
		return "", false
	}
	name, ok := strings.CutPrefix(text, "${vars.")
	if !ok {
		return "", false
	}
	name, ok = strings.CutSuffix(name, "}")
	if !ok || name == "" || strings.ContainsAny(name, "${}") {
		return "", false
	}
	declared, ok := c.Vars[name]
	if !ok {
		return "", false
	}
	return name, IsPlaceholder(declared, AuthoredBody)
}

func lintWiring(c *chain.Chain, lib *Library, cat *catalog.Catalog) []chain.Issue {
	idLeaves := map[string]bool{}
	for _, rpc := range lib.RPCs() {
		for _, ref := range fieldFroms(lib, rpc, "") {
			idLeaves[leafOf(ref.Path)] = true
		}
	}
	issues := []chain.Issue{}
	for _, w := range c.Wires() {
		s, _ := c.Step(w.Step)
		sameRPC, wanted := false, []string{}
		for _, ref := range fieldFroms(lib, canonicalCall(cat, s.Call), w.Field) {
			if leafOf(ref.Path) == leafOf(w.Path) {
				wanted = nil
				break
			}
			sameRPC = sameRPC || canonicalCall(cat, ref.RPC) == canonicalCall(cat, w.Call)
			wanted = append(wanted, path.Base(ref.RPC)+" "+ref.Path)
		}
		if len(wanted) > 0 && (sameRPC || idLeaves[leafOf(w.Path)]) && stepExpectsSuccess(s) {
			issues = append(issues, chain.Issue{Step: w.Step, Severity: chain.SeverityError, Message: fmt.Sprintf(
				"%s is fed from %s %s, but its contract takes it from %s",
				w.Field, path.Base(w.Call), w.Path, strings.Join(wanted, " or "))})
		}
	}
	return issues
}

func fieldFroms(lib *Library, rpc, field string) []Ref {
	rc, _ := lib.Get(rpc)
	if rc == nil {
		return nil
	}
	sets := []map[string]*FieldContract{rc.Fields}
	for _, a := range rc.Aliases {
		if a != nil {
			sets = append(sets, a.Fields)
		}
	}
	out := []Ref{}
	for _, fields := range sets {
		for name, f := range fields {
			if f == nil {
				continue
			}
			if ref, err := ParseRef(f.From); err == nil && (field == "" || namecase.Fold(name) == namecase.Fold(field)) {
				out = append(out, ref)
			}
		}
	}
	return out
}

func leafOf(p string) string {
	return namecase.Fold(p[strings.LastIndex(p, ".")+1:])
}
