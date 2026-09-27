package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

const pinFetchAfterCancel = `    - id: fetch_after_cancel
      call: OrderService/FetchOrder
      body:
        id_order: ${create_order_three.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
`

func TestChainPinKeepsTheFailureRedInASliceAndTheRestGreen(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	source := strings.Replace(cancelConfirmedChain, "vars:\n    tag: probe\n", "", 1) + pinFetchAfterCancel
	writeFile(t, ".shrt/chains/probe-orders.yaml", source)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"probe-orders", "-quiet"}) })
	if err == nil || !strings.HasSuffix(strings.TrimSpace(out), "pin it: shrt chain pin probe-orders (re-runs with -keep-going when needed)") {
		t.Fatalf("a red run ends with the pin command:\n%s", out)
	}

	out = captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-orders"}) })
	if err != nil {
		t.Fatalf("pin: %v\n%s", err, out)
	}
	for _, want := range []string{
		"ran probe-orders -keep-going: run ",
		"again with -keep add_stock",
		"wrote .shrt/chains/probe-orders-slice-cancel_confirmed.yaml: kept red on cancel_confirmed at status.code",
		"wrote .shrt/chains/probe-orders.yaml: probe-orders no longer runs cancel_confirmed: kept red in probe-orders-slice-cancel_confirmed",
		"verify reproduced",
		", passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n > 6 {
		t.Fatalf("pin prints one line per run, per written file and the verdict, got %d lines:\n%s", n, out)
	}
	rest, err := chain.LoadFile(".shrt/chains/probe-orders.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, left := rest.Step("cancel_confirmed"); left || len(rest.KeptRed) > 0 {
		t.Fatalf("the chain is rewritten without the pinned step:\n%v", rest.Steps)
	}
	if _, kept := rest.Step("fetch_after_cancel"); !kept {
		t.Fatalf("steps that did not fail stay in the chain")
	}
	captureStdout(t, func() { err = runRun(context.Background(), []string{"probe-orders", "-quiet"}) })
	if err != nil {
		t.Fatalf("the rest runs green: %v", err)
	}
	captureStdout(t, func() {
		err = runRun(context.Background(), []string{"probe-orders-slice-cancel_confirmed", "-quiet"})
	})
	if err != nil {
		t.Fatalf("the slice fails as pinned, exit 0: %v", err)
	}

	out = captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-orders-slice-cancel_confirmed"}) })
	if err == nil || !strings.Contains(err.Error(), "already declares kept_red") {
		t.Fatalf("a kept-red chain is not pinned again: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-orders"}) })
	if err != nil || !strings.Contains(out, "nothing to pin") {
		t.Fatalf("a green chain has nothing to pin: %v\n%s", err, out)
	}
}

func TestChainPinLeavesTheChainAloneWhenTheSliceDoesNotReproduce(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	source := strings.Replace(cancelConfirmedChain, "vars:\n    tag: probe\n", "", 1)
	writeFile(t, ".shrt/chains/probe-orders.yaml", source)
	captureStdout(t, func() { _ = runRun(context.Background(), []string{"probe-orders", "-quiet", "-keep-going"}) })
	shop.cancelConfirmedBug = false
	var err error
	out := captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-orders"}) })
	if err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("a failure the slice does not reproduce is not pinned: %v\n%s", err, out)
	}
	if raw, _ := os.ReadFile(".shrt/chains/probe-orders.yaml"); string(raw) != source {
		t.Fatalf("the chain is left as it was:\n%s", raw)
	}
	if _, err := os.Stat(".shrt/chains/probe-orders-slice-cancel_confirmed.yaml"); err == nil {
		t.Fatalf("no slice is written for a failure that did not reproduce")
	}
}

func TestChainPinHelpSaysItReRunsWithKeepGoing(t *testing.T) {
	if !strings.Contains(pinUsage, "re-running the chain with -keep-going first") {
		t.Errorf("chain pin -h says whether it re-runs: %s", pinUsage)
	}
}

const pinTwoDefectsChain = `apiVersion: shrt/v1
name: probe-two
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: two-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-two
        price_minor: "100"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "50"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock_extra
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "50"
      expect:
        - path: qty_on_hand
          equals: "999"
    - id: create_order_big
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "80"
        idempotency_key: big-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: confirm_big
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order_big.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order_single
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "1"
        idempotency_key: single-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: confirm_single
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order_single.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: cancel_confirmed
      call: OrderService/CancelOrder
      body:
        id_order: ${create_order_single.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
`

