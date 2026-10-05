package contract

import (
	"maps"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
)

type SingleItemRepeat struct {
	RPC          string   `json:"rpc"`
	Field        string   `json:"field"`
	Most         int      `json:"most"`
	Chains       []string `json:"chains"`
	SameResource bool     `json:"same_resource,omitempty"`
	Resource     string   `json:"resource,omitempty"`
	NoRepeat     bool     `json:"no_repeat,omitempty"`
}

func secondItems(body map[string]any, fields []*catalog.Field) []string {
	grown := []string{}
	var walk func(v any, fs []*catalog.Field, path string)
	walk = func(v any, fs []*catalog.Field, path string) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		for _, f := range fs {
			key, ok := namecase.LookupKey(m, f.Name)
			if !ok || len(f.Fields) == 0 || f.MapKey != "" || f.JSONForm != "" {
				continue
			}
			at := pathmask.Join(path, f.Name)
			list, isList := m[key].([]any)
			if !isList {
				walk(m[key], f.Fields, at)
				continue
			}
			for _, item := range list {
				walk(item, f.Fields, at)
			}
			if !f.Repeated || len(list) != 1 {
				continue
			}
			first, ok := list[0].(map[string]any)
			if !ok {
				continue
			}
			second := cloneBody(first).(map[string]any)
			distinctItem(second, f.Fields)
			m[key] = append(list, second)
			grown = append(grown, at)
		}
	}
	walk(body, fields, "")
	return grown
}

func distinctItem(item map[string]any, fields []*catalog.Field) {
	eachLeaf(item, fields, func(f *catalog.Field) bool { return idLike(f.Name) }, func(m map[string]any, key string, f *catalog.Field) {
		m[key] = nextValue(m[key], f.Kind)
	})
}

func idLike(name string) bool { return nameHasWord(name, "id", "uuid", "key") }

func nextValue(v any, kind string) any {
	if isNumericZero(v) {
		return v
	}
	switch t := v.(type) {
	case int:
		return t + 1
	case int64:
		return t + 1
	case float64:
		return t + 1
	case string:
		if t == "" || wholeReference(t) {
			return t
		}
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return strconv.FormatInt(n+1, 10)
		}
		if n, err := strconv.ParseFloat(t, 64); err == nil && kind != "string" {
			return strconv.FormatFloat(n+1, 'f', -1, 64)
		}
		if kind == "string" {
			return "2-" + t
		}
	}
	return v
}

func wholeReference(s string) bool {
	return chain.HasReference(s) && strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1
}

func SingleItemRepeats(chains []*chain.Chain, cat *catalog.Catalog) []SingleItemRepeat {
	type tally struct {
		most     int
		unknown  bool
		distinct bool
		repeat   bool
		sourced  bool
		resource string
		chains   map[string]bool
	}
	seen := map[string]*tally{}
	for c, s := range chainSteps(chains) {
		if len(s.Body) == 0 {
			continue
		}
		m, err := cat.Lookup(s.Call)
		if err != nil {
			continue
		}
		rpc := strings.TrimPrefix(m.Procedure(), "/")
		countRepeats(s.Body, catalog.DescribeMessage(m.Input()).Fields, "", func(path string, list []any, unknown bool) {
			k := rpc + "\x00" + path
			t := seen[k]
			if t == nil {
				t = &tally{chains: map[string]bool{}}
				seen[k] = t
			}
			t.unknown = t.unknown || unknown
			t.most = max(t.most, len(list))
			t.chains[c.Name] = true
			if len(list) < 2 {
				return
			}
			applied := []any{}
			for i, item := range list {
				if !itemRefused(s, i) {
					applied = append(applied, item)
				}
			}
			repeat, _ := repeatedResource(c, s, applied)
			_, sourced := repeatedResource(c, s, list)
			t.repeat = t.repeat || (repeat && effectOutcome(s) == outcomeSuccess)
			t.sourced = t.sourced || sourced
			if shared, ok := sharedResource(c, s, list); ok {
				if t.resource == "" {
					t.resource = shared
				}
				return
			}
			t.distinct = true
		})
	}
	out := []SingleItemRepeat{}
	for _, k := range chain.SortedKeys(seen) {
		t := seen[k]
		noRepeat := t.most >= 2 && t.distinct && t.sourced && !t.repeat
		if t.unknown || (t.most >= 2 && (t.distinct || t.resource == "") && !noRepeat) {
			continue
		}
		rpc, field, _ := strings.Cut(k, "\x00")
		names := chain.SortedKeys(t.chains)
		r := SingleItemRepeat{RPC: rpc, Field: field, Most: t.most, Chains: names}
		switch {
		case noRepeat:
			r.NoRepeat = true
		case t.most >= 2:
			r.SameResource, r.Resource = true, t.resource
		}
		out = append(out, r)
	}
	return out
}

