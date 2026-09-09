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
		for _, name := range rc.Required {
			if IsRequiredLiteral(name) {
				continue
			}
			if HasUsableValue(s.Body, name, AuthoredBody) {
				continue
			}
			issues = append(issues, chain.Issue{
				Step:     s.ID,
				Severity: chain.SeverityError,
				Message: fmt.Sprintf(
					"%s is required by the contract for %s and this step sends no value for it, "+
						"while expecting success — fill it, or say what refusal you expect",
					name, s.Call),
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

func stepExpectsSuccess(s *chain.Step) bool {
	for _, e := range s.Expect {
		if chain.ExpectsTransportRefusal(e) {
			return false
		}
		if e.Path != chain.EnvelopePath() {
			continue
		}
		if text, ok := e.Equals.(string); ok && text != chain.EnvelopeOK() {
			return false
		}
		if text, ok := e.NotEqual.(string); ok && text == chain.EnvelopeOK() {
			return false
		}
	}
	return true
}
