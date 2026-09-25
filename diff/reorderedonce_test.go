package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestAReorderedListIsNamedOnceAsStepThenPath(t *testing.T) {
	rep := diff.Compare(listSpot(), listRun(listRunReversed, nil))
	rep.SeparateInput(listSpot(), listRun(listRunReversed, nil), nil, diff.Fixtures{})
	text := rep.Text()
	if n := strings.Count(text, "same items in another order"); n != 1 {
		t.Fatalf("the reordered list must be named once, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "list products: same items in another order") {
		t.Fatalf("the line must read `<step> <path>: same items in another order`:\n%s", text)
	}
	if len(rep.Reordered) != 1 {
		t.Fatalf("one reordered list, got %v", rep.Reordered)
	}
}

func TestAReorderedListIsClassedOrderChangedWhateverElseDrifted(t *testing.T) {
	for _, other := range []bool{false, true} {
		spot, rec := listSpot(), listRun(listRunReversed, nil)
		if other {
			rec.Steps[0].Response = []byte(`{"id_product":"prd-0066f622803d","sku":"sku-w-changed"}`)
		}
		rep := diff.Compare(spot, rec)
		classes := map[string]string{}
		for _, c := range rep.Changes {
			classes[c.Step] = rep.Class(c)
		}
		if classes["list"] != "order changed" || other && classes["create_w"] != "regression" {
			t.Errorf("with another change %v: the reorder is order changed, the other a regression, got %v", other, classes)
		}
	}
}
