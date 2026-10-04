package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const confirmedCancelOnly = `apiVersion: shrt/v1
name: cancel-confirmed
steps:
    - id: create_customer
      call: shop.customers.v1.CustomerService/CreateCustomer
      body:
          email: a@example.test
          name: A
    - id: create_product
      call: shop.catalog.v1.ProductService/CreateProduct
      body:
          sku: S-1
          name: P
          price_minor: "100"
    - id: create_order
      call: shop.orders.v1.OrderService/CreateOrder
      body:
          id_customer: ${create_customer.customer.id_customer}
          lines:
              - id_product: ${create_product.product.id_product}
                qty: "2"
    - id: confirm_order
      call: shop.orders.v1.OrderService/ConfirmOrder
      body:
          id_order: ${create_order.order.id_order}
      expect:
          - path: order.status
            equals: ORDER_STATUS_CONFIRMED
    - id: cancel_order
      call: shop.orders.v1.OrderService/CancelOrder
      body:
          id_order: ${create_order.order.id_order}
`

func stateGapWorkspace(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("..", "..", "contract", "testdata", "shopdemo")
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(mustRead(t, filepath.Join(src, "descriptor.binpb"))))
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), shopConfig+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	for _, name := range []string{"catalog.yaml", "customers.yaml", "orders.yaml"} {
		text := string(mustRead(t, filepath.Join(src, "contracts", name)))
		text = strings.Replace(text, "    shop.orders.v1.OrderService/CancelOrder:\n", "    shop.orders.v1.OrderService/CancelOrder:\n"+
			"        needs: [shop.orders.v1.OrderService/ConfirmOrder]\n        effects: {qty_on_hand: {restore: CONFIRMED}}\n", 1)
		writeFile(t, filepath.Join(dir, ".shrt", "contracts", name), text)
	}
	writeFile(t, filepath.Join(dir, ".shrt", "chains", "cancel-confirmed.yaml"), confirmedCancelOnly)
	return chdir(t, dir)
}

func TestStatusGapsNameAWriteNoChainCallsFromTheStateBeforeItsNeed(t *testing.T) {
	defer stateGapWorkspace(t)()
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil || !strings.Contains(out, "no state     CancelOrder on an order in PENDING: no chain calls it so; its plan sends lines of 1, 2 or 3 items, though needs: [ConfirmOrder] only takes the order to CONFIRMED") ||
		!strings.Contains(out, "shrt contract plan <rpc> -write -force (no state)") {
		t.Fatalf("status -gaps names the state no chain cancels from and what to run (%v):\n%s", err, out)
	}
	if out = captureStdout(t, func() { err = contractStatus([]string{"-gaps", "-v"}) }); !strings.Contains(out, "no state     the write's own plan calls it") {
		t.Fatalf("-gaps -v explains a state gap:\n%s", out)
	}
	if out = captureStdout(t, func() { err = contractStatus(nil) }); !strings.Contains(out, "lists them as 'no state'") {
		t.Fatalf("the table counts the state gaps:\n%s", out)
	}
	captureStdout(t, func() { err = contractPlan([]string{"CancelOrder", "-write"}) })
	if err != nil {
		t.Fatal(err)
	}
	if out = captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) }); err != nil || strings.Contains(out, "no state") {
		t.Fatalf("the re-planned chain closes the state gap (%v):\n%s", err, out)
	}
}
