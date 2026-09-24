package diff

import (
	"fmt"
	"sort"
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

func reorderUnordered(want, got any, path string, declared map[string]bool, r *strings.Replacer) any {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return got
		}
		out := make(map[string]any, len(g))
		for k, gv := range g {
			if wv, ok := w[k]; ok {
				out[k] = reorderUnordered(wv, gv, pathmask.Join(path, k), declared, r)
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
			g = permuted(g, pairItems(w, g, r))
		}
		out := make([]any, len(g))
		for i := range g {
			if i < len(w) {
				out[i] = reorderUnordered(w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), declared, r)
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
	type cand struct{ i, j, score int }
	cands := []cand{}
	for i := range want {
		for j := range got {
			cands = append(cands, cand{i, j, likeness(want[i], got[j], r)})
		}
	}
	sort.SliceStable(cands, func(a, b int) bool {
		x, y := cands[a], cands[b]
		if x.score != y.score {
			return x.score > y.score
		}
		dx, dy := x.i-x.j, y.i-y.j
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		if dx != dy {
			return dx < dy
		}
		if x.i != y.i {
			return x.i < y.i
		}
		return x.j < y.j
	})
	order := make([]int, len(want))
	for i := range order {
		order[i] = -1
	}
	used := make([]bool, len(got))
	for _, c := range cands {
		if order[c.i] < 0 && !used[c.j] {
			order[c.i], used[c.j] = c.j, true
		}
	}
	return order
}

func permuted(got []any, order []int) []any {
	out := make([]any, 0, len(got))
	used := make([]bool, len(got))
	for _, j := range order {
		if j >= 0 {
			out = append(out, got[j])
			used[j] = true
		}
	}
	for j, g := range got {
		if !used[j] {
			out = append(out, g)
		}
	}
	return out
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
	for _, c := range r.reorderCandidates {
		if changesUnder(r.Changes, c) > 0 && changesUnder(h.Changes, c) == 0 {
			r.Reordered = append(r.Reordered, c.step+" "+c.path)
			r.reordered = append(r.reordered, c)
		}
	}
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
		under := false
		for _, at := range r.reordered {
			if changesUnder([]Change{c}, at) > 0 {
				under = true
				break
			}
		}
		if !under {
			return false
		}
	}
	return true
}

func (r *Report) reorderedText() string {
	if len(r.reordered) == 0 {
		return ""
	}
	var b strings.Builder
	for _, at := range r.reordered {
		b.WriteString("  " + at.step + " " + at.path + ": same items in another order: it holds what the safe spot holds, in another order. " +
			"If the rpc promises no order, declare `unordered: [" + at.path + "]` on step " + at.step +
			" (or at chain level), and verify compares that list as a multiset, pairing items by content\n")
	}
	return b.String()
}

func UnorderedAdded(spot *store.SafeSpot, rec *runner.Record) []string {
	was := map[string][]string{}
	for _, st := range spot.Steps {
		if st != nil {
			was[st.ID] = st.Unordered
		}
	}
	out := []string{}
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
			out = append(out, fmt.Sprintf("`unordered: [%s]` on step %s", strings.Join(added, ", "), st.ID))
		}
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
