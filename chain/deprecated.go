package chain

import (
	"fmt"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/pathmask"
)

const KindDeprecated = "deprecated"

func lintDeprecated(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	if m.Deprecated() {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindDeprecated, Message: fmt.Sprintf(
			"calls %s, deprecated in the proto; move the step to its replacement before the rpc goes", m.FullName)})
	}
	in := catalog.DescribeMessage(m.Input()).Fields
	for _, path := range deprecatedBodyPaths(s.Body, "", in) {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindDeprecated, Message: fmt.Sprintf(
			"body field %q is deprecated in %s: the backend may stop reading it", path, m.Input().Name())})
	}
	out := m.Response().Fields
	seen := map[string]bool{}
	for _, e := range s.Expect {
		if IsTransportPath(e.Path) || seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		if deprecatedAlong(out, SplitPath(e.Path)) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindDeprecated, Message: fmt.Sprintf(
				"expect %s reads a field deprecated in %s: the backend may stop sending it", e.Path, m.Output().Name())})
		}
	}
	return issues
}

func deprecatedBodyPaths(v any, path string, fields []*catalog.Field) []string {
	out := []string{}
	switch t := v.(type) {
	case map[string]any:
		for _, k := range SortedKeys(t) {
			next := pathmask.Join(path, k)
			if deprecatedAlong(fields, SplitPath(next)) {
				out = append(out, next)
				continue
			}
			out = append(out, deprecatedBodyPaths(t[k], next, fields)...)
		}
	case []any:
		for i, item := range t {
			out = append(out, deprecatedBodyPaths(item, fmt.Sprintf("%s.%d", path, i), fields)...)
		}
	}
	return out
}

func deprecatedAlong(fields []*catalog.Field, segs []string) bool {
	for i := range segs {
		if f, ok := catalog.FieldAt(fields, segs[:i+1]); ok && f.Deprecated && !IsDigits(segs[i]) {
			return true
		}
	}
	return false
}
