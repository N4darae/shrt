package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func reorderedFailingRun() *runner.Record {
	rec := listRun(listRunReversed, nil)
	list := rec.Steps[2]
	list.Status = runner.StatusFailed
	list.Expect = []chain.ExpectResult{{Path: "products.0.sku", Rule: "equals", Want: "sku-g", Got: "sku-w"}}
	rec.Status = runner.StatusFailed
	return rec
}

func TestAReorderWithAnOrderSensitiveExpectationIsAnOrderChange(t *testing.T) {
	rec := reorderedFailingRun()
	rep := diff.Compare(listSpot(), rec)
	rep.SeparateInput(listSpot(), rec, nil, diff.Fixtures{})
	text := rep.Text()
	if !rep.OnlyReordered() {
		t.Fatalf("the list holds the same items and only the expectation reading it by position failed: an order change:\n%s", text)
	}
	if strings.Contains(text, "not renamed consistently") {
		t.Fatalf("items that were merely reordered get no id-renaming lines:\n%s", text)
	}
	if !strings.Contains(text, "products.0.sku want=sku-g got=sku-w") {
		t.Fatalf("the failed expectation is named:\n%s", text)
	}
	if rep.FirstFailure != "step 0 list (failed): expectation failed: products.0.sku want=sku-g got=sku-w" {
		t.Fatalf("the first failing step names the expectation that failed, not only its status:\n%s", text)
	}
}