func repeatedResource(c *chain.Chain, at *chain.Step, list []any) (bool, bool) {
	seen := map[string]bool{}
	sourced := true
	repeat := false
	for _, item := range list {
		got := map[string]string{}
		resourceLeaves(c, at, item, "", "", got)
		if len(got) == 0 {
			sourced = false
			continue
		}
		parts := []string{}
		for _, k := range chain.SortedKeys(got) {
			parts = append(parts, k+"="+got[k])
		}
		key := strings.Join(parts, "\x00")
		repeat = repeat || seen[key]
		seen[key] = true
	}
	return repeat, sourced
}

func sharedResource(c *chain.Chain, at *chain.Step, list []any) (string, bool) {
	first := ""
	var want map[string]string
	for _, item := range list {
		got := map[string]string{}
		resourceLeaves(c, at, item, "", "", got)
		if len(got) == 0 || want != nil && !maps.Equal(got, want) {
			return "", false
		}
		if want == nil {
			want, first = got, got[chain.SortedKeys(got)[0]]
		}
	}
	return first, true
}

func resourceLeaves(c *chain.Chain, at *chain.Step, v any, path, name string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			resourceLeaves(c, at, x, pathmask.Join(path, k), k, out)
		}
	case []any:
		for i, x := range t {
			resourceLeaves(c, at, x, pathmask.Join(path, strconv.Itoa(i)), name, out)
		}
	case string:
		t = stepReference(c, at, t)
		if src, ok := refSource(t); ok {
			if _, isStep := c.Step(src); isStep || (strings.HasPrefix(t, "${vars.") && idLike(name)) {
				out[path] = t
			}
			return
		}
		if t != "" && !chain.HasReference(t) && idLike(name) {
			out[path] = t
		}
	}
}

func stepReference(c *chain.Chain, at *chain.Step, s string) string {
	if !wholeReference(s) {
		return s
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "${"), "}")
	if rest, ok := strings.CutPrefix(inner, "steps."); ok {
		if id, path, ok := strings.Cut(rest, ".response."); ok && !strings.Contains(id, ".") {
			return "${" + id + "." + path + "}"
		}
		return s
	}
	name := strings.TrimPrefix(inner, "exports.")
	if strings.Contains(name, ".") {
		return s
	}
	found := s
	for _, st := range c.Steps {
		if st == at {
			break
		}
		if path, ok := st.Export[name]; ok {
			found = "${" + st.ID + "." + path + "}"
		}
	}
	return found
}

func countRepeats(v any, fields []*catalog.Field, path string, record func(path string, list []any, unknown bool)) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, f := range fields {
		key, ok := namecase.LookupKey(m, f.Name)
		if !ok || len(f.Fields) == 0 || f.MapKey != "" || f.JSONForm != "" {
			continue
		}
		at := pathmask.Join(path, f.Name)
		if !f.Repeated {
			countRepeats(m[key], f.Fields, at, record)
			continue
		}
		list, isList := m[key].([]any)
		if !isList {
			record(at, nil, m[key] != nil)
			continue
		}
		record(at, list, false)
		for _, item := range list {
			countRepeats(item, f.Fields, at, record)
		}
	}
}

func (p *Plan) noteSecondItems(id string, grown []string) {
	if len(grown) == 0 {
		return
	}
	noun := p.noun
	if noun == "" {
		noun = "the plan"
	}
	p.note("step %s: %s %s repeated, so %s sends two items, the second with its numbers raised by one "+
		"and its free-text strings prefixed with 2- (ids, keys, enums, zeros and ${...} references are copied "+
		"as they are, except a reference to a step that creates a resource, which reads a second such step instead). "+
		"One item leaves per-item logic untested: a total summed over the items, a check on the second "+
		"one. Give the second item its own values, and assert what depends on both; 'shrt contract status -gaps' "+
		"names repeated fields no chain sends with two, or whose items all point at the same resource",
		id, strings.Join(grown, ", "), pluralVerb(len(grown), "is", "are"), noun)
}
