package diff

import (
	"fmt"
	"github.com/N4darae/shrt/chain"
	"slices"
	"strings"

	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const minFixtureEcho = 3

type Fixtures struct {
	Named     func(step, path string) bool
	Generated func(step, path string) bool
	Var       func(name string) bool
	Reads     map[string][]Read
}

type Read struct {
	Step    string
	Request bool
	Path    string
}

func (r *Report) SeparateInput(spot *store.SafeSpot, rec *runner.Record, extra []string, fx Fixtures) {
	requests := append([]Change{}, r.RequestChanges...)
	r.separateInput(spot, rec, extra, fx)
	r.noteReordered(spot, rec, extra, &fx, requests)
}

func (r *Report) separateInput(spot *store.SafeSpot, rec *runner.Record, extra []string, fx Fixtures) {
	r.inputSeparated = true
	fixture := fx.Named
	patterns := slices.Concat(spot.Volatile, rec.Volatile, extra)
	stepVolatile := map[string][]string{}
	for _, st := range append(append([]*runner.StepRecord{}, spot.Steps...), rec.Steps...) {
		stepVolatile[st.ID] = append(stepVolatile[st.ID], st.Volatile...)
	}
	material := []Change{}
	pairs := [][2]string{}
	for _, c := range r.RequestChanges {
		if c.Path != AuthProfilePath && !expectationChange(c) && !chainLevel(c) {
			m := pathmask.NewMasker(slices.Concat(patterns, stepVolatile[c.Step]))
			if maskedAt(m, c) || (fixture != nil && fixture(c.Step, c.Path)) {
				r.FixtureInput = append(r.FixtureInput, c)
				if p, ok := echoPair(c); ok {
					pairs = append(pairs, p)
				}
				continue
			}
		}
		material = append(material, c)
	}
	r.RequestChanges = material
	pairs = append(pairs, generatedPairs(spot.Steps, rec.Steps, fx.Generated)...)
	index := map[string]int{}
	for i, st := range spot.Steps {
		if _, seen := index[st.ID]; !seen {
			index[st.ID] = i
		}
	}
	from := -1
	firstEdited := -1
	edited := map[string]bool{}
	causal := fx.Reads != nil
	inputAt := map[string][]string{}
	edits := stepEdits{touched: map[string]bool{}, writeAt: map[string]bool{}, afterAddedWrite: map[string]bool{}}
	for _, c := range material {
		if !expectationChange(c) {
			_, ok := index[c.Step]
			switch {
			case stepLevel(c) && c.Path != "steps":
				edits.note(c, ok, rec)
			case !ok || stepLevel(c):
				causal = false
			}
			inputAt[c.Step] = append(inputAt[c.Step], c.Path)
		}
	}
	for _, c := range material {
		if expectationChange(c) {
			if !editedExpectFailed(rec, c) {
				continue
			}
			edited[c.Step] = true
			if i, ok := index[c.Step]; ok && (firstEdited < 0 || i < firstEdited) {
				firstEdited = i
			}
			continue
		}
		i, ok := index[c.Step]
		if stepLevel(c) || !ok {
			i = 0
		}
		if from < 0 || i < from {
			from = i
		}
	}
	remaining, echoed, stale := splitStaleEchoes(r.Changes, r.compared, append(pairs, r.renames...))
	r.dropEchoedUnapproved(append(pairs, r.renames...))
	r.noteHiddenStaleEchoes(append(pairs, r.renames...))
	r.FixtureEchoed = append(r.FixtureEchoed, echoed...)
	remaining = append(remaining, stale...)
	renameWants(remaining, renamer(append(pairs, r.renames...)))
	var explained map[string]bool
	if causal && from >= 0 {
		explained = explainedSteps(spot.Steps, remaining, inputAt, fx.Reads, readValueChanged(spot, rec, renamer(append(pairs, r.renames...))), edits)
	}
	kept := []Change{}
	for _, c := range remaining {
		if from >= 0 {
			i, ok := index[c.Step]
			c.WithInput = !ok || i >= from
			if explained != nil && ok {
				c.WithInput = explained[c.Step]
			}
		}
		if c.Kind == KindStatus && edited[c.Step] {
			c.WithInput = true
		}
		if i, ok := index[c.Step]; c.Kind == KindNotReached && firstEdited >= 0 && ok && i > firstEdited {
			c.WithInput = true
		}
		kept = append(kept, c)
	}
	r.Changes = kept
}

type stepEdits struct {
	touched         map[string]bool
	writeAt         map[string]bool
	afterAddedWrite map[string]bool
}

func (s stepEdits) note(c Change, inSpot bool, rec *runner.Record) {
	s.touched[c.Step] = true
	write := false
	for _, call := range []any{c.Want, c.Got} {
		if text, ok := call.(string); ok && text != "" && !chain.IsReadOnlyCall(text) {
			write = true
		}
	}
	if !write {
		return
	}
	if inSpot {
		s.writeAt[c.Step] = true
		return
	}
	after := false
	for _, st := range rec.Steps {
		if after {
			s.afterAddedWrite[st.ID] = true
		}
		if st.ID == c.Step {
			after = true
		}
	}
}

func explainedSteps(order []*runner.StepRecord, changes []Change, inputAt map[string][]string, reads map[string][]Read, valueChanged func(step, path string) bool, edits stepEdits) map[string]bool {
	responseChanged := map[string]bool{}
	for _, c := range changes {
		if c.Kind != KindStatus && c.Kind != KindNotReached {
			responseChanged[c.Step] = true
		}
	}
	explained := map[string]bool{}
	carried, stateChanged := false, false
	for _, st := range order {
		if _, done := explained[st.ID]; done {
			continue
		}
		why := len(inputAt[st.ID]) > 0
		for _, rd := range reads[st.ID] {
			switch {
			case rd.Request && pathsOverlap(inputAt[rd.Step], rd.Path):
				why = true
			case !rd.Request && responseChanged[rd.Step] && explained[rd.Step] && (valueChanged == nil || valueChanged(rd.Step, rd.Path)):
				why = true
			}
		}
		for _, rd := range reads[st.ID] {
			if edits.touched[rd.Step] {
				why = true
			}
		}
		if stateChanged || edits.touched[st.ID] || edits.afterAddedWrite[st.ID] {
			why = true
		}
		if why {
			carried = true
		}
		explained[st.ID] = why
		if len(inputAt[st.ID]) > 0 && responseChanged[st.ID] && !chain.IsReadOnlyCall(st.Call) || edits.writeAt[st.ID] {
			stateChanged = true
		}
		for _, c := range changes {
			if c.Step == st.ID && c.Kind == KindNotReached && carried {
				explained[st.ID] = true
			}
		}
	}
	return explained
}

func readValueChanged(spot *store.SafeSpot, rec *runner.Record, rn *strings.Replacer) func(step, path string) bool {
	return func(step, path string) bool {
		was := spotStep(spot, step)
		now, okNow := rec.Step(step)
		if was == nil || !okNow {
			return true
		}
		a, errA := decode(was.Response)
		b, errB := decode(now.Response)
		if errA != nil || errB != nil {
			return true
		}
		x, inA := chain.Get(a, path)
		y, inB := chain.Get(b, path)
		if inA != inB {
			return true
		}
		if !inA {
			return false
		}
		return !SameTree(x, y, "", func(_ string, a, b any) bool {
			if w, ok := a.(string); ok {
				g, ok := b.(string)
				return ok && (w == g || rn != nil && rn.Replace(w) == g)
			}
			return jsonKind(a) == jsonKind(b) && sameScalar(a, b)
		})
	}
}

func spotStep(spot *store.SafeSpot, id string) *runner.StepRecord {
	for _, st := range spot.Steps {
		if st != nil && st.ID == id {
			return st
		}
	}
	return nil
}

func SameTree(a, b any, path string, leaf func(path string, a, b any) bool) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if w, ok := y[k]; !ok || !SameTree(v, w, pathmask.Join(path, k), leaf) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !SameTree(x[i], y[i], path, leaf) {
				return false
			}
		}
		return true
	}
	return leaf(path, a, b)
}

