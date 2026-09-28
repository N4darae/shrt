package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func aliasFixture() *chain.Chain {
	return &chain.Chain{
		Name: "order-flow",
		Steps: []*chain.Step{
			{ID: "add_stock_a", Call: "StockService/AddStock"},
			{ID: "add_stock_b", Call: "StockService/AddStock"},
			{ID: "create_order", Call: "OrderService/CreateOrder"},
			{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
			{ID: "cancel_order", Call: "OrderService/CancelOrder"},
		},
	}
}

func aliasPrereqs(rpc string) []chain.Prereq {
	if rpc == "OrderService/ConfirmOrder" {
		return []chain.Prereq{
			{RPC: "StockService/AddStock", Alias: "a", Edge: "needs"},
			{RPC: "StockService/AddStock", Alias: "b", Edge: "needs"},
		}
	}
	return nil
}

func TestSliceKeepsOneStepPerAliasedPrerequisite(t *testing.T) {
	res, err := chain.Slice(aliasFixture(), "confirm_order", chain.SliceOptions{Prereqs: aliasPrereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(keptIDs(res), ","), "add_stock_a,add_stock_b,create_order,confirm_order"; got != want {
		t.Fatalf("kept %s, want %s: two aliased needs are two producers, not the nearest call twice", got, want)
	}
	for _, k := range res.Kept {
		if k.ID == "add_stock_a" && k.Reason != "contract needs StockService/AddStock@a (needs)" {
			t.Fatalf("add_stock_a reason is %q, want the alias named", k.Reason)
		}
	}
}

func TestSliceIgnoresWritesAfterTheTarget(t *testing.T) {
	res, err := chain.Slice(aliasFixture(), "create_order", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.DroppedWrites {
		if d.ID == "cancel_order" || d.ID == "confirm_order" {
			t.Fatalf("%s runs after the target, so dropping it cannot under-include the slice: %+v", d.ID, res.DroppedWrites)
		}
	}
	if len(res.DroppedWrites) != 2 || res.Reach != 3 {
		t.Fatalf("want the two earlier writes dropped out of 3 steps up to the target, got %+v reach %d", res.DroppedWrites, res.Reach)
	}
}

func TestSliceIndexesCountFromOneLikeTheRunRecord(t *testing.T) {
	res, err := chain.Slice(aliasFixture(), "confirm_order", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	last := res.Kept[len(res.Kept)-1]
	if last.ID != "confirm_order" || last.Index != 4 {
		t.Fatalf("confirm_order is the 4th step, got %+v", last)
	}
}

func TestSliceSatisfiesAContractEdgeWithTheStepTheBodyReferences(t *testing.T) {
	c := &chain.Chain{
		Name: "catalog-refusals",
		Steps: []*chain.Step{
			{ID: "create_product", Call: "ProductService/CreateProduct"},
			{ID: "create_product_blank_name", Call: "ProductService/CreateProduct"},
			{ID: "add_stock", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
		},
	}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "StockService/AddStock" {
			return []chain.Prereq{{RPC: "ProductService/CreateProduct", Edge: "from"}}
		}
		return nil
	}
	res, err := chain.Slice(c, "add_stock", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(keptIDs(res), ","), "create_product,add_stock"; got != want {
		t.Fatalf("kept %s, want %s: the from edge is the reference the body already makes", got, want)
	}
}

func pinFixture() (*chain.Chain, func(string) []chain.Prereq) {
	c := &chain.Chain{
		Name: "orders",
		Steps: []*chain.Step{
			{ID: "create_product", Call: "ProductService/CreateProduct"},
			{ID: "add_stock", Call: "StockService/AddStock", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
			{ID: "create_order", Call: "OrderService/CreateOrder", Body: map[string]any{"id_product": "${create_product.product.id_product}"}},
		},
	}
	prereqs := func(rpc string) []chain.Prereq {
		if rpc == "OrderService/CreateOrder" {
			return []chain.Prereq{{RPC: "ProductService/CreateProduct", Edge: "from"}}
		}
		return nil
	}
	return c, prereqs
}

func TestClosureLabelsABodyReferenceAsProducesEvenWhenAContractEdgeAgrees(t *testing.T) {
	c, prereqs := pinFixture()
	res, err := chain.Slice(c, "create_order", chain.SliceOptions{Prereqs: prereqs})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range res.Kept {
		if k.ID == "create_product" && k.Kind != chain.KeepProduces {
			t.Fatalf("create_order's body reads create_product, so the reason is the reference, got %s %q", k.Kind, k.Reason)
		}
	}
}

func TestSliceDoesNotLetOnePlainStepStandInForTwoAliases(t *testing.T) {
	c := &chain.Chain{
		Name: "one-stock",
		Steps: []*chain.Step{
			{ID: "stock_first", Call: "StockService/AddStock"},
			{ID: "create_order", Call: "OrderService/CreateOrder"},
			{ID: "confirm_order", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id_order": "${create_order.order.id_order}"}},
		},
	}
	res, err := chain.Slice(c, "confirm_order", chain.SliceOptions{Prereqs: aliasPrereqs})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(keptIDs(res), ","), "stock_first,create_order,confirm_order"; got != want {
		t.Fatalf("kept %s, want %s", got, want)
	}
	if len(res.Unmet) != 1 || res.Unmet[0].RPC != "StockService/AddStock@b" {
		t.Fatalf("stock_first stands in for @a, so @b is unmet and must be reported with its alias, got %+v", res.Unmet)
	}
}
