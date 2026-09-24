package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestATargetThatDiffersOnlyInCaseIsNotReportedAsAnotherTarget(t *testing.T) {
	spot := &store.SafeSpot{Chain: "c", RunID: "a", Target: "HTTP://127.0.0.1:18121"}
	rec := &runner.Record{Chain: "c", RunID: "b", Target: "http://127.0.0.1:18121/"}
	if rep := diff.CompareMasking(spot, rec, nil); rep.SafeSpotTarget != "" || rep.RunTarget != "" {
		t.Fatalf("HTTP:// and http:// name one target, got %q vs %q", rep.SafeSpotTarget, rep.RunTarget)
	}
	a := &runner.Record{Chain: "c", RunID: "a", Target: "http://API.example.test"}
	b := &runner.Record{Chain: "c", RunID: "b", Target: "http://api.example.test"}
	if rep := diff.CompareRuns(a, b); rep.TargetA != "" || rep.TargetB != "" {
		t.Fatalf("host case does not change the target, got %q vs %q", rep.TargetA, rep.TargetB)
	}
}
