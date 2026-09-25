package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const inconsistentID = "an id, but not renamed consistently"

func outerList(path string) string {
	segs := strings.Split(path, ".")
	for i := 1; i < len(segs); i++ {
		if _, err := strconv.Atoi(segs[i]); err == nil {
			return strings.Join(segs[:i], ".")
		}
	}
	return ""
}

func underList(path, list string) bool {
	rest, ok := strings.CutPrefix(path, list+".")
	if !ok {
		return false
	}
	head, _, _ := strings.Cut(rest, ".")
	_, err := strconv.Atoi(head)
	return err == nil
}

func (r *Report) collapseMembership(rec *runner.Record) {
	lists := map[string][]string{}
	at := map[string]int{}
	resized := map[string][]string{}
	for i, c := range r.Changes {
		if c.Kind != KindLength || c.Step == "" || c.Detail != "" {
			continue
		}
		if detail, changed := r.membership(rec, c.Step, c.Path); detail != "" {
			r.Changes[i].Detail = detail
			if changed {
				resized[c.Step] = append(resized[c.Step], c.Path)
			}
		}
		lists[c.Step] = append(lists[c.Step], c.Path)
		at[c.Step+" "+c.Path] = i
	}
	var extra []Change
	for _, c := range r.Changes {
		l := outerList(c.Path)
		if !strings.HasPrefix(c.Detail, inconsistentID) || l == "" || containsList(lists[c.Step], c.Path) {
			continue
		}
		lists[c.Step] = append(lists[c.Step], l)
		if detail, changed := r.membership(rec, c.Step, l); changed {
			_, gl := r.comparedAt(c.Step, l)
			at[c.Step+" "+l] = -1 - len(extra)
			extra = append(extra, Change{Step: c.Step, Path: l, Kind: KindMembership, Want: len(gl), Got: len(gl), Detail: detail})
			resized[c.Step] = append(resized[c.Step], l)
		}
	}
	drop := func(c Change) string {
		if !strings.HasPrefix(c.Detail, inconsistentID) {
			return ""
		}
		l := listOf(lists[c.Step], c.Path)
		if _, ok := at[c.Step+" "+l]; l == "" || !ok {
			return ""
		}
		return c.Step + " " + l
	}
	noted := map[string]bool{}
	for _, c := range r.Changes {
		key := drop(c)
		if key == "" || noted[key] {
			continue
		}
		noted[key] = true
		if i := at[key]; i >= 0 && r.Changes[i].Detail != "" {
			r.Changes[i].Detail += "; per-item ids not listed"
		} else if i < 0 {
			extra[-1-i].Detail += "; per-item ids not listed"
		}
	}
	kept := r.Changes[:0]
	for _, c := range r.Changes {
		if drop(c) == "" && (c.Kind == KindLength || c.Kind == KindMembership || listOf(resized[c.Step], c.Path) == "") {
			kept = append(kept, c)
		}
	}
	r.Changes = append(kept, extra...)
	for _, cs := range r.compared {
		for _, l := range resized[cs.id] {
			r.Changes = append(r.Changes, r.pairedChanges(cs, l)...)
		}
	}
}

func (r *Report) pairedChanges(cs comparedStep, list string) []Change {
	wl, gl := r.comparedAt(cs.id, list)
	key := itemKey(wl, gl)
	if key == "" {
		return nil
	}
	names := renamer(r.renames)
	was := map[string]any{}
	for _, it := range wl {
		v := fmt.Sprint(it.(map[string]any)[key])
		if names != nil {
			v = names.Replace(v)
		}
		was[v] = it
	}
	aligned := make([]any, len(gl))
	var out []Change
	for i, it := range gl {
		w, ok := was[fmt.Sprint(it.(map[string]any)[key])]
		if !ok {
			continue
		}
		aligned[i] = w
		walk(w, it, list+"."+strconv.Itoa(i), func(c Change) {
			c.Step = cs.id
			if cs.mask != nil && maskedValue(cs.mask, c) || c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got) {
				return
			}
			out = append(out, c)
		})
	}
	setAt(cs.want, strings.Split(list, "."), aligned)
	return out
}

