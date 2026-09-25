package contract

import (
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

type SingleItemRepeat struct {
	RPC    string   `json:"rpc"`
	Field  string   `json:"field"`
	Most   int      `json:"most"`
	Chains []string `json:"chains"`
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
			at := join(path, f.Name)
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
	for _, f := range fields {
		key, ok := namecase.LookupKey(item, f.Name)
		if !ok || f.Repeated || f.MapKey != "" || len(f.EnumValues) > 0 || idLike(f.Name) {
			continue
		}
		if len(f.Fields) > 0 {
			if nested, ok := item[key].(map[string]any); ok {
				distinctItem(nested, f.Fields)
			}
			continue
		}
		item[key] = nextValue(item[key], f.Kind)
	}
}

func idLike(name string) bool {
	for _, w := range namecase.Words(name) {
		if strings.EqualFold(w, "id") || strings.EqualFold(w, "uuid") || strings.EqualFold(w, "key") {
			return true
		}
	}
	return false
}

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
		most    int
		unknown bool
		chains  map[string]bool
	}
	seen := map[string]*tally{}
	keys := []string{}
	for _, c := range chains {
		if c == nil {
			continue
		}
		for _, s := range c.Steps {
			if s == nil || len(s.Body) == 0 {
				continue
			}
			m, err := cat.Lookup(s.Call)
			if err != nil {
				continue
			}
			rpc := strings.TrimPrefix(m.Procedure(), "/")
			countRepeats(s.Body, catalog.DescribeMessage(m.Input()).Fields, "", func(path string, n int, unknown bool) {
				k := rpc + "\x00" + path
				t := seen[k]
				if t == nil {
					t = &tally{chains: map[string]bool{}}
					seen[k] = t
					keys = append(keys, k)
				}
				t.unknown = t.unknown || unknown
				t.most = max(t.most, n)
				t.chains[c.Name] = true
			})
		}
	}
	out := []SingleItemRepeat{}
	for _, k := range keys {
		t := seen[k]
		if t.unknown || t.most >= 2 {
			continue
		}
		rpc, field, _ := strings.Cut(k, "\x00")
		names := []string{}
		for name := range t.chains {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, SingleItemRepeat{RPC: rpc, Field: field, Most: t.most, Chains: names})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RPC != out[j].RPC {
			return out[i].RPC < out[j].RPC
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func countRepeats(v any, fields []*catalog.Field, path string, record func(path string, n int, unknown bool)) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, f := range fields {
		key, ok := namecase.LookupKey(m, f.Name)
		if !ok || len(f.Fields) == 0 || f.MapKey != "" || f.JSONForm != "" {
			continue
		}
		at := join(path, f.Name)
		if !f.Repeated {
			countRepeats(m[key], f.Fields, at, record)
			continue
		}
		list, isList := m[key].([]any)
		if !isList {
			record(at, 0, m[key] != nil)
			continue
		}
		record(at, len(list), false)
		for _, item := range list {
			countRepeats(item, f.Fields, at, record)
		}
	}
}

func (p *Plan) noteSecondItems(id string, grown []string) {
	if len(grown) == 0 {
		return
	}
	p.note("step %s: %s %s repeated, so the plan sends two items, the second with its numbers raised by one "+
		"and its free-text strings prefixed with 2- (ids, keys, enums, zeros and ${...} references are copied "+
		"as they are). One item leaves per-item logic untested: a total summed over the items, a check on the second "+
		"one. Give the second item its own values, or its own resource through an aliased producer step if the "+
		"rpc wants distinct ones; 'shrt contract status -gaps' names repeated fields no chain sends with two",
		id, strings.Join(grown, ", "), pluralVerb(len(grown), "is", "are"))
}
