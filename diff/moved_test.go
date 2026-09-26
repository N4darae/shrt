package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestAnItemMovedInAListThatAlsoChangedIsNamedMovedByItsID(t *testing.T) {
	reversedAndRepriced := strings.ReplaceAll(listRunReversed, `"1250"`, `"999"`)
	rep := diff.Compare(listSpot(), listRun(reversedAndRepriced, nil))
	if rep.Class(diff.Change{Step: "list", Path: "products.0.price_minor"}) == "order changed" {
		t.Fatalf("a reorder with a changed price is no pure reorder")
	}
	if !rep.Moved("list", "products.0.price_minor") || !rep.Moved("list", "products.1.sku") {
		t.Errorf("the item the safe spot held at each position now sits at another one")
	}
	inPlace := strings.ReplaceAll(listSpotList, `"1250"`, `"999"`)
	inPlace = strings.ReplaceAll(strings.ReplaceAll(inPlace, "prd-bee2dc1a437c", "prd-1bcacf4cff60"), "prd-f9ef06e736d5", "prd-0066f622803d")
	rep = diff.Compare(listSpot(), listRun(inPlace, nil))
	if rep.Moved("list", "products.1.price_minor") {
		t.Errorf("an item repriced in place did not move")
	}
}
