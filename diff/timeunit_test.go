package diff_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func loginSpot(at time.Time, body string) *store.SafeSpot {
	return &store.SafeSpot{Chain: "thing-flow", RunID: at.UTC().Format("20060102T150405Z") + "-aaaa", Steps: []*runner.StepRecord{
		{ID: "login", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(body)},
	}}
}

func loginRun(at time.Time, body string) *runner.Record {
	rec := runOf(at.UTC().Format("20060102T150405Z")+"-bbbb", stepAs("login", runner.StatusPassed, body))
	rec.StartedAt = at
	rec.DurationMS = 20
	return rec
}

func TestTimestampMaskingKeepsUnitAndWindow(t *testing.T) {
	then := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	now := time.Now().Truncate(time.Second)
	secs := func(at time.Time) string { return fmt.Sprint(at.Add(time.Hour).Unix()) }
	spotBody := fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(then), then.Format(time.RFC3339), secs(then))
	same := fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), now.Format(time.RFC3339), secs(now))
	rep := diff.Compare(loginSpot(then, spotBody), loginRun(now, same))
	if !rep.Clean() || rep.Masked != 3 {
		t.Fatalf("now-ish timestamps of the same unit differ every run and must be masked, masked=%d:\n%s", rep.Masked, rep.Text())
	}
	ms := now.Add(time.Hour).UnixMilli()
	for name, tc := range map[string]struct{ body, line string }{
		"seconds to milliseconds": {fmt.Sprintf(`{"expires_at":%d,"issued_at":"%s","expiresAt":"%s"}`, ms, now.Format(time.RFC3339), secs(now)),
			"expires_at changed unit: seconds -> milliseconds"},
		"int64 text seconds to milliseconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%d"}`, secs(now), now.Format(time.RFC3339), ms),
			"expiresAt changed unit: seconds -> milliseconds"},
		"int64 text seconds to microseconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%d"}`, secs(now), now.Format(time.RFC3339), now.UnixMicro()),
			"expiresAt changed unit: seconds -> microseconds"},
		"seconds to RFC3339": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), now.Format(time.RFC3339), now.Format(time.RFC3339)),
			"expiresAt changed unit: seconds -> RFC3339 text"},
		"RFC3339 to seconds": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), secs(now), secs(now)),
			"issued_at changed unit: RFC3339 text -> seconds"},
		"seconds far in the future": {fmt.Sprintf(`{"expires_at":%d,"issued_at":"%s","expiresAt":"%s"}`, now.AddDate(150, 0, 0).Unix(), now.Format(time.RFC3339), secs(now)),
			"outside the time window of this run"},
		"RFC3339 far in the past": {fmt.Sprintf(`{"expires_at":%s,"issued_at":"%s","expiresAt":"%s"}`, secs(now), "1999-01-01T00:00:00Z", secs(now)),
			"issued_at is 1999-01-01T00:00:00Z, outside the time window"},
	} {
		rep := diff.Compare(loginSpot(then, spotBody), loginRun(now, tc.body))
		if rep.Clean() || rep.Counted() == 0 {
			t.Errorf("%s: a timestamp that changed unit or left the run's time window is a change, got none:\n%s", name, rep.Text())
		}
		if !strings.Contains(rep.Text(), tc.line) {
			t.Errorf("%s: verify must say %q:\n%s", name, tc.line, rep.Text())
		}
		if strings.Contains(rep.MaskedList(), "expires_at ("+secs(then)+" -> "+fmt.Sprint(ms)) {
			t.Errorf("%s: a unit change must not be listed as masked:\n%s", name, rep.MaskedList())
		}
		runs := diff.CompareRuns(loginRun(then, spotBody), loginRun(now, tc.body))
		if runs.Same() || !strings.Contains(runs.Text(), tc.line) {
			t.Errorf("%s: shrt diff must show the change and say %q:\n%s", name, tc.line, runs.Text())
		}
	}
}
