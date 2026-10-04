package diff

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

var refPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

const (
	BodyPathPrefix = "body."
	RefDetail      = "the step's body reads another step or field than the confirmed run did"
)

const HeaderRefDetail = "the step's header reads another step or field than the confirmed run did"

func refChange(c Change) bool {
	return c.Detail == RefDetail && strings.HasPrefix(c.Path, BodyPathPrefix) ||
		c.Detail == HeaderRefDetail && strings.HasPrefix(c.Path, HeadersPathPrefix)
}

func refChanges(before []*runner.StepRecord, was *runner.StepRecord, now *chain.Step) []Change {
	current := runner.BodyRefs(now.Body)
	if was.BodyRefs == nil {
		return inferredRefChanges(before, was, current)
	}
	paths := []string{}
	for p := range current {
		paths = append(paths, p)
	}
	for p := range was.BodyRefs {
		if _, ok := current[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	recorded, _ := decode(was.Request)
	out := []Change{}
	for _, p := range paths {
		w, hadRef := was.BodyRefs[p]
		g, hasRef := current[p]
		if hadRef && hasRef && chain.FoldedRefs(w) == chain.FoldedRefs(g) {
			continue
		}
		c := Change{Step: was.ID, Path: BodyPathPrefix + p, Kind: KindChanged, Want: w, Got: g, Detail: RefDetail}
		if !hadRef {
			if v, ok := chain.Get(recorded, p); ok {
				c.Want = v
			} else {
				c.Kind, c.Want = KindUnexpected, nil
			}
		}
		if !hasRef {
			if v, ok := chain.Get(now.Body, p); ok {
				c.Got = v
			} else {
				c.Kind, c.Got = KindMissing, nil
			}
		}
		out = append(out, c)
	}
	return wholeItems(out, recorded, now.Body, was.BodyRefs, current)
}

func wholeItems(changes []Change, recorded any, body map[string]any, wasRefs, nowRefs map[string]string) []Change {
	out := []Change{}
	done := map[string]bool{}
	for _, c := range changes {
		if c.Kind != KindMissing && c.Kind != KindUnexpected {
			out = append(out, c)
			continue
		}
		p := strings.TrimPrefix(c.Path, BodyPathPrefix)
		item, ok := goneItem(p, recorded, body, c.Kind == KindMissing)
		if !ok {
			out = append(out, c)
			continue
		}
		if done[item] {
			continue
		}
		done[item] = true
		whole := Change{Step: c.Step, Path: BodyPathPrefix + item, Kind: c.Kind, Detail: RefDetail}
		if c.Kind == KindMissing {
			v, _ := chain.Get(recorded, item)
			whole.Want = withTemplates(v, item, wasRefs)
		} else {
			v, _ := chain.Get(body, item)
			whole.Got = withTemplates(v, item, nowRefs)
		}
		out = append(out, whole)
	}
	return out
}

func goneItem(path string, recorded any, body map[string]any, removed bool) (string, bool) {
	segs := chain.SplitPath(path)
	for i := 1; i < len(segs); i++ {
		if !digitsOnly(segs[i]) {
			continue
		}
		prefix := strings.Join(segs[:i+1], ".")
		_, inRecorded := chain.Get(recorded, prefix)
		_, inBody := chain.Get(body, prefix)
		if removed && inRecorded && !inBody || !removed && inBody && !inRecorded {
			return prefix, true
		}
	}
	return "", false
}

func withTemplates(v any, prefix string, refs map[string]string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(m))
	for k, x := range m {
		out[k] = x
		if t, ok := refs[prefix+"."+k]; ok {
			out[k] = t
		}
	}
	return out
}

func inferredRefChanges(before []*runner.StepRecord, was *runner.StepRecord, current map[string]string) []Change {
	if len(current) == 0 || len(was.Request) == 0 {
		return nil
	}
	recorded, err := decode(was.Request)
	if err != nil {
		return nil
	}
	scope := chain.NewScope(nil)
	scope.Env = func(string) (string, bool) { return "", false }
	for _, st := range before {
		req, _ := decode(st.Request)
		resp, _ := decode(st.Response)
		scope.Record(st.ID, req, resp)
		for k, v := range st.Exported {
			scope.Exports[k] = v
		}
	}
	paths := []string{}
	for p := range current {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := []Change{}
	for _, p := range paths {
		text := current[p]
		if generated(text) {
			continue
		}
		resolved, err := scope.ResolveValue(text)
		if err != nil || redactedValue(resolved) {
			continue
		}
		v, ok := chain.Get(recorded, p)
		if !ok {
			out = append(out, Change{Step: was.ID, Path: BodyPathPrefix + p, Kind: KindUnexpected, Got: text, Detail: RefDetail})
			continue
		}
		if redactedValue(v) || fmt.Sprint(v) == fmt.Sprint(resolved) {
			continue
		}
		want := any(v)
		if ref := sourceOf(before, v, p); ref != "" {
			want = ref
		}
		out = append(out, Change{Step: was.ID, Path: BodyPathPrefix + p, Kind: KindChanged, Want: want, Got: text, Detail: RefDetail})
	}
	return out
}

func generated(text string) bool {
	for _, m := range refPattern.FindAllStringSubmatch(text, -1) {
		switch chain.ParseRef(m[1]).Kind {
		case chain.RefUUID, chain.RefClock:
			return true
		}
	}
	return false
}

func redactedValue(v any) bool {
	s, ok := v.(string)
	return ok && (s == pathmask.MaskRedacted || strings.Contains(s, pathmask.MaskRedacted))
}

func sourceOf(before []*runner.StepRecord, value any, requestPath string) string {
	want := fmt.Sprint(value)
	key := lastKey(requestPath)
	best, bestScore := "", -1
	for i := len(before) - 1; i >= 0; i-- {
		st := before[i]
		resp, err := decode(st.Response)
		if err != nil {
			continue
		}
		visitScalars(resp, "", func(path string, v any) {
			if fmt.Sprint(v) != want {
				return
			}
			score := 0
			if lastKey(path) == key {
				score = 1
			}
			if score > bestScore {
				best, bestScore = "${"+st.ID+"."+path+"}", score
			}
		})
	}
	return best
}

func visitScalars(v any, path string, fn func(string, any)) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			visitScalars(t[k], pathmask.Join(path, k), fn)
		}
	case []any:
		for i, x := range t {
			visitScalars(x, pathmask.Join(path, pathmask.IndexKey(i)), fn)
		}
	case nil:
	default:
		fn(path, v)
	}
}

func DropRefEdited(requests, chainChanges []Change) []Change {
	edited := map[string]bool{}
	for _, c := range chainChanges {
		if refChange(c) {
			edited[c.Step+"\x00"+strings.TrimPrefix(c.Path, BodyPathPrefix)] = true
		}
	}
	if len(edited) == 0 {
		return requests
	}
	out := []Change{}
	for _, c := range requests {
		if !edited[c.Step+"\x00"+c.Path] {
			out = append(out, c)
		}
	}
	return out
}
