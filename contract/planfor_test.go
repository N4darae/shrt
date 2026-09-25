package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

const (
	shopAddStock    = "shop.catalog.v1.StockService/AddStock"
	shopCancelOrder = "shop.orders.v1.OrderService/CancelOrder"
	shopFetchOrder  = "shop.orders.v1.OrderService/FetchOrder"
)

const cancelFlowOverlay = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "2"
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/FetchOrder:
        summary: reads an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/CancelOrder:
        summary: cancels an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        aliases:
            confirmed:
                note: cancel after the confirm, the path that returns stock
                fields:
                    id_order:
                        from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
`

func cancelFlowLibrary(t *testing.T) *contract.Library {
	return shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, cancelFlowOverlay)
}

func TestPlanNoteDoesNotSilenceTheScaffoldZero(t *testing.T) {
	cat := catalogtest.Shop()
	withNote := strings.Replace(shopCatalogOverlay, "            sku:\n                value: SKU-1\n",
		"            sku:\n                value: SKU-1\n            price_minor:\n                note: cents, must be greater than zero\n", 1)
	plan, err := contract.BuildPlan(shopCreateProduct, shopLibrary(t, withNote), cat, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "price_minor still carries the scaffold's numeric zero") {
		t.Fatalf("a note describing units does not make 0 deliberate, so the soft note must still fire: %v", plan.Notes)
	}

	explicit := strings.Replace(shopCatalogOverlay, "            sku:\n                value: SKU-1\n",
		"            sku:\n                value: SKU-1\n            price_minor:\n                value: \"0\"\n", 1)
	plan, err = contract.BuildPlan(shopCreateProduct, shopLibrary(t, explicit), cat, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if anyNote(plan.Notes, "numeric zero") {
		t.Fatalf("value: \"0\" is the explicit statement that 0 is meant, it must silence the note: %v", plan.Notes)
	}
}

func TestPlanTargetsAnAlias(t *testing.T) {
	cat := catalogtest.Shop()
	plan, err := contract.BuildPlan(shopCancelOrder+"@confirmed", cancelFlowLibrary(t), cat, "cancel")
	if err != nil {
		t.Fatalf("plan Rpc@alias must be accepted: %v", err)
	}
	last := plan.Order[len(plan.Order)-1]
	if last != shopCancelOrder+"@confirmed" {
		t.Fatalf("the target node must be the aliased one, got order %v", plan.Order)
	}
	step, ok := plan.Chain.Step("cancel_order_confirmed")
	if !ok {
		t.Fatalf("the aliased target must get its own step id, got steps %v", plan.Order)
	}
	if got := step.Body["id_order"]; got != "${confirm_order.order.id_order}" {
		t.Fatalf("the target step must use the alias's field overrides, id_order = %v", got)
	}
	if plan.Order[len(plan.Order)-2] != shopConfirmOrder {
		t.Fatalf("the alias's from: must pull ConfirmOrder in before the target, got %v", plan.Order)
	}

	_, err = contract.BuildPlan(shopCancelOrder+"@nope", cancelFlowLibrary(t), cat, "cancel")
	if err == nil || !strings.Contains(err.Error(), `no alias "nope"`) || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("an undeclared target alias must be refused naming the declared ones, got %v", err)
	}
}

func TestPlanForComposesSeveralTargetsInDependencyOrder(t *testing.T) {
	cat := catalogtest.Shop()
	targets := []string{shopConfirmOrder, shopFetchOrder, shopCancelOrder + "@confirmed", shopFetchOrder}
	plan, err := contract.BuildPlanFor(targets, cancelFlowLibrary(t), cat, "flow")
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, node := range plan.Order {
		if _, dup := pos[node]; dup {
			t.Fatalf("%s appears twice in %v", node, plan.Order)
		}
		pos[node] = i
	}
	for _, want := range []string{shopConfirmOrder, shopFetchOrder, shopCancelOrder + "@confirmed", shopCreateOrder} {
		if _, ok := pos[want]; !ok {
			t.Fatalf("%s missing from %v", want, plan.Order)
		}
	}
	if pos[shopCreateOrder] > pos[shopConfirmOrder] || pos[shopConfirmOrder] > pos[shopFetchOrder] ||
		pos[shopConfirmOrder] > pos[shopCancelOrder+"@confirmed"] {
		t.Fatalf("every dependency must precede what needs it, got %v", plan.Order)
	}
	if len(plan.Targets) != 3 {
		t.Fatalf("a repeated target must be deduplicated, got targets %v", plan.Targets)
	}
	if !strings.Contains(plan.Chain.Description, "ConfirmOrder, FetchOrder, CancelOrder@confirmed") {
		t.Fatalf("the description must name every target, got %q", plan.Chain.Description)
	}
}

const stockSiblingsOverlay = `apiVersion: shrt/contract/v1
domain: stock
rpcs:
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        required: [id_product]
        before: [shop.orders.v1.OrderService/ConfirmOrder]
        fields:
            id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            qty:
                value: "5"
        aliases:
            first:
                fields:
                    id_product:
                        from: shop.catalog.v1.ProductService/CreateProduct@a->product.id_product
            second:
                fields:
                    id_product:
                        from: shop.catalog.v1.ProductService/CreateProduct@b->product.id_product
        status: draft
