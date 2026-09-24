package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestAnIdOfAnotherKindIsDrift(t *testing.T) {
	want := `{"order":{"id_customer":"cus-5656cb156e31"}}`
	for name, got := range map[string]string{
		"product id in a customer field": `{"order":{"id_customer":"prd-000000000000"}}`,
		"prefix dropped":                 `{"order":{"id_customer":"5656cb156e31"}}`,
		"underscore prefix of another":   `{"order":{"id_customer":"prd_5656cb156e31"}}`,
	} {
		rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, got)))
		if rep.Clean() {
			t.Errorf("%s: an id of another kind is not the same id refreshed, got no drift:\n%s", name, rep.Text())
		}
		runs := diff.CompareRuns(runOf("a", stepAs("fetch_order", runner.StatusPassed, want)), runOf("b", stepAs("fetch_order", runner.StatusPassed, got)))
		if runs.Same() {
			t.Errorf("%s: shrt diff must show it too:\n%s", name, runs.Text())
		}
	}
	rep := diff.Compare(orderSpot(want), runOf("run", stepAs("fetch_order", runner.StatusPassed, `{"order":{"id_customer":"cus-0123456789ab"}}`)))
	if !rep.Clean() || rep.Masked != 1 {
		t.Fatalf("a fresh id of the same kind is masked, masked=%d:\n%s", rep.Masked, rep.Text())
	}
}

func TestMaskedListingNamesShapeMaskedValues(t *testing.T) {
	want := `{"order":{"id_order":"ord-819dac5f23ba","created_at":"2026-09-24T15:59:18Z","note":"a"}}`
	got := `{"order":{"id_order":"ord-0123456789ab","created_at":"2026-09-25T10:00:00Z","note":"b"}}`
	spot := orderSpot(want)
	spot.Volatile = []string{"**.note"}
	rep := diff.Compare(spot, runOf("run", stepAs("fetch_order", runner.StatusPassed, got)))
	list := rep.MaskedList()
	for _, s := range []string{
		"fetch_order order.id_order (ord-819dac5f23ba -> ord-0123456789ab)",
		"fetch_order order.created_at (2026-09-24T15:59:18Z -> 2026-09-25T10:00:00Z)",
		"fetch_order order.note (a -> b)",
	} {
		if !strings.Contains(list, s) {
			t.Errorf("-masked must list %q:\n%s", s, list)
		}
	}
	if len(rep.ShapeMasked) != 2 {
		t.Errorf("shape_masked = %+v, want the id and the timestamp", rep.ShapeMasked)
	}
}
