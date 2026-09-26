package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func confirmTwiceFixture() (*chain.Chain, func(string) []chain.Prereq) {
	c := &chain.Chain{
		Name: "confirm-twice",
		Steps: []*chain.Step{
			{ID: "create_product", Call: "ProductService/CreateProduct"},
			{ID: "add_stock", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}", "qty": "8"}},
			{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
			{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
			{ID: "confirm_twice", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
		},
	}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "OrderService/ConfirmOrder" {
			return []chain.Prereq{{RPC: "StockService/AddStock", Edge: "needs"}}
		}
		return nil
	}
	return c, prereqs
}

func TestPinModeLeavesANeedsPrerequisiteTheSourceRunPerformedToThatRun(t *testing.T) {
	c, prereqs := confirmTwiceFixture()
	res, err := chain.Slice(c, "confirm_twice", chain.SliceOptions{
		Mode: chain.SliceModePin, RunID: "r1", Prereqs: prereqs,
		Value:     func(ref string) (any, bool) { return "id-1", true },
		Performed: func(id string) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keptIDs(res), ","); got != "confirm_twice" {
		t.Fatalf("add_stock already ran in r1 against the pinned product; re-sending it adds stock again and changes the state being reproduced. kept %s", got)
	}
	if len(res.Satisfied) != 1 || res.Satisfied[0].ID != "add_stock" || res.Satisfied[0].RPC != "StockService/AddStock" {
		t.Fatalf("the prerequisite left to the source run must be reported: %+v", res.Satisfied)
	}
	dropped := map[string]bool{}
	for _, d := range res.DroppedWrites {
		dropped[d.ID] = true
	}
	if !dropped["add_stock"] || !res.UnderIncluded {
		t.Fatalf("add_stock is still a write the slice does not send: %+v", res.DroppedWrites)
	}
	if !strings.Contains(res.Chain.Description, "add_stock") || !strings.Contains(res.Chain.Description, "r1") {
		t.Fatalf("the description must say which prerequisite the slice relies on run r1 for:\n%s", res.Chain.Description)
	}
}

func TestPinModeKeepsANeedsPrerequisiteTheSourceRunDidNotPerform(t *testing.T) {
	c, prereqs := confirmTwiceFixture()
	res, err := chain.Slice(c, "confirm_twice", chain.SliceOptions{
		Mode: chain.SliceModePin, RunID: "r1", Prereqs: prereqs,
		Value:     func(ref string) (any, bool) { return "id-1", true },
		Performed: func(id string) bool { return id != "add_stock" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keptIDs(res), ","); got != "add_stock,confirm_twice" {
		t.Fatalf("no state from add_stock exists when the source run did not perform it, so the slice must send it; kept %s", got)
	}
}
