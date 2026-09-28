package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestALineDroppedFromLinesOfOneProductIsNamedDropped(t *testing.T) {
	spot := &store.SafeSpot{Chain: "orders", RunID: "spot", Steps: []*runner.StepRecord{
		stepAs("fetch", runner.StatusPassed, `{"order":{"lines":[{"id_product":"prd-aaaa1111aaaa","qty":6},{"id_product":"prd-aaaa1111aaaa","qty":5}]}}`),
	}}
	rec := runOf("run", stepAs("fetch", runner.StatusFailed, `{"order":{"lines":[{"id_product":"prd-aaaa1111aaaa","qty":6}]}}`))
	text := diff.Compare(spot, rec).Text()
	if !strings.Contains(text, "(0 added, 1 dropped, by id_product; dropped prd-aaaa1111aaaa)") {
		t.Fatalf("a repeated key is matched by count, so the missing line is dropped:\n%s", text)
	}
	if strings.Contains(text, "another order") {
		t.Fatalf("a dropped line is not a reorder:\n%s", text)
	}
}
