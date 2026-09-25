package main

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const pricedOrderChain = `apiVersion: shrt/v1
name: order-happy
vars:
    tag: happy
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "1250"
      expect:
        - path: status.code
          equals: SUCCESS
        - path: product.price_minor
          equals: 1250
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: c-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
        idempotency_key: k-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: order.total_minor
          equals: 2500
    - id: confirm_order
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
`

func TestSliceReachesTheTargetPastKeptStepsThatFailedAnUnrelatedExpectation(t *testing.T) {
	shop := newFakeShop()
	shop.priceBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/order-happy.yaml", pricedOrderChain)
	_ = runRun(context.Background(), []string{".shrt/scratch/order-happy.yaml", "-quiet", "-keep-going", "-var", "tag=src"})

	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/order-happy.yaml", "-step", "confirm_order", "-run", "latest",
			"-verify", "-repeat", "1", "-var", "tag=s1"})
	})
	if strings.Contains(out, "DID NOT RUN") {
		t.Fatalf("create_product and create_order were answered and failed only a price expectation, so the slice keeps them "+
			"with that expectation relaxed and reaches confirm_order:\n%s", out)
	}
	if !strings.Contains(out, "create_product product.price_minor equals") || !strings.Contains(out, "create_order order.total_minor equals") {
		t.Fatalf("the slice must say which expectations of kept steps it relaxed:\n%s", out)
	}
	next := nextCommand(out)
	if joined := strings.Join(next, " "); next == nil || !(strings.Contains(joined, "-keep add_stock ") || strings.Contains(joined, "-keep writes ")) {
		t.Fatalf("without the stock the confirm is refused; next must keep add_stock (exit %d):\n%s", exitCodeOf(err), out)
	}
	next = append(next[:len(next):len(next)], "-repeat", "1", "-var", "tag=s2")
	again := captureStdout(t, func() { err = chainSlice(context.Background(), next) })
	if exitCodeOf(err) != 0 || !strings.Contains(again, "verify reproduced") {
		t.Fatalf("the suggested command must reach and reproduce the target (exit %d):\n%s", exitCodeOf(err), again)
	}
}

func TestSliceNextIsWithheldWhenItsClosurePullsInAStepThatErredInTheSourceRun(t *testing.T) {
	c := &chain.Chain{Name: "flow", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create"},
		{ID: "stock", Call: "StockService/AddStock", Body: map[string]any{"id": "${create.id}"}},
		{ID: "order", Call: "OrderService/CreateOrder", Body: map[string]any{"id": "${create.id}"}},
		{ID: "confirm", Call: "OrderService/ConfirmOrder", Body: map[string]any{"id": "${order.id}"}},
		{ID: "get", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"}},
	}}
	res, err := chain.Slice(c, "get", chain.SliceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{
		{ID: "create", Status: runner.StatusPassed},
		{ID: "stock", Status: runner.StatusPassed},
		{ID: "order", Status: runner.StatusError, Error: "connection reset"},
		{ID: "confirm", Status: runner.StatusPassed},
		{ID: "get", Status: runner.StatusFailed},
	}}
	v := &sliceVerdict{Step: "get"}
	v.suggestKeep(res, rec, sliceVerifyArgs{reslice: func(keep []string) *chain.SliceResult {
		next, err := chain.Slice(c, "get", chain.SliceOptions{Keep: keep})
		if err != nil {
			return nil
		}
		return next
	}}, []string{"stock", "confirm"})
	if v.Next != "" {
		t.Fatalf("keeping confirm pulls in order, which erred in the source run, so the command would stop there: %s", v.Next)
	}
	if !strings.Contains(v.Reason, "order") || !strings.Contains(v.Reason, "error") {
		t.Fatalf("the reason must name the step that would stop the slice and why: %s", v.Reason)
	}
}
