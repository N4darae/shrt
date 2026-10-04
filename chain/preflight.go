package chain

import (
	"fmt"
	"os"

	"github.com/N4darae/shrt/catalog"
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
		for _, name := range sortedKeys(s.Headers) {
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

func (c *Chain) ResponseRefProblems(cat *catalog.Catalog) []string {
	out := []string{}
	if cat == nil {
		return out
	}
	c.eachTyped(cat, func(i int, s *Step, m *catalog.Method, responses map[string]*catalog.Method, exports map[string]exportOrigin) bool {
		never, _ := refTypeProblems(s, m, responses, exports)
		for _, why := range never {
			out = append(out, fmt.Sprintf("step %q (step %d): %s", s.ID, i+1, why))
		}
		for _, ref := range s.References() {
			if why, bad := responseRefProblem(ParseRef(ref), responses); bad {
				out = append(out, fmt.Sprintf("step %q (step %d): ${%s} %s", s.ID, i+1, ref, why))
			}
		}
		return true
	})
	return out
}

func (c *Chain) RefTypeMismatches(cat *catalog.Catalog, index int) []string {
	if cat == nil || index < 0 || index >= len(c.Steps) {
		return nil
	}
	var out []string
	c.eachTyped(cat, func(i int, s *Step, m *catalog.Method, responses map[string]*catalog.Method, exports map[string]exportOrigin) bool {
		if i == index {
			never, maybe := refTypeProblems(s, m, responses, exports)
			out = append(never, maybe...)
		}
		return i < index
	})
	return out
}

func (c *Chain) eachTyped(cat *catalog.Catalog, visit func(int, *Step, *catalog.Method, map[string]*catalog.Method, map[string]exportOrigin) bool) {
	responses := map[string]*catalog.Method{}
	exports := map[string]exportOrigin{}
	for i, s := range c.Steps {
		if s == nil {
			continue
		}
		m, _ := cat.Lookup(s.Call)
		if !visit(i, s, m, responses, exports) {
			return
		}
		if m != nil {
			responses[s.ID] = m
		}
		noteExports(s, exports)
	}
}
