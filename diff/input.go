package diff

import (
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
		if c.Path != AuthProfilePath && c.Path != ExpectPath && !chainLevel(c) {
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
	edited := map[string]bool{}
	for _, c := range material {
		if c.Path == ExpectPath {
			edited[c.Step] = true
			continue
		}
		i, ok := index[c.Step]
		if chainLevel(c) || !ok {
			i = 0
		}
		if from < 0 || i < from {
			from = i
		}
	}
	remaining, echoed, stale := splitStaleEchoes(r.Changes, r.compared, append(pairs, r.renames...))
	r.FixtureEchoed = append(r.FixtureEchoed, echoed...)
	remaining = append(remaining, stale...)
	kept := []Change{}
	for _, c := range remaining {
		if from >= 0 {
			i, ok := index[c.Step]
			c.WithInput = !ok || i >= from
		}
		if c.Kind == KindStatus && edited[c.Step] {
			c.WithInput = true
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

func (r *Report) ChainEdits(fedByVars func(Change) bool) []Change {
	out := []Change{}
	for _, c := range r.RequestChanges {
		if c.Path == AuthProfilePath || c.Path == ExpectPath || chainLevel(c) || fedByVars == nil || !fedByVars(c) {
			out = append(out, c)
		}
	}
	return out
}

func chainLevel(c Change) bool {
	return c.Path == "step" || c.Path == "steps" || c.Path == "call"
}
