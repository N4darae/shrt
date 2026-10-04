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
	responses := map[string]*catalog.Method{}
	exports := map[string]exportOrigin{}
	for i, s := range c.Steps {
		if s == nil {
			continue
		}
		m, err := cat.Lookup(s.Call)
		if err == nil {
			never, _ := refTypeProblems(s, m, responses, exports)
			for _, why := range never {
				out = append(out, fmt.Sprintf("step %q (step %d): %s", s.ID, i+1, why))
			}
		}
		refs := append(collectRefs(s.Body), collectRefs(headerValues(s.Headers))...)
		for _, e := range s.Expect {
			refs = append(refs, e.References()...)
		}
		for _, ref := range refs {
			if why, bad := responseRefProblem(ParseRef(ref), responses); bad {
				out = append(out, fmt.Sprintf("step %q (step %d): ${%s} %s", s.ID, i+1, ref, why))
			}
		}
		if err == nil {
			responses[s.ID] = m
		}
		noteExports(s, exports)
	}
	return out
}

func (c *Chain) RefTypeMismatches(cat *catalog.Catalog, index int) []string {
	if cat == nil || index < 0 || index >= len(c.Steps) {
		return nil
	}
	responses := map[string]*catalog.Method{}
	exports := map[string]exportOrigin{}
	for i, s := range c.Steps {
		if s == nil {
			continue
		}
		m, err := cat.Lookup(s.Call)
		if i == index {
			if err != nil {
				return nil
			}
			never, maybe := refTypeProblems(s, m, responses, exports)
			return append(never, maybe...)
		}
		if err == nil {
			responses[s.ID] = m
		}
		noteExports(s, exports)
	}
	return nil
}
