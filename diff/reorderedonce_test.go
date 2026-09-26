package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
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

func TestAListReorderedAtSeveralStepsIsExplainedOncePerPath(t *testing.T) {
	spot := listSpot()
	spot.Steps = append(spot.Steps, &runner.StepRecord{ID: "list_2", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(listSpotList)})
	rec := listRun(listRunReversed, nil)
	rec.Steps = append(rec.Steps, stepAs("list_2", runner.StatusPassed, listRunReversed))
	rep := diff.Compare(spot, rec)
	rep.SeparateInput(spot, rec, nil, diff.Fixtures{})
	text := rep.Text()
	if n := strings.Count(text, "same items in another order"); n != 1 || !strings.Contains(text, "  list, list_2 products: same items in another order") ||
		!strings.Contains(text, "declare `unordered: [products]` on those steps") {
		t.Fatalf("one paragraph for the path, naming both steps, got %d:\n%s", n, text)
	}
	if got := rep.ReorderedLists(); len(got) != 1 || got[0] != "list, list_2 products" {
		t.Fatalf("one reordered list per path, got %v", got)
	}
}

func TestAStatusChangeIsNotListedBesideTheChangesAtItsStep(t *testing.T) {
	spot := spotOf(nil, &runner.StepRecord{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusPassed, Response: json.RawMessage(`{"n":"3"}`)})
	rec := recOf(&runner.StepRecord{ID: "get", Call: "ThingService/Fetch", Status: runner.StatusFailed, Response: json.RawMessage(`{"n":"2"}`)})
	text := diff.Compare(spot, rec).Text()
	if strings.Contains(text, "want=passed got=failed") || !strings.Contains(text, "[get] changed    n want=3 got=2") {
		t.Fatalf("the changed field says why the step failed; its status line only repeats it:\n%s", text)
	}
}
