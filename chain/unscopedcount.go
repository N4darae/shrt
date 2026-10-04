package chain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

const KindUnscopedCount = "unscoped-count"

func listFieldsOf(m *catalog.Method) []string {
	out := []string{}
	if m == nil {
		return out
	}
	for _, f := range m.Response().Fields {
		if f.Repeated && f.Kind == "message" && f.MapKey == "" {
			out = append(out, f.Name)
		}
	}
	return out
}

func bodyScoped(v any) bool {
	scoped := false
	walkText(v, "", func(_, s string) { scoped = scoped || strings.Contains(s, "${") })
	return scoped
}

func lintUnscopedCount(s *Step, m *catalog.Method) []Issue {
	if s.AllowFail || !IsReadOnlyCall(s.Call) || bodyScoped(map[string]any(s.Body)) {
		return nil
	}
	lists := listFieldsOf(m)
	if len(lists) == 0 {
		return nil
	}
	counted := map[string]int{}
	for _, e := range s.Expect {
		if e.Exists == nil || *e.Exists {
			continue
		}
		for _, list := range lists {
			rest, ok := strings.CutPrefix(e.Path, list+".")
			if !ok {
				continue
			}
			if n, err := strconv.Atoi(rest); err == nil {
				counted[list] = n
			}
		}
	}
	issues := []Issue{}
	for _, list := range sortedKeys(counted) {
		n := counted[list]
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnscopedCount, Message: fmt.Sprintf(
			"asserts %s holds at most %d item(s) (%s.%d exists: false), but nothing in the request scopes the list to what "+
				"this run created (no field reads a var, a step or a generator), so it lists everything the backend holds and "+
				"fails on the second run against the same database. Scope it (a prefix built from ${vars.tag} that the fixtures "+
				"carry, or the id of a parent this run created), or assert a lower bound only (%s.%d exists: true)",
			list, n, list, n, list, max(n-1, 0))})
	}
	return issues
}
