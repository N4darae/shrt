package diff

import (
	"strings"

	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const minFixtureEcho = 3

func (r *Report) SeparateInput(spot *store.SafeSpot, rec *runner.Record, extra []string, fixture func(step, path string) bool) {
	r.inputSeparated = true
	patterns := mergePatterns(spot.Volatile, rec.Volatile, extra)
	stepVolatile := map[string][]string{}
	for _, st := range append(append([]*runner.StepRecord{}, spot.Steps...), rec.Steps...) {
		stepVolatile[st.ID] = append(stepVolatile[st.ID], st.Volatile...)
	}
	material := []Change{}
	pairs := [][2]string{}
	for _, c := range r.RequestChanges {
		if c.Path != AuthProfilePath && !chainLevel(c) {
			m := pathmask.NewMasker(mergePatterns(patterns, stepVolatile[c.Step]))
			if maskedAt(m, c) || (fixture != nil && fixture(c.Step, c.Path)) {
				r.FixtureInput = append(r.FixtureInput, c)
				a, okA := c.Want.(string)
				b, okB := c.Got.(string)
				if okA && okB && len(a) >= minFixtureEcho {
					pairs = append(pairs, [2]string{a, b})
				}
				continue
			}
		}
		material = append(material, c)
	}
	r.RequestChanges = material
	index := map[string]int{}
	for i, st := range spot.Steps {
		if _, seen := index[st.ID]; !seen {
			index[st.ID] = i
		}
	}
	from := -1
	for _, c := range material {
		i, ok := index[c.Step]
		if chainLevel(c) || !ok {
			i = 0
		}
		if from < 0 || i < from {
			from = i
		}
	}
	kept := []Change{}
	for _, c := range r.Changes {
		if echoesFixture(c, pairs) {
			r.FixtureEchoed = append(r.FixtureEchoed, c)
			continue
		}
		if from >= 0 {
			i, ok := index[c.Step]
			c.WithInput = !ok || i >= from
		}
		kept = append(kept, c)
	}
	r.Changes = kept
}

func (r *Report) Unexplained() []Change {
	if !r.inputSeparated && len(r.RequestChanges) > 0 {
		return nil
	}
	out := []Change{}
	for _, c := range r.Changes {
		if !c.WithInput {
			out = append(out, c)
		}
	}
	return out
}

func chainLevel(c Change) bool {
	return c.Path == "step" || c.Path == "steps" || c.Path == "call"
}

func echoesFixture(c Change, pairs [][2]string) bool {
	if c.Kind != KindChanged || len(pairs) == 0 {
		return false
	}
	want, okA := c.Want.(string)
	got, okB := c.Got.(string)
	if !okA || !okB {
		return false
	}
	for _, p := range pairs {
		want = strings.ReplaceAll(want, p[0], p[1])
	}
	return want == got
}
