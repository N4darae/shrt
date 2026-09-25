package chain

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

const KindUnassertedTimestamp = "unasserted-timestamp"

func IsTimestampName(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_at"), strings.HasSuffix(lower, "_time"), strings.Contains(lower, "timestamp"),
		strings.HasPrefix(lower, "expires"), strings.HasPrefix(lower, "expiry"):
		return true
	}
	for _, suffix := range []string{"At", "Time"} {
		if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func IsExpiryName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "expire") || strings.Contains(lower, "expiry")
}

func IsCreationStampName(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range []string{"created", "issued", "inserted", "registered"} {
		if strings.HasPrefix(lower, w) {
			return true
		}
	}
	return false
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
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
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
	for _, e := range s.Expect {
		if namecase.Fold(strings.Join(SplitPath(e.Path), ".")) == want {
			return true
		}
	}
	return false
}

func assertsAbsent(s *Step, path string) bool {
	want := namecase.Fold(path)
	for _, e := range s.Expect {
		if e.Exists == nil || *e.Exists {
			continue
		}
		got := namecase.Fold(strings.Join(SplitPath(e.Path), "."))
		if got == want || strings.HasPrefix(want, got+".") {
			return true
		}
	}
	return false
}

func expectsRefusal(s *Step) bool {
	for _, e := range s.Expect {
		path := strings.Join(SplitPath(e.Path), ".")
		switch {
		case strings.HasPrefix(path, TransportPrefix+"."):
			if e.Equals != nil && stringify(e.Equals) != TransportOK {
				return true
			}
		case path == EnvelopePath() && EnvelopeOK() != "":
			if e.Equals != nil && stringify(e.Equals) != EnvelopeOK() {
				return true
			}
			if e.NotEqual != nil && stringify(e.NotEqual) == EnvelopeOK() {
				return true
			}
		}
	}
	return false
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
		for _, p := range TimestampFields(m) {
			if namecase.Fold(p) == want {
				return src
			}
		}
	}
	return ""
}

func timestampHint(s *Step, path string, earlier map[string]*catalog.Method) string {
	last := path
	if i := strings.LastIndex(path, "."); i >= 0 {
		last = path[i+1:]
	}
	switch {
	case IsExpiryName(last):
		return fmt.Sprintf(`%s %s within: {of: "${nowunix+3600}", by: 5} for an hour-long expiry, or gte: "${nowunix}"`, s.ID, path)
	case IsCreationStampName(last):
		if src := StampSource(s, path, earlier); src != "" {
			return fmt.Sprintf("%s %s equals: ${%s.%s}", s.ID, path, src, path)
		}
	}
	return fmt.Sprintf(`%s %s within: {of: "${nowunix}", by: 300}`, s.ID, path)
}

func lintUnassertedTimestamps(c *Chain, methods map[string]*catalog.Method) []Issue {
	missing := []string{}
	hints := []string{}
	earlier := map[string]*catalog.Method{}
	for _, s := range c.Steps {
		m := methods[s.ID]
		if m != nil && !expectsRefusal(s) {
			for _, path := range TimestampFields(m) {
				if assertsPath(s, path) || assertsAbsent(s, path) {
					continue
				}
				missing = append(missing, fmt.Sprintf("%s (%s)", path, s.ID))
				hints = append(hints, timestampHint(s, path, earlier))
			}
		}
		if m != nil {
			earlier[s.ID] = m
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Issue{{Severity: SeverityWarn, Kind: KindUnassertedTimestamp, Message: fmt.Sprintf(
		"no expectation reads the timestamp field(s) %s, and verify masks a timestamp's value as volatile, so a clock "+
			"value in the wrong unit, zone or offset (milliseconds for seconds, a token that expires at once) passes every "+
			"check. Assert what each promises: %s. A stamp a call makes is now, within the chain's run and clock skew "+
			"(300s); a creation stamp read back equals the one the step that created it received; only an expiry sits its "+
			"lifetime ahead",
		listSome(missing, 6), strings.Join(firstN(hints, 6), "; "))}}
}

func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[:n]
}
