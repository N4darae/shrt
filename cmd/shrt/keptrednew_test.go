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

func TestANotAsPinnedSummaryPrintsEachReasonOnceAndLeadsWithTheFixtureLine(t *testing.T) {
	notSent := `not sent: ${create_order.order.id_order} reads step "create_order", which was refused`
	rec := &runner.Record{Chain: "red", Status: runner.StatusFailed, KeptRed: runner.KeptRedNotAsPinned,
		FailedSteps: []string{"create_order", "confirm_order"},
		Failure:     "kept_red: ran every step, as -keep-going does; 2 of 3 steps did not pass\nstep \"confirm_order\": " + notSent,
		KeptRedNote: `kept_red pins confirm_order status.code, but step "confirm_order" was not sent (why is on its line), so its pinned failure was not seen`,
		KeptRedNew:  runner.NewFailurePrefix + "create_order refused at transport: a; b; c; d"}
	out := runSummary(nil, rec, false, true, "fixture collision: step \"create_order\" ...")
	if strings.Contains(out, notSent) {
		t.Errorf("with the step lines shown above, the summary does not repeat their reasons:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 || !strings.HasPrefix(strings.TrimSpace(lines[2]), "fixture collision") {
		t.Errorf("the fixture line comes right after the verdict and the new failure:\n%s", out)
	}
	if quiet := runSummary(nil, rec, false, false, ""); !strings.Contains(quiet, notSent) {
		t.Errorf("under -quiet no step line was printed, so the summary keeps the reason:\n%s", quiet)
	}
	if err := runVerdict(rec); err == nil || !strings.Contains(err.Error(), "and 3 more (listed above)") {
		t.Errorf("the exit message stays short: %v", err)
	}
}
