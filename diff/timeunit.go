package diff

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const TimeMaskMargin = 400 * 24 * time.Hour

type runWindow struct {
	from, to time.Time
}

func (w *runWindow) holds(at time.Time) bool {
	if w == nil || w.from.IsZero() {
		return true
	}
	return !at.Before(w.from.Add(-TimeMaskMargin)) && !at.After(w.to.Add(TimeMaskMargin))
}

func recordWindow(rec *runner.Record) *runWindow {
	if rec == nil {
		return nil
	}
	from := rec.StartedAt
	if from.IsZero() {
		at, ok := runIDStamp(rec.RunID)
		if !ok {
			return nil
		}
		from = at
	}
	return &runWindow{from: from, to: from.Add(time.Duration(rec.DurationMS) * time.Millisecond)}
}

func spotWindow(spot *store.SafeSpot) *runWindow {
	if spot == nil {
		return nil
	}
	at, ok := runIDStamp(spot.RunID)
	if !ok {
		return nil
	}
	to := at
	for _, st := range spot.Steps {
		if st != nil {
			to = to.Add(time.Duration(st.LatencyMS) * time.Millisecond)
		}
	}
	return &runWindow{from: at, to: to.Add(time.Second)}
}

func runIDStamp(id string) (time.Time, bool) {
	stamp, _, _ := strings.Cut(id, "-")
	at, err := time.Parse("20060102T150405Z", stamp)
	return at, err == nil
}

type timeValue struct {
	unit string
	at   time.Time
}

const unitRFC3339 = "RFC3339 text"

var epochUnits = map[int]struct {
	name  string
	scale int64
}{
	10: {"seconds", 1e9},
	13: {"milliseconds", 1e6},
	16: {"microseconds", 1e3},
	19: {"nanoseconds", 1},
}

func epochDigits(v any) (string, bool) {
	switch t := v.(type) {
	case float64:
		if t <= 0 || t != math.Trunc(t) || t > 9.3e18 {
			return "", false
		}
		return strconv.FormatFloat(t, 'f', 0, 64), true
	case string:
		if t == "" || t[0] == '0' || !digitsOnly.MatchString(t) {
			return "", false
		}
		return t, true
	}
	return "", false
}

func numberDigits(v any) (int, bool) {
	s, ok := epochDigits(v)
	return len(s), ok
}

func timeOf(v any, named bool) (timeValue, bool) {
	if s, ok := v.(string); ok && isTimestamp(s) {
		at, _ := time.Parse(time.RFC3339Nano, s)
		return timeValue{unit: unitRFC3339, at: at}, true
	}
	if !named {
		return timeValue{}, false
	}
	digits, ok := epochDigits(v)
	if !ok {
		return timeValue{}, false
	}
	u, ok := epochUnits[len(digits)]
	if !ok {
		return timeValue{}, false
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return timeValue{}, false
	}
	perSec := int64(1e9) / u.scale
	return timeValue{unit: u.name, at: time.Unix(n/perSec, (n%perSec)*u.scale)}, true
}

func timeNamed(path string) bool {
	key := lastKey(path)
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, "_at") || strings.HasSuffix(lower, "_time") || strings.Contains(lower, "timestamp") ||
		camelSuffix(key, "At") || camelSuffix(key, "Time")
}

func unitName(v any, named bool) string {
	if t, ok := timeOf(v, named); ok {
		return t.unit
	}
	if n, ok := numberDigits(v); ok && named {
		return fmt.Sprintf("a %d-digit number", n)
	}
	return ""
}

func timeMismatch(path string, a, b any, wa, wb *runWindow) string {
	named := timeNamed(path)
	ta, okA := timeOf(a, named)
	tb, okB := timeOf(b, named)
	if !okA && !okB {
		return ""
	}
	if !okA || !okB || ta.unit != tb.unit {
		ua, ub := unitName(a, named), unitName(b, named)
		if ua == "" || ub == "" {
			return ""
		}
		return fmt.Sprintf("%s changed unit: %s -> %s", lastKey(path), ua, ub)
	}
	for _, side := range []struct {
		label string
		t     timeValue
		w     *runWindow
	}{{"the safe spot's run", ta, wa}, {"this run", tb, wb}} {
		if !side.w.holds(side.t.at) {
			return fmt.Sprintf("%s is %s, outside the time window of %s (%s, give or take %d days), so it is compared, not masked as a timestamp",
				lastKey(path), side.t.at.UTC().Format(time.RFC3339), side.label, side.w.from.UTC().Format(time.RFC3339), int(TimeMaskMargin.Hours()/24))
		}
	}
	return ""
}

func volatileIn(path string, a, b any, wa, wb *runWindow) (bool, string) {
	if why := timeMismatch(path, a, b, wa, wb); why != "" {
		return false, why
	}
	return looksVolatile(path, a, b), ""
}

func noteTimeUnit(c *Change) {
	if c.Detail != "" || (c.Kind != KindChanged && c.Kind != KindType) {
		return
	}
	if why := timeMismatch(c.Path, c.Want, c.Got, nil, nil); why != "" {
		c.Detail = why
	}
}
