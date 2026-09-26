package diff

import (
	"container/heap"
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

type stepPath struct {
	step, path string
}

func listPath(path string) string {
	out := []string{}
	for _, seg := range chain.SplitPath(path) {
		if _, err := strconv.Atoi(seg); err == nil {
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, ".")
}

func unorderedSet(lists ...[]string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lists {
		for _, p := range l {
			if n := listPath(p); n != "" {
				out[namecase.Fold(n)] = true
			}
		}
	}
	return out
}

func reorderUnordered(want, got any, path string, declared map[string]bool, r *strings.Replacer, moves map[string][]int) any {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return got
		}
		out := make(map[string]any, len(g))
		for k, gv := range g {
			if wv, ok := w[k]; ok {
				out[k] = reorderUnordered(wv, gv, pathmask.Join(path, k), declared, r, moves)
			} else {
				out[k] = gv
			}
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok {
			return got
		}
		if declared[namecase.Fold(listPath(path))] {
			var from []int
			g, from = permuted(g, pairItems(w, g, r))
			if moves != nil {
				moves[path] = from
			}
		}
		out := make([]any, len(g))
		for i := range g {
			if i < len(w) {
				out[i] = reorderUnordered(w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), declared, r, moves)
			} else {
				out[i] = g[i]
			}
		}
		return out
	}
	return got
}

func reorderCandidates(want, got any, path string, declared map[string]bool, r *strings.Replacer, emit func(string)) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return
		}
		for _, k := range sortedKeys(w, g) {
			wv, inW := w[k]
			gv, inG := g[k]
			if inW && inG {
				reorderCandidates(wv, gv, pathmask.Join(path, k), declared, r, emit)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return
		}
		if !declared[namecase.Fold(listPath(path))] && len(w) > 1 && len(w) == len(g) {
			order := pairItems(w, g, r)
			for i, j := range order {
				if i != j {
					emit(listPath(path))
					return
				}
			}
		}
		for i := range min(len(w), len(g)) {
			reorderCandidates(w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), declared, r, emit)
		}
	}
}

func pairItems(want, got []any, r *strings.Replacer) []int {
	scores := pairScores(want, got, r)
	order := make([]int, len(want))
	for i := range order {
		order[i] = -1
	}
	used := make([]bool, len(got))
	rows := &rowHeap{}
	for i, cands := range scores {
		if len(cands) == 0 {
			continue
		}
		row := &pairRow{i: i, cands: cands}
		heap.Init(row)
		rows.items = append(rows.items, row)
	}
	heap.Init(rows)
	for rows.Len() > 0 {
		row := rows.items[0]
		best := row.cands[0]
		if !used[best.j] {
			order[row.i], used[best.j] = best.j, true
			heap.Pop(rows)
			continue
		}
		heap.Pop(row)
		if row.Len() == 0 {
			heap.Pop(rows)
			continue
		}
		heap.Fix(rows, 0)
	}
	pairRemaining(order, used)
	return order
}

type pairCand struct{ j, score int }

type pairRow struct {
	i     int
	cands []pairCand
}

func (r *pairRow) before(a, b pairCand) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	da, db := absInt(r.i-a.j), absInt(r.i-b.j)
	if da != db {
		return da < db
	}
	return a.j < b.j
}

func (r *pairRow) Len() int           { return len(r.cands) }
func (r *pairRow) Less(a, b int) bool { return r.before(r.cands[a], r.cands[b]) }
func (r *pairRow) Swap(a, b int)      { r.cands[a], r.cands[b] = r.cands[b], r.cands[a] }
func (r *pairRow) Push(x any)         { r.cands = append(r.cands, x.(pairCand)) }
func (r *pairRow) Pop() any {
	last := r.cands[len(r.cands)-1]
	r.cands = r.cands[:len(r.cands)-1]
	return last
}

type rowHeap struct{ items []*pairRow }

