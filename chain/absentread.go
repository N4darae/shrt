package chain

import (
	"fmt"
	"strings"
)

func lintAbsentReads(c *Chain, s *Step, known map[string]bool) []Issue {
	refs := append(collectRefs(s.Body), collectRefs(headerValues(s.Headers))...)
	for _, e := range s.Expect {
		refs = append(refs, e.References()...)
	}
	issues := []Issue{}
	seen := map[string]bool{}
	for _, ref := range refs {
		r := ParseRef(ref)
		if r.Kind != RefStep || !known[r.Head] || r.Head == s.ID || seen[ref] {
			continue
		}
		seen[ref] = true
		path := r.Rest
		if section, tail, _ := strings.Cut(path, "."); section == "request" {
			continue
		} else if section == "response" {
			path = tail
		}
		producer, ok := c.Step(r.Head)
		if !ok || path == "" {
			continue
		}
		if why := absentAt(producer, path); why != "" {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn,
				Message: fmt.Sprintf("${%s} reads what %s expects %s, so it resolves to nothing at run time", ref, producer.ID, why)})
		}
	}
	return issues
}

func absentAt(producer *Step, path string) string {
	path = strings.Join(SplitPath(path), ".")
	first, _, _ := strings.Cut(path, ".")
	asserted := false
	for _, e := range producer.Expect {
		ep := strings.Join(SplitPath(e.Path), ".")
		if e.Exists != nil && !*e.Exists {
			if path == ep || strings.HasPrefix(path, ep+".") {
				return fmt.Sprintf("absent (%s exists: false)", e.Path)
			}
			continue
		}
		if head, _, _ := strings.Cut(ep, "."); head == first {
			asserted = true
		}
	}
	envHead, _, _ := strings.Cut(EnvelopePath(), ".")
	if asserted || first == TransportPrefix || first == envHead || !ExpectsRefusal(producer) {
		return ""
	}
	return "to be refused"
}