func setAt(root any, segs []string, v any) {
	for len(segs) > 1 {
		switch t := root.(type) {
		case map[string]any:
			root = t[segs[0]]
		case []any:
			i, err := strconv.Atoi(segs[0])
			if err != nil || i >= len(t) {
				return
			}
			root = t[i]
		default:
			return
		}
		segs = segs[1:]
	}
	if m, ok := root.(map[string]any); ok && len(segs) == 1 {
		m[segs[0]] = v
	}
}

func listOf(lists []string, path string) string {
	best := ""
	for _, l := range lists {
		if underList(path, l) && (best == "" || len(l) < len(best)) {
			best = l
		}
	}
	return best
}

func containsList(lists []string, path string) bool {
	return listOf(lists, path) != ""
}

func (r *Report) comparedAt(step, list string) ([]any, []any) {
	for _, cs := range r.compared {
		if cs.id != step {
			continue
		}
		w, _ := chain.Get(cs.want, list)
		g, _ := chain.Get(cs.got, list)
		wl, _ := w.([]any)
		gl, _ := g.([]any)
		return wl, gl
	}
	return nil, nil
}

func itemKey(lists ...[]any) string {
	candidates := map[string]bool{}
	first := true
	for _, l := range lists {
		for _, it := range l {
			m, ok := it.(map[string]any)
			if !ok {
				return ""
			}
			here := map[string]bool{}
			for k, v := range m {
				if idNamed(k) && idValue(v) {
					here[k] = true
				}
			}
			if first {
				candidates, first = here, false
				continue
			}
			for k := range candidates {
				if !here[k] {
					delete(candidates, k)
				}
			}
		}
	}
	keys := []string{}
	for k := range candidates {
		distinct := true
		for _, l := range lists {
			seen := map[string]bool{}
			for _, it := range l {
				v := fmt.Sprint(it.(map[string]any)[k])
				if seen[v] {
					distinct = false
				}
				seen[v] = true
			}
		}
		if distinct {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "id") != (keys[j] == "id") {
			return keys[i] == "id"
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func (r *Report) membership(rec *runner.Record, step, list string) (string, bool) {
	wl, gl := r.comparedAt(step, list)
	key := itemKey(wl, gl)
	if key == "" {
		return "", false
	}
	rename := map[string]string{}
	for _, p := range r.renames {
		rename[p[0]] = p[1]
	}
	expected := map[string]bool{}
	for _, it := range wl {
		v := fmt.Sprint(it.(map[string]any)[key])
		if g, ok := rename[v]; ok {
			v = g
		}
		expected[v] = true
	}
	present := map[string]bool{}
	var added []map[string]any
	for _, it := range gl {
		m := it.(map[string]any)
		v := fmt.Sprint(m[key])
		present[v] = true
		if !expected[v] {
			added = append(added, m)
		}
	}
	dropped := 0
	for v := range expected {
		if !present[v] {
			dropped++
		}
	}
	if len(added) == 0 && dropped == 0 {
		return "", false
	}
	out := fmt.Sprintf("%d added, %d dropped, by %s", len(added), dropped, key)
	if st, ok := rec.Step(step); ok && st != nil {
		out += filterMisses(st.Request, added)
	}
	return out, true
}

func filterMisses(request json.RawMessage, added []map[string]any) string {
	var req map[string]any
	if len(added) == 0 || json.Unmarshal(request, &req) != nil {
		return ""
	}
	fields := make([]string, 0, len(req))
	for f := range req {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	out := ""
	for _, f := range fields {
		want, ok := req[f].(string)
		if !ok || want == "" || strings.HasSuffix(want, "_UNSPECIFIED") {
			continue
		}
		n := 0
		for _, m := range added {
			if v, ok := m[f]; ok && fmt.Sprint(v) != want {
				n++
			}
		}
		if n > 0 {
			out += fmt.Sprintf("; %d added have %s other than the request's %s", n, f, want)
		}
		field := strings.Trim(strings.Replace(f, "prefix", "", 1), "_")
		if field == f || field == "" {
			continue
		}
		n = 0
		for _, m := range added {
			if v, ok := m[field].(string); ok && !strings.HasPrefix(v, want) {
				n++
			}
		}
		if n > 0 {
			out += fmt.Sprintf("; %d added have %s not starting with %s %q", n, field, f, want)
		}
	}
	return out
}
