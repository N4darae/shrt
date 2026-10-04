package diff

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
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
		if detail, changed := r.membership(rec, c.Step, c.Path, true); detail != "" {
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
		if detail, changed := r.membership(rec, c.Step, l, false); changed {
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
	kept := r.Changes[:0]
	for _, c := range r.Changes {
		if drop(c) == "" && listOf(resized[c.Step], c.Path) == "" {
			kept = append(kept, c)
		}
	}
	r.Changes = kept
	for _, c := range extra {
		if listOf(resized[c.Step], c.Path) == "" {
			r.Changes = append(r.Changes, c)
		}
	}
	for _, cs := range r.compared {
		for _, l := range resized[cs.id] {
			if listOf(resized[cs.id], l) == "" {
				r.Changes = append(r.Changes, r.pairedChanges(cs, l)...)
			}
		}
	}
}

func (r *Report) pairedChanges(cs comparedStep, list string) []Change {
	wl, gl := r.comparedAt(cs.id, list)
	key := itemKey(wl, gl)
	if key == "" {
		return nil
	}
	wkeys, _ := occurrences(wl, key, renamer(r.renames))
	was := map[string][]any{}
	for i, v := range wkeys {
		was[v] = append(was[v], wl[i])
	}
	gkeys, gnth := occurrences(gl, key, nil)
	aligned := make([]any, len(gl))
	var out []Change
	for i, it := range gl {
		v, n := gkeys[i], gnth[i]
		if n >= len(was[v]) {
			continue
		}
		w := was[v][n]
		aligned[i] = w
		walk(w, it, list+"."+strconv.Itoa(i), func(c Change) {
			c.Step = cs.id
			if cs.mask != nil && maskedValue(cs.mask, c) || c.Kind == KindChanged && LooksVolatile(c.Path, c.Want, c.Got) {
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
				if namecase.IDNamed(k) && idValue(v) {
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
	repeated := map[string]bool{}
	for k := range candidates {
		for _, l := range lists {
			seen := map[string]bool{}
			for _, it := range l {
				v := fmt.Sprint(it.(map[string]any)[k])
				if seen[v] {
					repeated[k] = true
				}
				seen[v] = true
			}
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if repeated[keys[i]] != repeated[keys[j]] {
			return !repeated[keys[i]]
		}
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

func repeatedKey(key string, lists ...[]any) bool {
	for _, l := range lists {
		if _, nth := occurrences(l, key, nil); slices.ContainsFunc(nth, func(n int) bool { return n > 0 }) {
			return true
		}
	}
	return false
}

func occurrences(items []any, key string, names *strings.Replacer) ([]string, []int) {
	keys, nth := make([]string, len(items)), make([]int, len(items))
	seen := map[string]int{}
	for i, it := range items {
		keys[i] = keyOf(it, key, names)
		nth[i] = seen[keys[i]]
		seen[keys[i]]++
	}
	return keys, nth
}

func keyOf(item any, key string, names *strings.Replacer) string {
	v := fmt.Sprint(item.(map[string]any)[key])
	if names != nil {
		v = names.Replace(v)
	}
	return v
}

func (r *Report) membership(rec *runner.Record, step, list string, repeats bool) (string, bool) {
	wl, gl := r.comparedAt(step, list)
	key := itemKey(wl, gl)
	if key == "" || !repeats && repeatedKey(key, wl, gl) {
		return "", false
	}
	rename := map[string]string{}
	for _, p := range r.renames {
		rename[p[0]] = p[1]
	}
	renamed := func(it any) string {
		v := fmt.Sprint(it.(map[string]any)[key])
		return cmp.Or(rename[v], v)
	}
	expected := map[string]int{}
	for _, it := range wl {
		expected[renamed(it)]++
	}
	present := map[string]int{}
	var added []map[string]any
	var addedIDs []string
	for _, it := range gl {
		m := it.(map[string]any)
		v := fmt.Sprint(m[key])
		present[v]++
		if present[v] > expected[v] {
			added = append(added, m)
			addedIDs = append(addedIDs, v)
		}
	}
	var dropped []string
	for _, it := range wl {
		v := renamed(it)
		if present[v]--; present[v] < 0 {
			dropped = append(dropped, v)
		}
	}
	if len(added) == 0 && len(dropped) == 0 {
		return "", false
	}
	out := fmt.Sprintf("%d added, %d dropped, by %s", len(added), len(dropped), key)
	made := producedIDs(rec, step)
	if len(dropped) > 0 {
		out += "; dropped " + namedItems(dropped, made)
	}
	if len(added) > 0 {
		out += "; added " + namedItems(addedIDs, made)
	}
	if st, ok := rec.Step(step); ok && st != nil {
		out += filterMisses(st.Request, added)
	}
	return out, true
}

func namedItems(ids []string, made map[string]string) string {
	shown := []string{}
	for _, id := range ids {
		if by := made[id]; by != "" {
			id += " (" + by + ")"
		}
		shown = append(shown, id)
	}
	return chain.ListSome(shown, 5)
}

func producedIDs(rec *runner.Record, before string) map[string]string {
	out := map[string]string{}
	if rec == nil {
		return out
	}
	var visit func(v any, key, step string)
	visit = func(v any, key, step string) {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				visit(x, k, step)
			}
		case []any:
			for _, x := range t {
				visit(x, key, step)
			}
		case string:
			if _, seen := out[t]; !seen && namecase.IDNamed(key) && idValue(t) {
				out[t] = step
			}
		}
	}
	for _, st := range rec.Steps {
		if st == nil || st.ID == before {
			break
		}
		var body any
		if chain.IsReadOnlyCall(st.Call) || json.Unmarshal(st.Response, &body) != nil {
			continue
		}
		visit(body, "", st.ID)
	}
	return out
}

func filterMisses(request json.RawMessage, added []map[string]any) string {
	var req map[string]any
	if len(added) == 0 || json.Unmarshal(request, &req) != nil {
		return ""
	}
	out := ""
	for _, f := range sortedKeys(req, nil) {
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

func (r *Report) Moved(step, path string) bool {
	return r.Mover()(step, path)
}

func (r *Report) Mover() func(step, path string) bool {
	type list struct {
		wkeys []string
		wnth  []int
		at    map[string]int
	}
	names := renamer(r.renames)
	lists := map[string]*list{}
	return func(step, path string) bool {
		segs := strings.Split(path, ".")
		for k := 1; k < len(segs); k++ {
			i, err := strconv.Atoi(segs[k])
			if err != nil {
				continue
			}
			at := step + "\x00" + strings.Join(segs[:k], ".")
			l, ok := lists[at]
			if !ok {
				wl, gl := r.comparedAt(step, strings.Join(segs[:k], "."))
				if key := itemKey(wl, gl); key != "" {
					l = &list{at: map[string]int{}}
					l.wkeys, l.wnth = occurrences(wl, key, names)
					gkeys, gnth := occurrences(gl, key, nil)
					for j := len(gl) - 1; j >= 0; j-- {
						l.at[gkeys[j]+"\x00"+strconv.Itoa(gnth[j])] = j
					}
				}
				lists[at] = l
			}
			if l == nil || i < 0 || i >= len(l.wkeys) {
				continue
			}
			if j, ok := l.at[l.wkeys[i]+"\x00"+strconv.Itoa(l.wnth[i])]; ok && j != i {
				return true
			}
		}
		return false
	}
}
