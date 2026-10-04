package chain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

const KindUnassertedTimestamp = "unasserted-timestamp"

func IsTimestampName(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_at"), strings.HasSuffix(lower, "_time"), strings.Contains(lower, "timestamp"),
		strings.HasPrefix(lower, "expires"), strings.HasPrefix(lower, "expiry"),
		name != "At" && strings.HasSuffix(name, "At"), name != "Time" && strings.HasSuffix(name, "Time"):
		return true
	}
	return false
}

func IsExpiryName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "expire") || strings.Contains(lower, "expiry")
}

func IsCreationStampName(name string) bool {
	lower := strings.ToLower(name)
	return slices.ContainsFunc([]string{"created", "issued", "inserted", "registered"}, func(w string) bool { return strings.HasPrefix(lower, w) })
}

func isTimestampField(f *catalog.Field) bool {
	if f == nil || f.Repeated || f.MapKey != "" || !IsTimestampName(f.Name) {
		return false
	}
	switch {
	case f.Message == "google.protobuf.Timestamp", IsNumericKind(f.Kind), f.Kind == "string":
		return true
	}
	return false
}

func TimestampFields(m *catalog.Method) []string {
	if m == nil {
		return nil
	}
	out := []string{}
	for _, f := range m.Response().Fields {
		if isTimestampField(f) {
			out = append(out, f.Name)
			continue
		}
		if f.Kind != "message" || f.Repeated || f.MapKey != "" || f.Message == "google.protobuf.Timestamp" {
			continue
		}
		for _, sub := range f.Fields {
			if isTimestampField(sub) {
				out = append(out, f.Name+"."+sub.Name)
			}
		}
	}
	return out
}

func assertsPath(s *Step, path string) bool {
	want := namecase.Fold(path)
	return slices.ContainsFunc(s.Expect, func(e Expectation) bool {
		got := namecase.Fold(strings.Join(SplitPath(e.Path), "."))
		return got == want || e.Exists != nil && !*e.Exists && strings.HasPrefix(want, got+".")
	})
}

func expectsRefusal(s *Step) bool {
	return slices.ContainsFunc(s.Expect, func(e Expectation) bool {
		path := strings.Join(SplitPath(e.Path), ".")
		if strings.HasPrefix(path, TransportPrefix+".") {
			return e.Equals != nil && stringify(e.Equals) != TransportOK
		}
		return path == EnvelopePath() && (e.Equals != nil && stringify(e.Equals) != EnvelopeOK() || e.NotEqual != nil && stringify(e.NotEqual) == EnvelopeOK())
	})
}

func StampSource(s *Step, path string, earlier map[string]*catalog.Method) string {
	want := namecase.Fold(path)
	for _, ref := range s.References() {
		src, rest, _ := strings.Cut(strings.TrimSpace(ref), ".")
		if src == "steps" {
			src, _, _ = strings.Cut(rest, ".")
		}
		m := earlier[src]
		if m == nil || src == s.ID {
			continue
		}
		if slices.ContainsFunc(TimestampFields(m), func(p string) bool { return namecase.Fold(p) == want }) {
			return src
		}
	}
	return ""
}

func timestampHint(s *Step, path string, earlier map[string]*catalog.Method) string {
	last := path[strings.LastIndex(path, ".")+1:]
	switch {
	case IsExpiryName(last):
		return `within: {of: "${nowunix+3600}", by: 5} for an hour-long expiry, or gte: "${nowunix}"`
	case IsCreationStampName(last):
		if src := StampSource(s, path, earlier); src != "" {
			return fmt.Sprintf("equals: ${%s.%s}", src, path)
		}
	}
	return `within: {of: "${nowunix}", by: 300}`
}

const unassertedTimestampWhy = "verify masks timestamps, so only an expectation catches one in the wrong unit or offset (PLAYBOOK.md §4)"

func lintUnassertedTimestamps(c *Chain, methods map[string]*catalog.Method) []Issue {
	var order []string
	steps := map[string][]string{}
	fixes := map[string]string{}
	earlier := map[string]*catalog.Method{}
	for _, s := range c.Steps {
		m := methods[s.ID]
		if m != nil && !expectsRefusal(s) {
			for _, path := range TimestampFields(m) {
				if assertsPath(s, path) {
					continue
				}
				fix := timestampHint(s, path, earlier)
				key := path + "\x00" + fix
				if strings.HasPrefix(fix, "equals: ${") {
					key = path + "\x00read-back"
				}
				if steps[key] == nil {
					order = append(order, key)
					fixes[key] = fix
				}
				steps[key] = append(steps[key], s.ID)
			}
		}
		if m != nil {
			earlier[s.ID] = m
		}
	}
	var issues []Issue
	for _, key := range order {
		path, _, _ := strings.Cut(key, "\x00")
		ids, fix := steps[key], fixes[key]
		i := Issue{Step: ids[0], Severity: SeverityWarn, Kind: KindUnassertedTimestamp, Why: unassertedTimestampWhy,
			Message: fmt.Sprintf("timestamp %s unasserted; expect %s", path, fix)}
		if len(ids) > 1 {
			if strings.HasSuffix(key, "\x00read-back") {
				fix = fmt.Sprintf("equals: the stamp the step that created it received, e.g. %s %s", ids[0], fix)
			}
			i.Step = ""
			i.Message = fmt.Sprintf("timestamp %s unasserted at %d steps (%s); expect %s", path, len(ids), ListSome(ids, 3), fix)
		}
		issues = append(issues, i)
	}
	return issues
}
