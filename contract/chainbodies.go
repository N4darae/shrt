package contract

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func LintChainBodies(c *chain.Chain, lib *Library, cat *catalog.Catalog) []chain.Issue {
	if c == nil || lib == nil || cat == nil {
		return nil
	}
	issues := []chain.Issue{}
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
			remedy := "answer the field's TODO in the contract to say the empty value is the point"
			if !ok {
				remedy = "scaffold a contract for this rpc with 'shrt contract init', and say there that the " +
					"empty value is the point"
			}
			issues = append(issues, chain.Issue{
				Step:     s.ID,
				Severity: chain.SeverityWarn,
				Message: fmt.Sprintf(
					"sends an empty string or a placeholder enum for %s, and the contract does not say that is "+
						"deliberate — a scaffolded or planned body carries those until someone fills the test data, "+
						"so this step would exercise an empty request rather than the case you meant. Fill it, or %s. "+
						"A numeric zero is NOT reported here: lint cannot tell the scaffold's filler from a deliberate "+
						"0, which is what the plan header is for",
					strings.Join(unfilled, ", "), remedy),
			})
		}
		if !ok {
			continue
		}
		if facts := DeclaredFacts(rc); len(facts) > 0 && !s.AllowFail && AssertsOnlyVerdict(s) {
			issues = append(issues, chain.Issue{
				Step:     s.ID,
				Severity: chain.SeverityWarn,
				Kind:     chain.KindEnvelopeOnly,
				Message:  EnvelopeOnlyMessage(s.Call, facts),
			})
		}
		for _, name := range rc.Required {
			if IsRequiredLiteral(name) {
				continue
			}
			if v, ok := bodyValue(s.Body, name); ok {
				if varName, empty := emptyDeclaredVar(c, v); empty {
					issues = append(issues, chain.Issue{
						Step:     s.ID,
						Severity: chain.SeverityError,
						Message: fmt.Sprintf(
							"%s is required by the contract for %s and this step sends ${vars.%s}, which vars: "+
								"declares as empty, so a run without -var %s=... sends no value for it. Give the var "+
								"a value under vars:, or remove it from vars: so shrt run refuses the chain until "+
								"-var %s=... supplies one",
							name, s.Call, varName, varName, varName),
					})
					continue
				}
			}
			if HasUsableValue(s.Body, name, AuthoredBody) {
				continue
			}
			issues = append(issues, chain.Issue{
				Step:     s.ID,
				Severity: chain.SeverityError,
				Message: fmt.Sprintf(
					"%s is required by the contract for %s and this step sends no value for it. Lint reads "+
						"this step as expecting success, because none of its expect entries states a refusal on "+
						"%s (the envelope path) or on transport.code / transport.http_status; an assertion on "+
						"another field, such as an app_code, does not make it a probe. Fill the field, or, if this "+
						"step is meant to be refused for leaving it out, state that refusal: %s equals: <refusal "+
						"code> (or not_equal: %s), or transport.code equals: <code>",
					name, s.Call, chain.EnvelopePath(), chain.EnvelopePath(), chain.EnvelopeOK()),
			})
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return issues
}

func unfilledBodyValues(s *chain.Step, m *catalog.Method, rc *RPCContract) []string {
	if len(s.Body) == 0 {
		return nil
	}
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
	if len(out) == 0 {
		return nil
	}
	return out
}

func contractRequiresField(rc *RPCContract, name string) bool {
	if rc == nil {
		return false
	}
	for _, r := range rc.Required {
		if !IsRequiredLiteral(r) && namecase.Equal(r, name) {
			return true
		}
	}
	return false
}

func contractExplainsField(rc *RPCContract, name string) bool {
	if rc == nil {
		return false
	}
	f := rc.Fields[name]
	if f == nil {
		return false
	}
	if f.From != "" || f.SameAs != "" || f.Value != "" {
		return true
	}
	if strings.TrimSpace(f.Note) == "" {
		return false
	}
	return !rc.IsUnfilled("fields." + name + ".note")
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
	return sortedFlagKeys(seen)
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

func EnvelopeOnlyMessage(rpc string, facts []string) string {
	return fmt.Sprintf("asserts only the verdict, which says the call did not fail, not what it did — and the "+
		"contract for %s declares what its response carries (%s). Assert at least one of those, with the value "+
		"this step should have produced (README rule 4); 'chain lint -strict' fails a step that does not",
		rpc, strings.Join(clipList(facts, 4), ", "))
}

func stepExpectsSuccess(s *chain.Step) bool {
	for _, e := range s.Expect {
		if chain.ExpectsTransportRefusal(e) {
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
