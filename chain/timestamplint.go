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
		}
	}
	return false
}

func lintUnassertedTimestamps(c *Chain, methods map[string]*catalog.Method) []Issue {
	missing := []string{}
	example := ""
	for _, s := range c.Steps {
		m := methods[s.ID]
		if m == nil || expectsRefusal(s) {
			continue
		}
		for _, path := range TimestampFields(m) {
			if assertsPath(s, path) {
				continue
			}
			missing = append(missing, fmt.Sprintf("%s (%s)", path, s.ID))
			if example == "" {
				example = path
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Issue{{Severity: SeverityWarn, Kind: KindUnassertedTimestamp, Message: fmt.Sprintf(
		"no expectation reads the timestamp field(s) %s, and verify masks a timestamp's value as volatile, so a clock "+
			"value in the wrong unit, zone or offset (milliseconds for seconds, a token that expires at once) passes every "+
			"check. Assert a range the rpc promises: %s within: {of: \"${nowunix+3600}\", by: 5} for an hour-long expiry, "+
			"or gte: \"${nowunix}\" / lte: \"${nowunix}\" for a stamp that must not be in the past or the future",
		listSome(missing, 6), example)}}
}
