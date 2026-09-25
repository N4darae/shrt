package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestRunDiffListsEachMaskedDifferenceWithWhatHidIt(t *testing.T) {
	a := runOf("a", stepAs("create", runner.StatusPassed,
		`{"product":{"id_product":"prd-0123456789ab","note":"x1","created_at":"2026-09-01T10:00:00Z"}}`))
	b := runOf("b", stepAs("create", runner.StatusPassed,
		`{"product":{"id_product":"prd-ba9876543210","note":"x2","created_at":"2026-09-02T10:00:00Z"}}`))
	a.Volatile, b.Volatile = []string{"**.created_at", "product.note"}, []string{"**.created_at", "product.note"}
	rep := diff.CompareRuns(a, b)
	if !rep.Same() {
		t.Fatalf("every difference is masked:\n%s", rep.Text())
	}
	masks := map[string]string{}
	for _, c := range rep.MaskedChanges {
		masks[c.Path] = c.Mask
	}
	if masks["product.created_at"] != "**.created_at" || masks["product.note"] != "product.note" {
		t.Fatalf("each value hidden by a volatile pattern names that pattern: %+v", rep.MaskedChanges)
	}
	if !strings.Contains(masks["product.id_product"], "shape") {
		t.Fatalf("an id masked by its shape says so: %+v", rep.MaskedChanges)
	}
	list := rep.MaskedList()
	for _, want := range []string{"product.created_at", "2026-09-01T10:00:00Z -> 2026-09-02T10:00:00Z", "hidden by **.created_at", "hidden by product.note"} {
		if !strings.Contains(list, want) {
			t.Fatalf("the masked list names %q:\n%s", want, list)
		}
	}
}
