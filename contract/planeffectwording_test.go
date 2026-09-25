package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestTheNoteForAnUnstatedBatchIncreaseNamesTheWordingForAnIncrease(t *testing.T) {
	p, text := shopDemoMutated(t, contract.PlanOptions{}, func(rpcs map[string]*contract.RPCContract) {
		if c := rpcs["shop.catalog.v1.StockService/AddStockBatch"]; c != nil {
			c.Fields["lines"].Note = "each line adds its qty to that product, independently"
		}
	}, "AddStockBatch")
	notes := strings.Join(p.Notes, "\n")
	var line string
	for _, n := range p.Notes {
		if strings.HasPrefix(n, "AddStockBatch says nothing of qty_on_hand") {
			line = n
		}
	}
	if line == "" {
		t.Fatalf("the batch is no longer recognised, so the plan says what to state:\n%s\n%s", notes, text)
	}
	if !strings.Contains(line, `add effects: {qty_on_hand: {increase: lines.qty}}`) || strings.Contains(line, "decrease") {
		t.Fatalf("an increase is stated as one AddStock per line, not as a reservation:\n%s", line)
	}
}
