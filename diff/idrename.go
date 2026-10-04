package diff

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
)

type idPair struct {
	step string
	path string
	want any
	got  any
}

func collectIDPairs(step string, want, got any, path string, mask *pathmask.Masker, out *[]idPair) {
	if path != "" && mask.Masks(path) {
		return
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return
		}
		for _, k := range sortedKeys(w, g) {
			wv, wok := w[k]
			gv, gok := g[k]
			if wok && gok {
				collectIDPairs(step, wv, gv, pathmask.Join(path, k), mask, out)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return
		}
		for i := range min(len(w), len(g)) {
			collectIDPairs(step, w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), mask, out)
		}
	default:
		if renameable(path, want, got) {
			*out = append(*out, idPair{step: step, path: path, want: want, got: got})
		}
	}
}

func renameable(path string, want, got any) bool {
	if !idValue(want) || !idValue(got) || jsonKind(want) != jsonKind(got) {
		return false
	}
	if namecase.IDNamed(lastKey(path)) {
		return sameScalar(want, got) || sameShape(want, got)
	}
	return bothAre(want, got, uuidShape)
}

func idValue(v any) bool {
	switch t := v.(type) {
	case string:
		return t != "" && t != pathmask.MaskRedacted && t != pathmask.MaskVolatile && !isTimestamp(t)
	case float64:
		return t != 0
	}
	return false
}

func IDNamedPath(path string) bool {
	return namecase.IDNamed(lastKey(path))
}

func idKey(v any) string {
	return jsonKind(v) + ":" + fmt.Sprint(v)
}

func renamingViolations(pairs []idPair) []Change {
	forward := map[string]idPair{}
	reverse := map[string]idPair{}
	out := []Change{}
	for _, p := range pairs {
		w, g := idKey(p.want), idKey(p.got)
		if first, ok := forward[w]; ok && idKey(first.got) != g {
			out = append(out, Change{Step: p.step, Path: p.path, Kind: KindChanged, Want: first.got, Got: p.got,
				Detail: fmt.Sprintf("an id, but not renamed consistently: the safe spot's %v became %v at %s %s and %v here, "+
					"so this field now points at something else than it did", p.want, first.got, first.step, first.path, p.got)})
			continue
		}
		if first, ok := reverse[g]; ok && idKey(first.want) != w {
			out = append(out, Change{Step: p.step, Path: p.path, Kind: KindChanged, Want: p.want, Got: p.got,
				Detail: fmt.Sprintf("an id, but not renamed consistently: %v stands for the safe spot's %v at %s %s and for %v here, "+
					"so two different ids of the safe spot became one", p.got, first.want, first.step, first.path, p.want)})
			continue
		}
		if _, ok := forward[w]; !ok {
			forward[w] = p
		}
		if _, ok := reverse[g]; !ok {
			reverse[g] = p
		}
	}
	return out
}

func zeroID(s string) bool {
	rest := s
	if prefix := kindPrefix(s); prefix != "" {
		rest = s[len(prefix)+1:]
	}
	runs := alnumRuns(rest)
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if strings.Trim(r, "0") != "" {
			return false
		}
	}
	return true
}

func starPath(path string) string {
	segs := strings.Split(path, ".")
	out := []string{}
	for _, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil && len(out) > 0 {
			out[len(out)-1] += "[]"
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, ".")
}

func (r *Report) inconsistentIDGroups() (map[int]string, map[int]bool) {
	members, order := map[string][]int{}, []string{}
	for i, c := range r.Changes {
		if !strings.HasPrefix(c.Detail, inconsistentID) || outerList(c.Path) == "" || r.folded[c.Step] || r.underReordered(c) {
			continue
		}
		k := starPath(c.Path)
		if members[k] == nil {
			order = append(order, k)
		}
		members[k] = append(members[k], i)
	}
	lines, folded := map[int]string{}, map[int]bool{}
	for _, k := range order {
		idx := members[k]
		if len(idx) < 2 {
			continue
		}
		var steps []string
		for _, i := range idx {
			if !containsString(steps, r.Changes[i].Step) {
				steps = append(steps, r.Changes[i].Step)
			}
			folded[i] = true
		}
		c := r.Changes[idx[0]]
		lines[idx[0]] = fmt.Sprintf("  [%s] %-10s %s at %d item(s): %s, so each now points at something else than it did%s; e.g. %s %s\n",
			stepsText(steps, 3), c.Kind, k, len(idx), inconsistentID, r.oneValue(steps, c.Path), c.Path, c.describeValues())
	}
	return lines, folded
}

func (r *Report) oneValue(steps []string, path string) string {
	list := outerList(path)
	rest := strings.TrimPrefix(path, list+".")
	if _, tail, ok := strings.Cut(rest, "."); ok {
		rest = tail
	} else {
		return ""
	}
	from := ""
	for _, step := range steps {
		var cs *comparedStep
		for i := range r.compared {
			if r.compared[i].id == step {
				cs = &r.compared[i]
			}
		}
		if cs == nil {
			return ""
		}
		l, _ := chain.Get(cs.got, list)
		items, _ := l.([]any)
		if len(items) < 2 {
			return ""
		}
		first, _ := chain.Get(items[0], rest)
		for _, it := range items[1:] {
			if v, _ := chain.Get(it, rest); fmt.Sprint(v) != fmt.Sprint(first) {
				return ""
			}
		}
		at := sentAt(cs.sent, "", first)
		if at == "" || from != "" && at != from {
			from = "-"
		} else if from == "" {
			from = at
		}
	}
	if from == "" || from == "-" {
		return "; every item of " + list + " holds one value in each step"
	}
	return "; every item of " + list + " holds one value in each step, the request's " + from
}

func sentAt(v any, path string, want any) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(t, nil) {
			if p := sentAt(t[k], pathmask.Join(path, k), want); p != "" {
				return p
			}
		}
	case []any:
		for i, it := range t {
			if p := sentAt(it, pathmask.Join(path, pathmask.IndexKey(i)), want); p != "" {
				return p
			}
		}
	default:
		if path != "" && fmt.Sprint(v) == fmt.Sprint(want) {
			return path
		}
	}
	return ""
}
