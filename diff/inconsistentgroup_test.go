package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestIdsNotRenamedConsistentlyAtOneListPathAreOneLine(t *testing.T) {
	spotBatch := `{"results":[{"id_product":"prd-aaaa1111aaaa"},{"id_product":"prd-bbbb2222bbbb"}]}`
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("create_a", runner.StatusPassed, `{"product":{"id_product":"prd-aaaa1111aaaa"}}`),
		stepAs("create_b", runner.StatusPassed, `{"product":{"id_product":"prd-bbbb2222bbbb"}}`),
		stepAs("batch", runner.StatusPassed, spotBatch),
		stepAs("batch_2", runner.StatusPassed, spotBatch),
	}}
	runBatch := `{"results":[{"id_product":"prd-cccc3333cccc"},{"id_product":"prd-cccc3333cccc"}]}`
	sent := json.RawMessage(`{"lines":[{"id_product":"prd-cccc3333cccc"},{"id_product":"prd-dddd4444dddd"}]}`)
	rec := runOf("run",
		stepAs("create_a", runner.StatusPassed, `{"product":{"id_product":"prd-cccc3333cccc"}}`),
		stepAs("create_b", runner.StatusPassed, `{"product":{"id_product":"prd-dddd4444dddd"}}`),
		stepAs("batch", runner.StatusPassed, runBatch),
		stepAs("batch_2", runner.StatusPassed, runBatch))
	rec.Steps[2].Request, rec.Steps[3].Request = sent, sent
	text := diff.Compare(spot, rec).Text()
	if n := strings.Count(text, "not renamed consistently"); n != 1 {
		t.Fatalf("one line per list path, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "[batch, batch_2] changed    results[].id_product at 2 item(s)") ||
		!strings.Contains(text, "every item of results holds one value in each step, the request's lines.0.id_product") {
		t.Fatalf("the line names the steps, the path, the count and the one value every item holds:\n%s", text)
	}
}
