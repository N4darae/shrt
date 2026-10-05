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
	for _, list := range SortedKeys(counted) {
		n := counted[list]
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnscopedCount, Message: fmt.Sprintf(
			"asserts %s holds at most %d item(s), but the request is not scoped to this run; scope it by ${vars.tag} "+
				"or a parent id, or assert a lower bound only (%s.%d exists: true)", list, n, list, max(n-1, 0)),
			Why: "an unscoped list holds everything the backend has, so the count fails on the second run against the same database"})
	}
	return issues
}