func (h *rowHeap) Len() int { return len(h.items) }
func (h *rowHeap) Less(a, b int) bool {
	x, y := h.items[a], h.items[b]
	cx, cy := x.cands[0], y.cands[0]
	if cx.score != cy.score {
		return cx.score > cy.score
	}
	dx, dy := absInt(x.i-cx.j), absInt(y.i-cy.j)
	if dx != dy {
		return dx < dy
	}
	if x.i != y.i {
		return x.i < y.i
	}
	return cx.j < cy.j
}
func (h *rowHeap) Swap(a, b int) { h.items[a], h.items[b] = h.items[b], h.items[a] }
func (h *rowHeap) Push(x any)    { h.items = append(h.items, x.(*pairRow)) }
func (h *rowHeap) Pop() any {
	last := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	return last
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func pairRemaining(order []int, used []bool) {
	rows := []int{}
	for i, j := range order {
		if j < 0 {
			rows = append(rows, i)
		}
	}
	free := 0
	for _, u := range used {
		if !u {
			free++
		}
	}
	for d := 0; len(rows) > 0 && free > 0 && d <= len(order)+len(used); d++ {
		left := rows[:0]
		for _, i := range rows {
			assigned := false
			for _, j := range []int{i - d, i + d} {
				if j >= 0 && j < len(used) && !used[j] && !assigned {
					order[i], used[j], assigned = j, true, true
					free--
				}
				if d == 0 {
					break
				}
			}
			if !assigned {
				left = append(left, i)
			}
		}
		rows = left
	}
}

func pairScores(want, got []any, r *strings.Replacer) [][]pairCand {
	index := map[string][]int{}
	for j, g := range got {
		flattenLeaves(g, "", func(path, key string, _ any) {
			k := path + "\x00" + key
			index[k] = append(index[k], j)
		})
	}
	out := make([][]pairCand, len(want))
	score := make([]int, len(got))
	touched := []int{}
	for i, w := range want {
		touched = touched[:0]
		add := func(k string, n int) {
			for _, j := range index[k] {
				if score[j] == 0 {
					touched = append(touched, j)
				}
				score[j] += n
			}
		}
		flattenLeaves(w, "", func(path, key string, v any) {
			add(path+"\x00"+key, 1)
			if text, ok := v.(string); ok && r != nil {
				if renamed := r.Replace(text); renamed != text {
					add(path+"\x00"+leafKey(renamed), 2)
				}
			}
		})
		cands := make([]pairCand, 0, len(touched))
		for _, j := range touched {
			cands = append(cands, pairCand{j: j, score: score[j]})
			score[j] = 0
		}
		out[i] = cands
	}
	return out
}

func leafKey(v any) string {
	if text, ok := v.(string); ok {
		return "string\x00" + text
	}
	return jsonKind(v) + "\x00" + fmt.Sprintf("%v", v)
}

func flattenLeaves(v any, path string, visit func(path, key string, v any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			flattenLeaves(item, path+"\x01k"+k, visit)
		}
	case []any:
		for i, item := range t {
			flattenLeaves(item, path+"\x01i"+strconv.Itoa(i), visit)
		}
	default:
		visit(path, leafKey(v), v)
	}
}

func permuted(got []any, order []int) ([]any, []int) {
	out := make([]any, 0, len(got))
	from := make([]int, 0, len(got))
	used := make([]bool, len(got))
	for _, j := range order {
		if j >= 0 {
			out = append(out, got[j])
			from = append(from, j)
			used[j] = true
		}
	}
	for j, g := range got {
		if !used[j] {
			out = append(out, g)
			from = append(from, j)
		}
	}
	return out, from
}

func replayPath(path string, moves map[string][]int) string {
	if len(moves) == 0 {
		return ""
	}
	segs := chain.SplitPath(path)
	aligned, replay, moved := "", "", false
	for _, seg := range segs {
		if from, ok := moves[aligned]; ok {
			if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(from) {
				if from[i] != i {
					moved = true
				}
				aligned = pathmask.Join(aligned, seg)
				replay = pathmask.Join(replay, pathmask.IndexKey(from[i]))
				continue
			}
		}
		aligned = pathmask.Join(aligned, seg)
		replay = pathmask.Join(replay, seg)
	}
	if !moved {
		return ""
	}
	return replay
}

