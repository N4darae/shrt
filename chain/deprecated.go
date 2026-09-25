package chain

import (
	"fmt"
	"sort"

	"github.com/N4darae/shrt/catalog"
)

const KindDeprecated = "deprecated"

func lintDeprecated(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	if m.Deprecated() {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindDeprecated, Message: fmt.Sprintf(
			"calls %s, which the proto marks deprecated (option deprecated = true): it may be removed, and this step with it; "+
				"move the step to its replacement, or keep it until the rpc goes and expect it then", m.FullName)})
	}
	in := catalog.DescribeMessage(m.Input()).Fields
	for _, path := range deprecatedBodyPaths(s.Body, "", in) {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindDeprecated, Message: fmt.Sprintf(
			"body field %q is deprecated in %s (option deprecated = true): the backend may stop reading it", path, m.Input().FullName())})
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
				"expect on %q reads a field %s marks deprecated (option deprecated = true): the backend may stop sending it", e.Path, m.Output().FullName())})
		}
	}
	return issues
}

func deprecatedBodyPaths(v any, path string, fields []*catalog.Field) []string {
	out := []string{}
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := k
			if path != "" {
				next = path + "." + k
			}
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
		if f, ok := catalog.FieldAt(fields, segs[:i+1]); ok && f.Deprecated && !isIndexSegment(segs[i]) {
			return true
		}
	}
	return false
}
