package chain

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/namecase"
)

func lintVolatileIDs(c *Chain) []Issue {
	issues := []Issue{}
	check := func(step string, patterns []string) {
		ids := []string{}
		for _, p := range patterns {
			leaf := p[strings.LastIndex(p, ".")+1:]
			if namecase.IDNamed(strings.ReplaceAll(leaf, "*", "x")) {
				ids = append(ids, fmt.Sprintf("%q", p))
			}
		}
		if len(ids) > 0 {
			issues = append(issues, Issue{Step: step, Severity: SeverityWarn, Message: fmt.Sprintf(
				"volatile %s masks id-shaped paths: verify already pairs ids across runs, and masking them hides a stale id; drop it", strings.Join(ids, ", "))})
		}
	}
	check("", c.Volatile)
	for _, s := range c.Steps {
		check(s.ID, s.Volatile)
	}
	return issues
}