func renameWants(changes []Change, rn *strings.Replacer) {
	if rn == nil {
		return
	}
	for i, c := range changes {
		w, okW := c.Want.(string)
		g, okG := c.Got.(string)
		if c.Kind != KindChanged || c.Detail != "" || !okW || !okG || renameable(c.Path, w, g) {
			continue
		}
		if renamed := rn.Replace(w); renamed != w && renamed != g {
			changes[i].Want = renamed
		}
	}
}

func pathsOverlap(changed []string, read string) bool {
	for _, p := range changed {
		if read == "" || p == read || strings.HasPrefix(p, read+".") || strings.HasPrefix(read, p+".") {
			return true
		}
	}
	return false
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

func editedExpectFailed(rec *runner.Record, c Change) bool {
	if c.Kind == KindMissing {
		return false
	}
	text := c.Got
	if c.Path == ExpectValuePath {
		text = c.Want
	}
	path, _, _ := strings.Cut(fmt.Sprint(text), " ")
	st, ok := rec.Step(c.Step)
	if !ok || (st.Transport != nil && !chain.IsTransportPath(path)) {
		return false
	}
	for _, r := range st.Expect {
		if !r.Passed && r.Rule != "unevaluated" && r.Path == path {
			return true
		}
	}
	return false
}

func (r *Report) OnlyExpectationsEdited() bool {
	if len(r.RequestChanges) == 0 {
		return false
	}
	for _, c := range r.RequestChanges {
		if !expectationChange(c) {
			return false
		}
	}
	return true
}

func chainLevel(c Change) bool {
	return stepLevel(c) || refChange(c)
}

func stepLevel(c Change) bool {
	return c.Path == "step" || c.Path == "steps" || c.Path == "call"
}

func generatedPairs(was, now []*runner.StepRecord, generated func(step, path string) bool) [][2]string {
	if generated == nil {
		return nil
	}
	byID := map[string]*runner.StepRecord{}
	for _, st := range now {
		if _, seen := byID[st.ID]; !seen {
			byID[st.ID] = st
		}
	}
	out := [][2]string{}
	for _, a := range was {
		if b, ok := byID[a.ID]; ok {
			walkRequests(a, b, func(c Change) {
				if p, ok := echoPair(c); ok && c.Kind == KindChanged && generated(a.ID, c.Path) {
					out = append(out, p)
				}
			})
		}
	}
	return out
}

func (r *Report) dropEchoedUnapproved(pairs [][2]string) {
	rn := renamer(pairs)
	if rn == nil || len(r.UnapprovedMasked) == 0 {
		return
	}
	echo := map[string]bool{}
	for _, c := range r.VolatileValues {
		w, okW := c.Want.(string)
		g, okG := c.Got.(string)
		if c.Kind == KindChanged && okW && okG && w != g && !renameable(c.Path, w, g) && rn.Replace(w) == g {
			echo[c.Step+" "+c.Path] = true
		}
	}
	kept := r.UnapprovedMasked[:0]
	for _, p := range r.UnapprovedMasked {
		if !echo[p] {
			kept = append(kept, p)
		}
	}
	r.UnapprovedMasked = kept
	if r.approvedMask == nil {
		return
	}
	values, paths := r.VolatileValues[:0], r.VolatilePaths[:0]
	for _, c := range r.VolatileValues {
		key := c.Step + " " + c.Path
		if echo[key] && !maskedAt(r.approvedMask, c) {
			r.VolatileMasked--
			r.FixtureEchoed = append(r.FixtureEchoed, c)
			continue
		}
		values = append(values, c)
		paths = append(paths, key)
	}
	r.VolatileValues, r.VolatilePaths = values, paths
}

func (r *Report) noteHiddenStaleEchoes(pairs [][2]string) {
	if len(r.UnapprovedVolatile) == 0 || r.approvedMask == nil {
		return
	}
	steps := make([]comparedStep, 0, len(r.compared))
	masks := map[string]*pathmask.Masker{}
	for _, st := range r.compared {
		masks[st.id] = st.mask
		st.mask = r.approvedMask
		steps = append(steps, st)
	}
	listed := map[string]bool{}
	for _, p := range r.UnapprovedMasked {
		listed[p] = true
	}
	walkRenamedText(steps, renamer(pairs), func(step, path, want, got, renamed string) {
		key := step + " " + path
		if want != got || listed[key] || masks[step] == nil || !underMask(masks[step], path) {
			return
		}
		listed[key] = true
		r.UnapprovedMasked = append(r.UnapprovedMasked, key)
	})
}

func (r *Report) Class(c Change) string {
	switch {
	case r.underReordered(c), c.Kind == KindStatus && r.reorderOnlyStep(c.Step):
		return "order changed"
	case len(r.RequestChanges) > 0 && (!r.inputSeparated || c.WithInput) && r.OnlyChainChanged():
		return "chain change"
	case len(r.RequestChanges) > 0 && (!r.inputSeparated || c.WithInput):
		return "different input"
	case r.PrincipalChanged():
		return "different input"
	}
	return "regression"
}