func likeness(want, got any, r *strings.Replacer) int {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return 0
		}
		n := 0
		for k, wv := range w {
			if gv, ok := g[k]; ok {
				n += likeness(wv, gv, r)
			}
		}
		return n
	case []any:
		g, ok := got.([]any)
		if !ok {
			return 0
		}
		n := 0
		for i := range min(len(w), len(g)) {
			n += likeness(w[i], g[i], r)
		}
		return n
	case string:
		g, ok := got.(string)
		if !ok {
			return 0
		}
		if w == g {
			return 1
		}
		if r != nil && r.Replace(w) == g {
			return 2
		}
		return 0
	}
	if jsonKind(want) == jsonKind(got) && sameScalar(want, got) {
		return 1
	}
	return 0
}

func (r *Report) noteReordered(spot *store.SafeSpot, rec *runner.Record, extra []string, fx *Fixtures, requests []Change) {
	r.Reordered, r.reordered = nil, nil
	if len(r.reorderCandidates) == 0 || r.Clean() {
		return
	}
	assumed := map[string][]string{}
	for _, c := range r.reorderCandidates {
		assumed[c.step] = append(assumed[c.step], c.path)
	}
	h := compareMasking(spot, rec, extra, assumed)
	if fx != nil {
		h.RequestChanges = append([]Change{}, requests...)
		h.separateInput(spot, rec, extra, *fx)
	}
	r.reorderExpect = map[string][]string{}
	for _, c := range r.reorderCandidates {
		if changesUnder(r.Changes, c) > 0 && changesUnder(h.Changes, c) == 0 {
			r.Reordered = append(r.Reordered, c.step+" "+c.path)
			r.reordered = append(r.reordered, c)
			if st, ok := rec.Step(c.step); ok {
				for _, e := range st.Expect {
					if p := listPath(e.Path); !e.Passed && (p == c.path || strings.HasPrefix(p, c.path+".")) {
						r.reorderExpect[c.step] = append(r.reorderExpect[c.step], chain.DescribeFailure(e))
					}
				}
			}
		}
	}
}

func (r *Report) underReordered(c Change) bool {
	for _, at := range r.reordered {
		if changesUnder([]Change{c}, at) > 0 {
			return true
		}
	}
	return false
}

func (r *Report) hiddenUnder(at stepPath) int {
	n := 0
	for _, c := range r.Changes {
		if c.Kind != KindNotReached && changesUnder([]Change{c}, at) > 0 {
			n++
		}
	}
	return n
}

func (r *Report) reorderOnlyStep(step string) bool {
	found := false
	for _, c := range r.Changes {
		if c.Step != step || c.Kind == KindStatus {
			continue
		}
		if !r.underReordered(c) {
			return false
		}
		found = true
	}
	return found
}

func (r *Report) ReorderedExpectations() []string {
	out := []string{}
	for _, at := range r.reordered {
		for _, e := range r.reorderExpect[at.step] {
			out = append(out, at.step+" "+e)
		}
	}
	return out
}

func changesUnder(changes []Change, at stepPath) int {
	n := 0
	for _, c := range changes {
		if c.Step != at.step {
			continue
		}
		if p := listPath(c.Path); p == at.path || strings.HasPrefix(p, at.path+".") {
			n++
		}
	}
	return n
}

func (r *Report) OnlyReordered() bool {
	if len(r.reordered) == 0 || r.Clean() {
		return false
	}
	for _, c := range r.Changes {
		switch {
		case r.underReordered(c):
		case c.Kind == KindStatus && r.reorderOnlyStep(c.Step):
		case c.Kind == KindNotReached:
		default:
			return false
		}
	}
	return true
}

type reorderGroup struct {
	path  string
	steps []string
}

func (r *Report) reorderedGroups() []reorderGroup {
	var out []reorderGroup
	at := map[string]int{}
	for _, sp := range r.reordered {
		i, ok := at[sp.path]
		if !ok {
			i = len(out)
			at[sp.path] = i
			out = append(out, reorderGroup{path: sp.path})
		}
		if !containsString(out[i].steps, sp.step) {
			out[i].steps = append(out[i].steps, sp.step)
		}
	}
	return out
}

