package runner

import (
	"regexp"
	"strings"
)

var skipReason = regexp.MustCompile(`^(not sent: \$\{[^}]*\} reads step "([^"]*)"), (.+)$`)

type SkipCondenser struct {
	first map[string]string
}

func NewSkipCondenser() *SkipCondenser {
	return &SkipCondenser{first: map[string]string{}}
}

func (c *SkipCondenser) Condense(stepID, text string) string {
	m := skipReason.FindStringSubmatch(text)
	if m == nil || len(m[3]) < 80 {
		return text
	}
	key := strings.ReplaceAll(m[3], "${steps."+m[2]+".", "${steps.*.")
	if at, seen := c.first[key]; seen && at != stepID {
		return m[1] + ", for the same reason as at step " + at + " above"
	}
	c.first[key] = stepID
	return text
}
