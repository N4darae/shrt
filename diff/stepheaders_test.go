package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAStepHeaderEditIsAnInputChange(t *testing.T) {
	spot := orderSpot(`{"name":"Widget"}`)
	spot.Steps[0].Headers = map[string]string{}
	got := stepAs("fetch_order", runner.StatusPassed, `{"name":"Widget"}`)
	got.Headers = map[string]string{"X-Dry-Run": "1"}
	changes := diff.CompareRequests(spot, runOf("run", got), nil)
	if len(changes) != 1 || changes[0].Path != "headers.X-Dry-Run" || changes[0].Kind != diff.KindUnexpected {
		t.Fatalf("a header the confirmed run did not send is different input: %+v", changes)
	}
	spot.Steps[0].Headers = map[string]string{"X-Dry-Run": "0"}
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 1 || changes[0].Kind != diff.KindChanged {
		t.Fatalf("a header value that differs is different input: %+v", changes)
	}
	spot.Steps[0].Headers = nil
	if changes := diff.CompareRequests(spot, runOf("run", got), nil); len(changes) != 0 {
		t.Fatalf("a safe spot recorded before headers were recorded has nothing to compare: %+v", changes)
	}
}
