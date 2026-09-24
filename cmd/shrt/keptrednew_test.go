package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestANotAsPinnedRunLeadsWithTheNewFailure(t *testing.T) {
	finding := runner.NewFailurePrefix + "create_order order.total_minor want=1250 got=500"
	rec := &runner.Record{Chain: "red", Status: runner.StatusFailed, KeptRed: runner.KeptRedNotAsPinned,
		FailedSteps: []string{"create_order", "confirm_order"},
		KeptRedNote: "kept_red pins confirm_order status.code, but step \"create_order\" failed where nothing is pinned: order.total_minor equals want=1250 got=500",
		KeptRedNew:  finding}
	out := summary(rec, false)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[1]) != finding {
		t.Fatalf("the line after the verdict must be the new failure %q:\n%s", finding, out)
	}
	if !strings.Contains(lines[0], "NOT AS PINNED") {
		t.Fatalf("the verdict line must say the kept red chain did not fail as pinned:\n%s", out)
	}
	err := runVerdict(rec)
	if err == nil || !strings.Contains(err.Error(), finding) {
		t.Fatalf("the exit error must carry the new failure %q, got %v", finding, err)
	}
}
