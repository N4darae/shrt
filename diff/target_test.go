package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVerifyReportSaysTheReplayRanAgainstAnotherTarget(t *testing.T) {
	body := `{"order":{"total":"500"}}`
	spot := orderSpot(body)
	spot.Target = "http://127.0.0.1:18199"
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, body))
	rec.Target = "http://prod.example"
	rep := diff.Compare(spot, rec)
	if rep.SafeSpotTarget != "http://127.0.0.1:18199" || rep.RunTarget != "http://prod.example" {
		t.Fatalf("targets not reported: %+v", rep)
	}
	text := rep.Text()
	if !strings.HasPrefix(text, "targets differ: safe spot http://127.0.0.1:18199, this run http://prod.example") {
		t.Fatalf("a different target must lead the report:\n%s", text)
	}
	rec.Target = spot.Target
	if text := diff.Compare(spot, rec).Text(); strings.Contains(text, "targets differ") {
		t.Fatalf("same target, nothing to say:\n%s", text)
	}
}
