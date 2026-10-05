package chain

import (
	"crypto/rand"
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

const RunTagVar = "tag"

func (c *Chain) FreshRunTag() bool {
	_, declared := c.Vars[RunTagVar]
	return !declared && slices.ContainsFunc(chainRefs(c), func(r Ref) bool {
		name, _, _ := strings.Cut(r.Rest, ".")
		return r.Kind == RefVars && name == RunTagVar
	})
}

func NewRunTag() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("t%x", b)
}

func lintWholeTag(c *Chain, s *Step, m *catalog.Method) []Issue {
	if _, declared := c.Vars[RunTagVar]; (!declared && !c.FreshRunTag()) || s == nil || m == nil {
		return nil
	}
	issues := []Issue{}
	walkTypedBody(s.Body, catalog.DescribeMessage(m.Input()).Fields, "", func(path string, target *catalog.Field, value string, whole bool) {
		if whole || target.Kind != "string" {
			return
		}
		refs := collectRefs(value)
		if len(refs) != 1 || strings.TrimSpace(value) != "${"+refs[0]+"}" {
			return
		}
		r := ParseRef(refs[0])
		if r.Kind != RefVars || r.Rest != RunTagVar {
			return
		}
		leaf := path[strings.LastIndex(path, ".")+1:]
		if lower := strings.ToLower(leaf); lower == "id" || strings.HasSuffix(lower, "_id") || strings.HasPrefix(lower, "id_") {
			return
		}
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Message: fmt.Sprintf(
			"%s is ${vars.tag} alone, so every gate verify drifts with different input; build it into text: %s-${vars.tag}", path, leaf)})
	})
	return issues
}
