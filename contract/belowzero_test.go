package contract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestAPlanThatWouldTakeStockBelowZeroSaysWhatIsMissing(t *testing.T) {
	cat, _ := shopDemo(t)
	lib := shopDemoEdited(t, func(name, body string) string {
		return strings.Replace(body, "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1)
	})
	for _, target := range []string{"ConfirmOrder", "ListOrders"} {
		p, err := contract.BuildPlanFor([]string{target}, lib, cat, "shopdemo")
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range p.Chain.Steps {
			for _, e := range st.Expect {
				if strings.HasSuffix(e.Path, "qty_on_hand") && strings.HasPrefix(fmt.Sprint(e.Equals), "-") {
					t.Fatalf("%s: step %s asserts %s equals %v", target, st.ID, e.Path, e.Equals)
				}
			}
		}
		want := "below zero, since nothing before it adds any, so no level is asserted after it; add needs: [shop.catalog.v1.StockService/AddStock] to the contract of ConfirmOrder"
		if gaps := strings.Join(p.GapNotes(), "\n"); !strings.Contains(gaps, want) {
			t.Fatalf("%s: want gap %q in:\n%s", target, want, gaps)
		}
	}
}