`

func TestPlanNotesAPlainSiblingOfAliasedNodes(t *testing.T) {
	cat := catalogtest.Shop()
	confirm := strings.Replace(cancelFlowOverlay,
		"    shop.orders.v1.OrderService/ConfirmOrder:\n        summary: confirms an order\n",
		"    shop.orders.v1.OrderService/ConfirmOrder:\n        summary: confirms an order\n"+
			"        needs: [shop.catalog.v1.StockService/AddStock@first, shop.catalog.v1.StockService/AddStock@second]\n", 1)
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, confirm, stockSiblingsOverlay)
	plan, err := contract.BuildPlan(shopConfirmOrder, lib, cat, "siblings")
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, n := range plan.Notes {
		if strings.Contains(n, "all call AddStock") {
			found = n
		}
	}
	if found == "" {
		t.Fatalf("a plain AddStock next to AddStock@first/@second must be flagged: %v", plan.Notes)
	}
	for _, want := range []string{"add_stock", "add_stock_first", "add_stock_second", "AddStock before: ConfirmOrder",
		"AddStock@first (via ConfirmOrder needs:)", "likely a duplicate"} {
		if !strings.Contains(found, want) {
			t.Fatalf("the note must name %q: %s", want, found)
		}
	}

	noBefore := strings.Replace(stockSiblingsOverlay, "        before: [shop.orders.v1.OrderService/ConfirmOrder]\n", "", 1)
	lib = shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, confirm, noBefore)
	plan, err = contract.BuildPlan(shopConfirmOrder, lib, cat, "siblings")
	if err != nil {
		t.Fatal(err)
	}
	if anyNote(plan.Notes, "all call AddStock") {
		t.Fatalf("only aliased AddStock nodes are no duplicate: %v", plan.Notes)
	}
}

func TestPlanDoesNotCallAnExplicitTargetADuplicate(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, cancelFlowOverlay, stockSiblingsOverlay)
	plan, err := contract.BuildPlanFor([]string{
		"shop.catalog.v1.StockService/AddStock",
		"shop.catalog.v1.StockService/AddStock@first",
	}, lib, cat, "both")
	if err != nil {
		t.Fatal(err)
	}
	if anyNote(plan.Notes, "all call AddStock") {
		t.Fatalf("both AddStock steps were asked for, so neither is a duplicate: %v", plan.Notes)
	}
}

func TestPlanDeclaresAndNotesAVarItsValuesInterpolate(t *testing.T) {
	cat := catalogtest.Shop()
	overlay := strings.Replace(shopCatalogOverlay, "value: SKU-1", "value: SKU-${vars.tag}", 1)
	if overlay == shopCatalogOverlay {
		t.Fatal("the catalog fixture carries no sku value to rewrite")
	}
	lib := shopLibrary(t, overlay, shopCustomersOverlay, cancelFlowOverlay, stockSiblingsOverlay)
	plan, err := contract.BuildPlan("shop.catalog.v1.ProductService/CreateProduct", lib, cat, "tagged")
	if err != nil {
		t.Fatal(err)
	}
	if _, declared := plan.Chain.Vars["tag"]; declared {
		t.Fatalf("an undeclared ${vars.tag} is fresh on every run, so the plan must not declare it: %v", plan.Chain.Vars)
	}
	if missing, _ := chain.ExternalInputs(plan.Chain); len(missing) > 0 {
		t.Fatalf("a planned chain reading ${vars.tag} needs no -var, got %v", missing)
	}
	if anyNote(plan.Notes, "-var tag=") {
		t.Fatalf("a planned chain re-runs without -var tag, so no note may ask for one: %v", plan.Notes)
	}
}
