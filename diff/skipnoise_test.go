package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestVerifyPrintsARepeatedNotSentReasonOnce(t *testing.T) {
	reason := `reads step "create", which was refused in-band (status.code = REJECTED): a refused call's response decodes to zero values, and the request would carry them as if they were real.`
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot"}
	rec := runOf("run", stepAs("create", runner.StatusFailed, `{"n":2}`))
	spot.Steps = append(spot.Steps, &runner.StepRecord{ID: "create", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)})
	for i, id := range []string{"a", "b", "c"} {
		spot.Steps = append(spot.Steps, &runner.StepRecord{ID: id, Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":1}`)})
		skipped := stepAs(id, runner.StatusSkipped, "")
		skipped.Error = "not sent: ${create.f" + string(rune('0'+i)) + "} " + reason
		rec.Steps = append(rec.Steps, skipped)
	}
	text := diff.Compare(spot, rec).Text()
	if n := strings.Count(text, "decodes to zero values"); n != 1 {
		t.Fatalf("the reason is printed once and referred to after, printed %d times:\n%s", n, text)
	}
	if !strings.Contains(text, "${create.f2}") {
		t.Fatalf("each skipped step keeps its own reference:\n%s", text)
	}
}
