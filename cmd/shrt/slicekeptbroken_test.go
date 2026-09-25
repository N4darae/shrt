package main

import (
	"context"
	"strings"
	"testing"
)

const confirmThenFetchChain = `apiVersion: shrt/v1
name: probe-confirm
kept_red:
    - step: fetch_order
      path: order.total_minor
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: cust-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-a
        price_minor: "100"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
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
        idempotency_key: ${uuid}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: confirm_order
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: fetch_order
      call: OrderService/FetchOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: order.total_minor
          equals: 999
`

func TestSliceVerifyIsInconclusiveWhenAKeptStepThatPassedInTheSourceFailsInTheSlice(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/scratch/probe-confirm.yaml", confirmThenFetchChain)
	writeFile(t, ".shrt/contracts/orders.yaml", `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms the order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/FetchOrder:
        summary: reads the order
        required: [NONE]
        status: draft
`)
	_ = runRun(context.Background(), []string{".shrt/scratch/probe-confirm.yaml", "-quiet"})
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), []string{".shrt/scratch/probe-confirm.yaml", "-step", "fetch_order", "-run", "latest", "-verify"})
	})
	if strings.Contains(out, "verify reproduced") {
		t.Fatalf("a slice whose kept confirm fails for want of the stock it dropped is no receipt:\n%s", out)
	}
	if exitCodeOf(err) != 3 || !strings.Contains(out, "kept step(s) confirm_order passed in source run") {
		t.Fatalf("the verdict names the kept step that broke and is inconclusive (exit %d):\n%s", exitCodeOf(err), out)
	}
}
