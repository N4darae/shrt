package diff

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const undeclaredValueNotRecorded = "<on the wire, value not recorded>"

func (r *Report) DropUnsentDefaults(spot *store.SafeSpot, rec *runner.Record, unsent func(procedure, path string, v any) bool) {
	if unsent == nil || rec == nil {
		return
	}
	kept := r.Changes[:0]
	for _, c := range r.Changes {
		if c.Kind != KindUnexpected || c.Path == "step" || c.Path == "response" {
			kept = append(kept, c)
			continue
		}
		st, ok := rec.Step(c.Step)
		if !ok {
			kept = append(kept, c)
			continue
		}
		fits := func(path string, v any) bool { return unsent(st.Procedure, path, v) }
		was, known, onWire := undeclaredInSpot(spot, c.Step, c.Path)
		switch {
		case !onWire:
			if fits(c.Path, c.Got) {
				r.UnsentDefaults = append(r.UnsentDefaults, c.Step+" "+c.Path)
				continue
			}
		case known && sameOnWire(c.Path, was, c.Got, fits):
			r.UndeclaredSame = append(r.UndeclaredSame, c.Step+" "+c.Path)
			continue
		case known:
			c.Kind, c.Want = KindChanged, was
			c.Detail = undeclaredDetail(fits(c.Path, c.Got))
		case fits(c.Path, c.Got):
			c.Kind, c.Want = KindChanged, undeclaredValueNotRecorded
			c.Detail = undeclaredDetail(true)
		default:
			r.UndeclaredUnknown = append(r.UndeclaredUnknown, c.Step+" "+c.Path)
			continue
		}
		kept = append(kept, c)
	}
	r.Changes = kept
}

func undeclaredDetail(nowDefault bool) string {
	if nowDefault {
		return "on the wire, undeclared, in the safe spot's run; not on the wire now (left at the proto3 default)"
	}
	return "on the wire, undeclared, in the safe spot's run; declared now"
}

func undeclaredInSpot(spot *store.SafeSpot, stepID, path string) (value any, known, onWire bool) {
	if spot == nil {
		return nil, false, false
	}
	st := spotStep(spot, stepID)
	if st == nil {
		return nil, false, false
	}
	segs := strings.Split(path, ".")
	if len(st.Undeclared) > 0 {
		var tree any
		if err := json.Unmarshal(st.Undeclared, &tree); err == nil {
			if v, ok := lookupUndeclared(tree, segs); ok {
				return v, true, true
			}
		}
	}
	for _, line := range strings.Split(st.Warning, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), runner.UndeclaredFieldsWarning)
		if !ok {
			continue
		}
		for _, f := range strings.Split(rest, ", ") {
			if undeclaredPathMatches(f, segs) {
				return nil, false, true
			}
		}
	}
	return nil, false, false
}

func fieldKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "")
}

func lookupUndeclared(tree any, segs []string) (any, bool) {
	cur := tree
	for _, seg := range segs {
		switch node := cur.(type) {
		case map[string]any:
			next, found := node[seg]
			if !found {
				for k, v := range node {
					if fieldKey(k) == fieldKey(seg) {
						next, found = v, true
						break
					}
				}
			}
			if !found {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	if m, ok := cur.(map[string]any); ok && len(m) == 0 {
		return nil, false
	}
	return cur, true
}

func undeclaredPathMatches(pattern string, segs []string) bool {
	i := 0
	for _, p := range strings.Split(pattern, ".") {
		repeated := strings.HasSuffix(p, "[]")
		p = strings.TrimSuffix(p, "[]")
		if i >= len(segs) || fieldKey(segs[i]) != fieldKey(p) {
			return false
		}
		i++
		if repeated {
			if i >= len(segs) {
				return false
			}
			i++
		}
	}
	return i == len(segs)
}

func sameOnWire(path string, was, now any, unsent func(path string, v any) bool) bool {
	switch w := was.(type) {
	case map[string]any:
		g, ok := now.(map[string]any)
		if !ok {
			return false
		}
		matched := map[string]bool{}
		for k, gv := range g {
			wk, found := "", false
			for key := range w {
				if key == k || fieldKey(key) == fieldKey(k) {
					wk, found = key, true
					break
				}
			}
			child := pathmask.Join(path, k)
			if !found {
				if !unsent(child, gv) {
					return false
				}
				continue
			}
			matched[wk] = true
			if !sameOnWire(child, w[wk], gv, unsent) {
				return false
			}
		}
		return len(matched) == len(w)
	case []any:
		g, ok := now.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !sameOnWire(pathmask.Join(path, pathmask.IndexKey(i)), w[i], g[i], unsent) {
				return false
			}
		}
		return true
	default:
		return sameScalar(was, now)
	}
}
