package chain

import (
	"fmt"
	"strings"
)

func relaxStep(st *Step, results []ExpectResult) []Relaxed {
	if len(results) == 0 || len(st.Expect) == 0 {
		return nil
	}
	aligned := len(results) == len(st.Expect)
	used := map[int]bool{}
	failed := func(i int, e Expectation) (ExpectResult, bool) {
		names := ruleNames(e)
		rule := ""
		if len(names) > 0 {
			rule = names[0]
		}
		if aligned {
			r := results[i]
			return r, !r.Passed && r.Path == e.Path && (rule == "" || r.Rule == rule)
		}
		for j, r := range results {
			if !used[j] && !r.Passed && r.Path == e.Path && r.Rule == rule {
				used[j] = true
				return r, true
			}
		}
		return ExpectResult{}, false
	}
	out := []Relaxed{}
	kept := make([]Expectation, 0, len(st.Expect))
	for i, e := range st.Expect {
		if r, bad := failed(i, e); bad {
			out = append(out, Relaxed{Step: st.ID, Path: r.Path, Rule: r.Rule, Want: r.Want, Got: r.Got})
			continue
		}
		kept = append(kept, e)
	}
	st.Expect = kept
	return out
}

func (r Relaxed) String() string {
	return fmt.Sprintf("%s %s %s (want %s, got %s)", r.Step, r.Path, r.Rule, verdictText(r.Want), verdictText(r.Got))
}

func RelaxedList(relaxed []Relaxed) string {
	parts := make([]string, 0, len(relaxed))
	for _, r := range relaxed {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, "; ")
}