func TestChainPinGivesSeparateDefectsTheirOwnSliceAndLeavesTheChainGreen(t *testing.T) {
	shop := newFakeShop()
	shop.cancelConfirmedBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/probe-two.yaml", pinTwoDefectsChain)
	var err error
	out := captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-two"}) })
	if err != nil {
		t.Fatalf("pin: %v\n%s", err, out)
	}
	pins := map[string][]string{}
	for _, target := range []string{"add_stock_extra", "confirm_big", "cancel_confirmed"} {
		c, err := chain.LoadFile(".shrt/chains/probe-two-slice-" + target + ".yaml")
		if err != nil {
			t.Fatalf("each defect gets a slice of its own: %v\n%s", err, out)
		}
		for _, k := range c.KeptRed {
			if !containsStr(pins[target], k.Step) {
				pins[target] = append(pins[target], k.Step)
			}
		}
		if len(pins[target]) != 1 || pins[target][0] != target {
			t.Fatalf("slice of %s keeps red only its own step, got %v\n%s", target, pins[target], out)
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := "wrote .shrt/chains/probe-two.yaml: probe-two no longer runs add_stock_extra: kept red in probe-two-slice-add_stock_extra; " +
		"confirm_big: kept red in probe-two-slice-confirm_big; cancel_confirmed: kept red in probe-two-slice-cancel_confirmed"
	if len(lines) < 2 || !strings.HasSuffix(lines[len(lines)-2], ", passed") || lines[len(lines)-1] != want {
		t.Fatalf("pin ends on the rewritten chain's green run, then one line naming what it no longer runs and where each went:\n%s", out)
	}
	captureStdout(t, func() { err = runRun(context.Background(), []string{"probe-two", "-quiet"}) })
	if err != nil {
		t.Fatalf("after one pin the rewritten chain runs green: %v", err)
	}
}

const pinReadBackChain = `apiVersion: shrt/v1
name: probe-wipe
steps:
  - call: CustomerService/CreateCustomer
    id: create_customer
    body: {email: "wipe-${vars.tag}@example.test"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: ProductService/CreateProduct
    id: create_product
    body:
      sku: 'sku-${vars.tag}-wipe'
      price_minor: "100"
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: OrderService/CreateOrder
    id: create_order
    body:
      id_customer: ${create_customer.customer.id_customer}
      lines:
        - {id_product: "${create_product.product.id_product}", qty: "1"}
      idempotency_key: wipe-${vars.tag}
    expect:
      - {path: status.code, equals: SUCCESS}

  - call: OrderService/CancelOrder
    id: cancel_order
    body:
      id_order: ${create_order.order.id_order}
    expect:
      - {path: status.code, equals: SUCCESS}

  - call: OrderService/FetchOrder
    id: fetch_after_cancel
    body:
      id_order: ${create_order.order.id_order}
    expect:
      - {path: status.code, equals: SUCCESS}

  - call: ProductService/GetProduct
    id: get_product
    body:
      id_product: ${create_product.product.id_product}
    expect:
      - {path: status.code, equals: SUCCESS}
`

func TestChainPinTakesTheFailingReadBacksOfAPinnedWriteAndLeavesTheRestOfTheFileAsWritten(t *testing.T) {
	shop := newFakeShop()
	shop.cancelWipesBug = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/probe-wipe.yaml", pinReadBackChain)
	var err error
	out := captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-wipe"}) })
	if err != nil {
		t.Fatalf("pin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "wrote .shrt/chains/probe-wipe.yaml: probe-wipe no longer runs cancel_order, fetch_after_cancel: kept red in probe-wipe-slice-cancel_order") {
		t.Fatalf("the read-back that failed right after the pinned write leaves the chain with it:\n%s", out)
	}
	slice, err := chain.LoadFile(".shrt/chains/probe-wipe-slice-cancel_order.yaml")
	if err != nil {
		t.Fatal(err)
	}
	red := map[string]bool{}
	for _, k := range slice.KeptRed {
		red[k.Step] = true
	}
	if !red["cancel_order"] || !red["fetch_after_cancel"] || len(red) != 2 {
		t.Fatalf("the slice keeps the write and its read-back red, got %v", slice.KeptRed)
	}
	cancelAt := strings.Index(pinReadBackChain, "\n  - call: OrderService/CancelOrder")
	getAt := strings.Index(pinReadBackChain, "  - call: ProductService/GetProduct")
	want := pinReadBackChain[:cancelAt+1] + pinReadBackChain[getAt:]
	if raw, _ := os.ReadFile(".shrt/chains/probe-wipe.yaml"); string(raw) != want {
		t.Fatalf("only the pinned steps leave the file, the rest stays as written:\n%s\nwant:\n%s", raw, want)
	}
}

func TestPinNamesTheReadsOfAPinnedStepThatNoSliceHolds(t *testing.T) {
	c := &chain.Chain{Name: "src", Steps: []*chain.Step{
		{ID: "make", Call: "S/Create", Export: map[string]string{"id": "thing.id"}},
		{ID: "confirm", Call: "S/Confirm", Body: map[string]any{"id": "${id}"}, Export: map[string]string{"ref": "ref"}},
		{ID: "read_ref", Call: "S/Get", Body: map[string]any{"ref": "${ref}"}},
		{ID: "read_make", Call: "S/Get", Body: map[string]any{"id": "${id}"}},
	}}
	slice := &chain.Chain{Name: "src-slice-confirm", Steps: c.Steps[:2]}
	got := movedSteps(c, slice, []string{"confirm"})
	if want := "confirm: kept red in src-slice-confirm; read_ref: in no slice"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
