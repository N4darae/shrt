package chain

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
)

const RunTagVar = "tag"

func (c *Chain) FreshRunTag() bool {
	if _, declared := c.Vars[RunTagVar]; declared {
		return false
	}
	for _, r := range chainRefs(c) {
		if name, _, _ := strings.Cut(r.Rest, "."); r.Kind == RefVars && name == RunTagVar {
			return true
		}
	}
	return false
}

func NewRunTag() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
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
			"%s is ${vars.tag} as a whole value, so it is input, not a fixture name: shrt gate passes a "+
				"fresh -var tag to every run and verify, and each verify then fails with `drift with different input`. "+
				"Build it inside other text, such as %s-${vars.tag}, which verify treats as a fixture name", path, leaf)})
	})
	return issues
}