func (g reorderGroup) String() string {
	return stepsText(g.steps, 3) + " " + g.path
}

func stepsText(steps []string, max int) string {
	if len(steps) <= max {
		return strings.Join(steps, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(steps[:max], ", "), len(steps)-max)
}

func (r *Report) ReorderedLists() []string {
	var out []string
	for _, g := range r.reorderedGroups() {
		out = append(out, g.String())
	}
	return out
}

func (r *Report) reorderedText() string {
	var b strings.Builder
	for _, g := range r.reorderedGroups() {
		on := "step " + g.steps[0]
		if len(g.steps) > 1 {
			on = "those steps"
		}
		b.WriteString("  " + g.String() + ": same items in another order: it holds what the safe spot holds, in another order. " +
			"If the rpc promises no order, declare `unordered: [" + g.path + "]` on " + on +
			" (or at chain level), and verify compares that list as a multiset, pairing items by content")
		var failed []string
		hidden := 0
		for _, step := range g.steps {
			for _, e := range r.reorderExpect[step] {
				if len(g.steps) > 1 {
					e = step + " " + e
				}
				failed = append(failed, e)
			}
			hidden += r.hiddenUnder(stepPath{step: step, path: g.path})
		}
		if len(failed) > 0 {
			b.WriteString("; the expectation(s) reading it by position failed: " + stepsText(failed, 3))
		}
		if hidden > 0 {
			fmt.Fprintf(&b, "; %d positional change(s) under it are counted above but not listed one by one (-json lists them)", hidden)
		}
		b.WriteString("\n")
	}
	return b.String()
}

type UnorderedAddition struct {
	Step  string
	Paths []string
}

func UnorderedAdditions(spot *store.SafeSpot, rec *runner.Record) []UnorderedAddition {
	was := map[string][]string{}
	for _, st := range spot.Steps {
		if st != nil {
			was[st.ID] = st.Unordered
		}
	}
	out := []UnorderedAddition{}
	compared := 0
	for _, st := range rec.Steps {
		if st == nil {
			continue
		}
		approved, ok := was[st.ID]
		if !ok {
			continue
		}
		added := []string{}
		for _, p := range st.Unordered {
			if !containsString(approved, p) && !containsString(added, p) {
				added = append(added, p)
			}
		}
		if len(added) > 0 {
			out = append(out, UnorderedAddition{Step: st.ID, Paths: added})
		}
		compared++
	}
	return chainLevelAdditions(out, compared)
}

func chainLevelAdditions(steps []UnorderedAddition, compared int) []UnorderedAddition {
	if compared < 2 || len(steps) < compared {
		return steps
	}
	common := []string{}
	for _, p := range steps[0].Paths {
		every := true
		for _, a := range steps[1:] {
			if !containsString(a.Paths, p) {
				every = false
				break
			}
		}
		if every {
			common = append(common, p)
		}
	}
	if len(common) == 0 {
		return steps
	}
	out := []UnorderedAddition{{Paths: common}}
	for _, a := range steps {
		rest := []string{}
		for _, p := range a.Paths {
			if !containsString(common, p) {
				rest = append(rest, p)
			}
		}
		if len(rest) > 0 {
			out = append(out, UnorderedAddition{Step: a.Step, Paths: rest})
		}
	}
	return out
}

func UnorderedAdded(spot *store.SafeSpot, rec *runner.Record) []string {
	out := []string{}
	for _, a := range UnorderedAdditions(spot, rec) {
		if a.Step == "" {
			out = append(out, fmt.Sprintf("`unordered: [%s]` at chain level", strings.Join(a.Paths, ", ")))
			continue
		}
		out = append(out, fmt.Sprintf("`unordered: [%s]` on step %s", strings.Join(a.Paths, ", "), a.Step))
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if namecase.Equal(x, s) {
			return true
		}
	}
	return false
}
