package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestVerifyWarnsWhenAVolatilePatternMasksAWholeResponse(t *testing.T) {
	spot := orderSpot(`{"order":{"total":"500"}}`)
	rec := runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"total":"999"}}`))
	rep := diff.CompareMasking(spot, rec, []string{"**"})
	text := rep.Text()
	if len(rep.FullyMasked) != 1 || rep.FullyMasked[0] != "fetch_order" {
		t.Fatalf("every field of fetch_order is volatile: %+v", rep.FullyMasked)
	}
	if !strings.Contains(text, "WARNING: every response field of step(s) fetch_order") || !strings.Contains(text, "no drift") {
		t.Fatalf("a no-drift verdict over a fully masked response must carry the warning diff and confirm give:\n%s", text)
	}
}
