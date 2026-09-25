package contract

import (
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

func listUnscoped(st *chain.Step) bool {
	var scoped func(v any) bool
	scoped = func(v any) bool {
		switch t := v.(type) {
		case string:
			return strings.Contains(t, "${")
		case map[string]any:
			for _, item := range t {
				if scoped(item) {
					return true
				}
			}
		case []any:
			for _, item := range t {
				if scoped(item) {
					return true
				}
			}
		}
		return false
	}
	return !scoped(map[string]any(st.Body))
}

func prefixTargetKey(prefixKey string, producer *chain.Step) string {
	want := namecase.Fold(strings.TrimSuffix(strings.TrimPrefix(strings.ReplaceAll(strings.ToLower(prefixKey), "prefix", ""), "_"), "_"))
	if want == "" {
		return ""
	}
	for k := range producer.Body {
		if namecase.Fold(k) == want {
			return k
		}
	}
	return ""
}

func runPrefix(v string) string {
	loc := planVarRef.FindStringIndex(v)
	if loc == nil || strings.Contains(v[:loc[0]], "${") {
		return ""
	}
	end := loc[1]
	if end < len(v) && strings.IndexByte("-_./:#|~", v[end]) >= 0 {
		end++
	}
	return v[:end]
}

func (p *Plan) scopeListByPrefix(t *listTarget) (string, string, bool) {
	keys := make([]string, 0, len(t.step.Body))
	for k := range t.step.Body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if text, ok := t.step.Body[key].(string); !ok || text != "" || !strings.Contains(namecase.Fold(key), "prefix") {
			continue
		}
		target := prefixTargetKey(key, t.producers[0])
		if target == "" {
			continue
		}
		value, _ := t.producers[0].Body[target].(string)
		prefix := runPrefix(value)
		if prefix == "" {
			continue
		}
		shared := true
		for _, prod := range t.producers {
			if v, _ := prod.Body[target].(string); !strings.HasPrefix(v, prefix) {
				shared = false
			}
		}
		if !shared {
			continue
		}
		t.step.Body[key] = prefix
		return key, prefix, true
	}
	return "", "", false
}

func (p *Plan) scopeUnscopedList(t *listTarget) {
	if !listUnscoped(t.step) {
		return
	}
	if key, prefix, ok := p.scopeListByPrefix(t); ok {
		p.note("step %s: the contract leaves %s empty, which lists everything the backend holds, so an item count or "+
			"position would fail on a second run against the same database; the plan sets it to %q, the start every "+
			"fixture's value shares, so the list holds only what this run created", t.step.ID, key, prefix)
		return
	}
	t.unscoped = true
}

func assertLowerBound(st *chain.Step, listPath string) {
	n, ok := assertedLength(st, listPath)
	if !ok || n == 0 {
		return
	}
	last := listPath + "." + itoa(n-1)
	for _, e := range st.Expect {
		if e.Path == last || strings.HasPrefix(e.Path, last+".") {
			return
		}
	}
	st.Expect = append(st.Expect, chain.Expectation{Path: last, Exists: boolPtr(true)})
}

func (p *Plan) noteUnscopedList(t *listTarget, n int) {
	t.step.Expect = append(t.step.Expect, chain.Expectation{Path: t.listPath + "." + itoa(n-1), Exists: boolPtr(true)})
	p.assertMembers(t)
	p.note("step %s: nothing in its request scopes %s to what this run created (no field reads a var, a step or a "+
		"generator), so it lists whatever else the backend holds too: the plan asserts at least %d item(s) and that each "+
		"fixture is among them by id (includes:), and no position or exact count, which would fail on the second run. "+
		"Give the list a filter the fixtures share (a prefix built from ${vars.tag}) in the contract's value: to have "+
		"the order and the count asserted", t.step.ID, t.listPath, n)
}

func (p *Plan) maskUnscopedLists() {
	masked := []string{}
	for _, st := range p.Chain.Steps {
		if !chain.IsReadOnlyCall(st.Call) || effectOutcome(st) != outcomeSuccess || !listUnscoped(st) {
			continue
		}
		m, err := p.cat.Lookup(st.Call)
		if err != nil {
			continue
		}
		list := repeatedMessageField(m)
		if list == nil {
			continue
		}
		if !containsString(st.Volatile, list.Name) {
			st.Volatile = append(st.Volatile, list.Name)
		}
		masked = append(masked, st.ID)
	}
	if len(masked) == 0 {
		return
	}
	p.note("%s %s everything the backend holds, which other runs add to, so %s %s its list volatile: verify and "+
		"the confirm summary tolerate the list growing (5 item(s) -> 112 item(s)) instead of reporting drift every run, "+
		"a list that came back empty is still reported, and the includes: expectations, which a volatile path does not "+
		"mask, fail the run when a fixture this run created is missing, naming its id", strings.Join(masked, ", "),
		pluralVerb(len(masked), "lists", "list"), pluralVerb(len(masked), "it", "each"), pluralVerb(len(masked), "declares", "declare"))
}
