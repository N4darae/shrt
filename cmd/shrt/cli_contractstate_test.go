package main

import (
	"context"
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
	if err != nil || !strings.Contains(out, "no state     CancelOrder on a PENDING order: no chain calls it so; its plan sends 1, 2 or 3 lines, though needs: [ConfirmOrder] only takes the order to CONFIRMED") ||
		!strings.Contains(out, ", so it acts on a PENDING order too: shrt contract plan CancelOrder -write orderservice-cancelorder-gaps.yaml (into .shrt/scratch/)\n") ||
		!strings.Contains(out, "shrt contract plan <rpc> -write <file>.yaml (no state)") || strings.Contains(out, "-force") {
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

func TestChainWhichSaysTheStateAndItemCountAWriteStepActsOn(t *testing.T) {
	defer stateGapWorkspace(t)()
	var err error
	out := captureStdout(t, func() { err = chainWhich([]string{"-rpc", "CancelOrder"}) })
	if err != nil || !strings.Contains(out, "\n  cancel_order  on CONFIRMED order, 1 line\n") {
		t.Fatalf("chain which says the state and line count a step cancels from (%v):\n%s", err, out)
	}
	if out = captureStdout(t, func() { err = chainWhich([]string{"-code", "1304"}) }); strings.Contains(out, " order, ") {
		t.Fatalf("only -rpc prints the state a step acts on:\n%s", out)
	}
	captureStdout(t, func() { err = contractPlan([]string{"CancelOrder", "-write"}) })
	if err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() { err = chainWhich([]string{"-rpc", "CancelOrder"}) })
	if err != nil || !strings.Contains(out, "\n  cancel_order_3_lines_from_pending  on PENDING order, 3 lines\n") {
		t.Fatalf("chain which shows the planned 3-line cancel of a PENDING order (%v):\n%s", err, out)
	}
	if !strings.Contains(out, "\n  cancel_order_when_cancelled  asserts 1304\n") {
		t.Fatalf("a refused step acts on nothing, so its row names no state:\n%s", out)
	}
}

func TestGateReproNamesTheStatesNoChainCallsAGatedWriteFrom(t *testing.T) {
	defer stateGapWorkspace(t)()
	saved := gateExec
	t.Cleanup(func() { gateExec = saved })
	gateExec = func(context.Context, []string) gateOutcome {
		return gateOutcome{code: 1, side: gateSidecar{Error: "boom"}}
	}
	out, code := runGateOut(t, "-repro")
	gap := "\n  CancelOrder on a PENDING order: no chain calls it so; its plan sends 1, 2 or 3 lines\n"
	if code != 1 || !strings.Contains(out, "\ngaps: 3 states no chain calls a gated write from, 0 probed against the contract:\n") ||
		!strings.Contains(out, gap+"    not probed: boom (shrt run .shrt/scratch/orderservice-cancelorder-gaps.yaml)\n") || strings.Contains(out, "restore:") ||
		!strings.HasSuffix(out, "shrt gate: FAIL: 1 of 1 chain failed\n") {
		t.Fatalf("gate -repro lists the state gaps of the rpcs it covers beside its verdict, says which it could not probe, and the verdict points at those, got %d:\n%s", code, out)
	}
	if out, _ = runGateOut(t); strings.Contains(out, "gaps:") {
		t.Fatalf("the CI gate prints no gap block:\n%s", out)
	}
	writeFile(t, ".shrt/chains/customer-only.yaml", "apiVersion: shrt/v1\nname: customer-only\nsteps:\n    - id: create_customer\n"+
		"      call: shop.customers.v1.CustomerService/CreateCustomer\n      body:\n          email: b@example.test\n          name: B\n")
	if out, _ = runGateOut(t, "-repro", "customer-only"); !strings.Contains(out, "\ngaps: none\n") {
		t.Fatalf("a gate of chains that call no write with a gap lists none:\n%s", out)
	}
	var err error
	if captureStdout(t, func() { err = contractPlan([]string{"CancelOrder", "-write"}) }); err != nil {
		t.Fatal(err)
	}
	if out, _ = runGateOut(t, "-repro"); !strings.Contains(out, "\ngaps: none\n") || !strings.HasSuffix(out, "shrt gate: FAIL: 3 of 3 chains failed\n") {
		t.Fatalf("once a chain covers the state, the gate says no gap is left:\n%s", out)
	}
}
