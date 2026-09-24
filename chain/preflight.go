package chain

import (
	"fmt"
	"os"
	"sort"
)

func (c *Chain) PreflightProblems() []string {
	out := []string{}
	known := map[string]bool{}
	knownExports := map[string]bool{}
	idx := newRefIndex(c)
	for i, s := range c.Steps {
		if s == nil {
			continue
		}
		type use struct{ ref, where string }
		uses := []use{}
		for _, ref := range collectRefs(s.Body) {
			uses = append(uses, use{ref, ""})
		}
		for _, name := range sortedHeaderNames(s.Headers) {
			for _, ref := range collectRefs([]any{s.Headers[name]}) {
				uses = append(uses, use{ref, " header " + name})
			}
		}
		for _, u := range uses {
			ref := u.ref
			r := ParseRef(ref)
			if r.Kind == RefEnv {
				if _, set := os.LookupEnv(r.Rest); !set {
					out = append(out, fmt.Sprintf("step %q (step %d)%s reads ${%s}, and env %s is not set", s.ID, i+1, u.where, ref, r.Rest))
				}
				continue
			}
			if why := referenceProblem(r, known, knownExports, idx); why != "" {
				out = append(out, fmt.Sprintf("step %q (step %d)%s: ${%s} %s", s.ID, i+1, u.where, ref, why))
			}
		}
		known[s.ID] = true
		for name := range s.Export {
			knownExports[name] = true
		}
	}
	return out
}

func sortedHeaderNames(h map[string]string) []string {
	out := make([]string, 0, len(h))
	for name := range h {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
