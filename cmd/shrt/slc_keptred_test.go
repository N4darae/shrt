package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestChainLsMarksAChainKeptRed(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-red.yaml", `apiVersion: shrt/v1
name: cli-red
description: reach Fetch, composed from the contract dependency graph
kept_red:
    - step: fetch
      path: error.code
steps:
    - id: fetch
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: OK
`)
	var err error
	out := captureStdout(t, func() { err = chainList(nil) })
	if err != nil {
		t.Fatal(err)
	}
	var red, plain string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, " cli-red ") {
			red = line
		}
		if strings.Contains(line, " cli-thing-flow ") {
			plain = line
		}
	}
	if !strings.HasSuffix(red, " 1 step(s)") || strings.Contains(red, "reach Fetch") {
		t.Fatalf("a line ends at the step count; -long prints the description:\n%s", out)
	}
	if !strings.HasPrefix(red, " R ") || strings.HasPrefix(plain, " R ") {
		t.Fatalf("a chain kept red is marked R, one that is not is not:\n%s", out)
	}
	if !strings.Contains(out, "R = kept red") {
		t.Fatalf("the legend explains the mark:\n%s", out)
	}
	out = captureStdout(t, func() { err = chainList([]string{"-json"}) })
	var rows []map[string]any
	if json.Unmarshal([]byte(out), &rows) != nil {
		t.Fatalf("bad json: %s", out)
	}
	for _, r := range rows {
		if r["name"] == "cli-red" && r["kept_red"] != true {
			t.Fatalf("the JSON row says it is kept red: %v", r)
		}
	}
}

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
	if !strings.Contains(out, "again with -keep add_stock") || !strings.Contains(out, "verify reproduced") {
		t.Fatalf("pin re-slices with the write it needs and verifies the slice:\n%s", out)
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
	rest, err := chain.LoadFile(".shrt/chains/probe-two.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"add_stock_extra", "confirm_big", "cancel_confirmed"} {
		if _, left := rest.Step(gone); left {
			t.Fatalf("the chain no longer runs %s:\n%s", gone, out)
		}
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
		t.Fatalf("the slice keeps the write and its read-back red, got %v\n%s", slice.KeptRed, out)
	}
	cancelAt := strings.Index(pinReadBackChain, "\n  - call: OrderService/CancelOrder")
	getAt := strings.Index(pinReadBackChain, "  - call: ProductService/GetProduct")
	want := pinReadBackChain[:cancelAt+1] + pinReadBackChain[getAt:]
	if raw, _ := os.ReadFile(".shrt/chains/probe-wipe.yaml"); string(raw) != want {
		t.Fatalf("only the pinned steps leave the file, the rest stays as written:\n%s\nwant:\n%s", raw, want)
	}
}

const pinCheckpointChain = `apiVersion: shrt/v1
name: probe-restock
steps:
  - call: ProductService/CreateProduct
    id: create_product
    body: {sku: 'sku-${uuid}', price_minor: "100"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: StockService/AddStock
    id: add_stock
    body: {id_product: "${create_product.product.id_product}", qty: "5"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: CustomerService/CreateCustomer
    id: create_customer
    body: {email: "restock-${uuid}@example.test"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: OrderService/CreateOrder
    id: create_order
    body:
      id_customer: ${create_customer.customer.id_customer}
      lines:
        - {id_product: "${create_product.product.id_product}", qty: "2"}
      idempotency_key: ${uuid}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: ProductService/GetProduct
    id: get_after_create
    body: {id_product: "${create_product.product.id_product}"}
    expect:
      - {path: product.qty_on_hand, equals: "5"}
  - call: OrderService/ConfirmOrder
    id: confirm_order
    body: {id_order: "${create_order.order.id_order}"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: ProductService/GetProduct
    id: get_after_confirm
    body: {id_product: "${create_product.product.id_product}"}
    expect:
      - {path: product.qty_on_hand, equals: "3"}
  - call: OrderService/CancelOrder
    id: cancel_order
    body: {id_order: "${create_order.order.id_order}"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: ProductService/GetProduct
    id: get_after_cancel
    body: {id_product: "${create_product.product.id_product}"}
    expect:
      - {path: product.qty_on_hand, equals: "5"}
`

func TestAPinnedSliceKeepsTheLastPassingReadOfItsFieldBeforeTheKeptWrites(t *testing.T) {
	shop := newFakeShop()
	shop.stockInProduct = true
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/probe-restock.yaml", pinCheckpointChain)
	var err error
	out := captureStdout(t, func() { err = chainPin(context.Background(), []string{"probe-restock"}) })
	if err != nil {
		t.Fatalf("pin: %v\n%s", err, out)
	}
	path := ".shrt/chains/probe-restock-slice-get_after_cancel.yaml"
	slice, ids := slcSteps(t, path)
	if !strings.Contains(ids, "confirm_order,get_after_confirm,cancel_order,get_after_cancel") || strings.Contains(ids, "get_after_create") {
		t.Fatalf("the slice keeps the read between the kept writes, and only the nearest: %s", ids)
	}
	if !strings.Contains(slice.Description, "get_after_confirm (last passing read of product.qty_on_hand before cancel_order)") {
		t.Fatalf("the description names the checkpoint and why:\n%s", slice.Description)
	}
	if _, err := quietly(func() error {
		return runRun(context.Background(), []string{"probe-restock-slice-get_after_cancel", "-quiet"})
	}); err != nil {
		t.Fatalf("green as pinned on the same backend: %v", err)
	}
	shop.confirmExtraUnit = true
	out, err = quietly(func() error {
		return runRun(context.Background(), []string{"probe-restock-slice-get_after_cancel", "-quiet"})
	})
	if err == nil || !strings.Contains(out, "NEW FAILURE outside the pinned defect: get_after_confirm product.qty_on_hand want=3 got=2") {
		t.Fatalf("a later confirm defect fails the checkpoint: %v\n%s", err, out)
	}
	inProcessGate(t)
	out, _ = quietly(func() error { return runGate(context.Background(), []string{"probe-restock-slice-get_after_cancel"}) })
	if !strings.Contains(out, "suspect write confirm_order (OrderService/ConfirmOrder)") || strings.Contains(out, "suspect write cancel_order") {
		t.Fatalf("the gate names the write before the checkpoint:\n%s", out)
	}
	sliced, err := quietly(func() error {
		return chainSlice(context.Background(), []string{"probe-restock", "-step", "get_after_confirm", "-run", "latest", "-v"})
	})
	if err != nil || strings.Contains(sliced, "checkpoint") {
		t.Fatalf("a plain slice keeps no checkpoint: %v\n%s", err, sliced)
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

const keptRedThingChain = `apiVersion: shrt/v1
name: cli-red-flow
kept_red:
    - step: fetch
      path: name
      got: widget
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
`

func TestConfirmSaysAKeptRedChainIsNeverConfirmed(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-red-flow.yaml", keptRedThingChain)
	if err := runRun(context.Background(), []string{"cli-red-flow", "-quiet"}); err != nil {
		t.Fatalf("the chain fails as pinned, so run exits 0: %v", err)
	}
	err := runConfirm(context.Background(), []string{"cli-red-flow", "-note", "fetch names the defect"})
	if err == nil {
		t.Fatal("a kept_red chain must never be proposed")
	}
	for _, want := range []string{"kept red on purpose", "never confirmed", "evidence", "not baselines"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}

func TestVerifySaysAKeptRedChainHasNoSafeSpotByDesign(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-red-flow.yaml", keptRedThingChain)
	err := runVerify(context.Background(), []string{"cli-red-flow"})
	if err == nil || !strings.Contains(err.Error(), "kept red on purpose, so it has no safe spot by design: shrt gate compares its pins") || strings.Contains(err.Error(), "shrt confirm") {
		t.Fatalf("a kept-red chain is never confirmed, so verify does not suggest confirm: %v", err)
	}
}

func TestAPinnedStepThatReturnsSomethingElseIsNotAsPinned(t *testing.T) {
	var total atomic.Int64
	total.Store(5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "gadget", "total": total.Load()})
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	run := func() (string, error) {
		var err error
		out := captureStdout(t, func() { err = runRun(context.Background(), []string{"red"}) })
		return out, err
	}
	out, err := run()
	if err != nil || !strings.Contains(out, "FAILED AS PINNED") || !strings.Contains(out, "this run is the reference for the next one") {
		t.Fatalf("an old pin with no got still fails as pinned, and the first run says it had nothing to compare with: %v\n%s", err, out)
	}
	if out, err = run(); err != nil || !strings.Contains(out, "the pinned steps return what they returned in run") {
		t.Fatalf("an unchanged pinned step stays as pinned: %v\n%s", err, out)
	}
	total.Store(7)
	for i := 0; i < 2; i++ {
		out, err = run()
		if err == nil || !strings.Contains(out, "FAILED, NOT AS PINNED") || !strings.Contains(out, "fetch changed total a=5 b=7") {
			t.Fatalf("run %d: the pinned expectation still fails the same way, but the pinned step returns another total: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(out, "the pins held, so this is a new change outside them, not a reason to re-pin") || strings.Contains(out, "-force") {
			t.Fatalf("the note says the pins held and does not suggest re-pinning the change:\n%s", out)
		}
		if strings.Contains(out, "NEW FAILURE") || !strings.Contains(err.Error(), "a pinned step now returns something else: fetch changed total a=5 b=7") {
			t.Fatalf("a pinned step whose pins still fail as pinned is not a new failure outside the pinned defect: %v\n%s", err, out)
		}
	}
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name, got: gadget}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	if out, err = run(); err != nil || !strings.Contains(out, "FAILED AS PINNED") {
		t.Fatalf("a re-pinned chain file starts a new reference: %v\n%s", err, out)
	}
}

func TestKeptRedPinsRecordAStableGot(t *testing.T) {
	c := &chain.Chain{Name: "red", KeptRed: []chain.Pin{{Step: "list", Path: "orders.1"}}, Steps: []*chain.Step{{ID: "create"}, {ID: "list"}}}
	rec := &runner.Record{RunID: "r1", Vars: map[string]any{"tag": "t42"}, Steps: []*runner.StepRecord{
		{ID: "create", Status: runner.StatusPassed, Response: []byte(`{"order":{"id_order":"ord-ba9876543210"}}`)},
		{ID: "list", Status: runner.StatusFailed, Expect: []chain.ExpectResult{
			{Path: "orders.1", Rule: "exists", Want: false, Got: true},
			{Path: "orders.0.status", Rule: "equals", Want: "PENDING", Got: "CANCELLED"},
			{Path: "orders.0.id_order", Rule: "equals", Want: "ord-0123456789ab", Got: "ord-ba9876543210"},
			{Path: "orders.0.name", Rule: "equals", Want: "Widget", Got: "Widget t42"},
			{Path: "orders.0.total_minor", Rule: "equals", Want: 1250.0, Got: 500.0},
			{Path: "orders.0.created_at", Rule: "equals", Want: "2026-01-01T00:00:00Z", Got: "2026-01-02T00:00:00Z"},
			{Path: "status.details.0.reason", Rule: "equals", Want: "Cancelled"},
		}}}}
	pins, err := failurePins(c, rec, "list")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range pins {
		if p.Got != nil {
			got[p.Path] = *p.Got
		} else {
			got[p.Path] = "<none>"
		}
	}
	want := map[string]string{"orders.0.status": "CANCELLED", "orders.0.id_order": "${create.order.id_order}", "orders.0.name": "Widget ${vars.tag}",
		"orders.0.total_minor": "500", "orders.0.created_at": "<none>", "status.details.0.reason": ""}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s: pinned got=%s, want %s (all: %v)", path, got[path], w, got)
		}
	}
	if c.KeptRed[0].Got == nil || *c.KeptRed[0].Got != "true" {
		t.Errorf("re-pinning an old pin with no got records the got it failed with: %+v", c.KeptRed[0])
	}
}

func TestAKeptRedRunReportsLatencyAgainstTheLastRunThatFailedAsPinned(t *testing.T) {
	var delay atomic.Int64
	srv := newSlowFetchBackend(&delay)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: fetch, path: name, got: widget}
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: gadget}
`)
	cfg, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n")
	ctx := context.Background()
	if err := runRun(ctx, []string{"red", "-quiet"}); err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	delay.Store(150)
	var rerr error
	out := captureStdout(t, func() { rerr = runRun(ctx, []string{"red", "-quiet"}) })
	if rerr != nil {
		t.Fatalf("without latency.fail a slowdown is a warning: %v\n%s", rerr, out)
	}
	if !strings.Contains(out, "LATENCY: Fetch at step fetch") || !strings.Contains(out, "(the last run that failed as pinned)") {
		t.Fatalf("a kept-red chain has no safe spot, so latency is measured against its last as-pinned run:\n%s", out)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n    fail: true\n")
	out = captureStdout(t, func() { rerr = runRun(ctx, []string{"red", "-quiet"}) })
	if rerr == nil || !strings.Contains(rerr.Error(), "latency regression in red") {
		t.Fatalf("with latency.fail a confirmed slowdown fails the kept-red run: %v\n%s", rerr, out)
	}
}

func TestAKeptRedPinThatMovedLeadsAndAGonePinKeepsTheRunsLine(t *testing.T) {
	gone := "run: kept red, but it passed: the pinned defect is gone"
	chains := []*gateChain{
		{name: "confirm-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "confirm_short", Call: maskConfirm, Path: "order.status", Want: "PENDING", Got: "REJECTED", Pinned: "CONFIRMED"}}},
		{name: "list-slice", failed: true, keptRed: runner.KeptRedGone, first: gone, items: []gateItem{
			{Step: "list_pending", Call: maskList, Path: "orders.1", Rule: "exists", Want: "false", Got: "false", Pinned: "true", Passes: true}}},
		{name: "fixed-slice", failed: true, keptRed: runner.KeptRedGone, first: gone, items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "4", Got: "4", Pinned: "-1", Passes: true}}},
	}
	settleGate(chains)
	for i, want := range map[int]string{
		0: "confirm_short (OrderService/ConfirmOrder) order.status pinned got=CONFIRMED, now got=REJECTED",
		1: gone,
		2: gone,
	} {
		if chains[i].first != want {
			t.Errorf("%s: got %q, want %q", chains[i].name, chains[i].first, want)
		}
	}
	if chains[0].class != "not as pinned" {
		t.Errorf("got class %q", chains[0].class)
	}
	out := captureStdout(t, func() { printGateGroups(chains, false) })
	if strings.Contains(out, "GetProduct") || strings.Contains(out, "ListOrders") {
		t.Errorf("a pin that passes is no failure to group:\n%s", out)
	}
}

func TestAKeptRedDriftIsJudgedAgainstTheRunItDriftedFrom(t *testing.T) {
	create := shopStep("create_customer", "shop.customers.v1.CustomerService/CreateCustomer", `{"customer":{"id_customer":"c1"}}`)
	confirm := shopStep("confirm_3", maskConfirm, `{"order":{"id_order":"o3"}}`, "create_customer")
	list := shopStep("list_cancelled", maskList, `{"orders":[{"id_order":"o1"},{"id_order":"o2"}]}`, "create_customer")
	list.Status = runner.StatusFailed
	list.Expect = []chain.ExpectResult{{Path: "orders.1", Rule: "exists", Want: false, Got: true}}
	rec := shopRecord(create, confirm, list)
	held := map[string]bool{"list_cancelled orders.1": true}
	if b := pinnedAttribution(nil, rec, held).of("list_cancelled", "orders"); !b.blames() {
		t.Fatalf("judged by its expectations alone, the read changed after a write: %+v", b)
	}
	drift := []diff.Change{{Step: "list_cancelled", Path: "orders", Kind: diff.KindLength, Want: 3, Got: 2}}
	b := changesAttribution(nil, rec, drift).of("list_cancelled", "orders")
	if b.Kind != reasonSet || b.Path != "orders" {
		t.Errorf("against the run it drifted from, the writes answered as before, so the read is the suspect: %+v", b)
	}
}

func TestAGateGroupExampleIsARealChangeNotAMaskedPinThatNowPasses(t *testing.T) {
	chains := []*gateChain{
		{name: "stock-slice", failed: true, keptRed: runner.KeptRedNotAsPinned, items: []gateItem{
			{Step: "get_2", Call: maskGet, Path: "product.qty_on_hand", Want: "1", Got: "1", Pinned: "-1", Passes: true}}},
		{name: "lifecycle", failed: true, items: []gateItem{
			{Step: "get", Call: maskGet, Path: "product.qty_on_hand", Want: "5", Got: "4", Reason: reason{Kind: reasonSet, Step: "get", RPC: maskGet, Path: "product"}}}},
	}
	settleGate(chains)
	out := captureStdout(t, func() { printGateGroups(chains, false) })
	if !strings.Contains(out, "e.g. lifecycle get; suspect read get") {
		t.Errorf("the example is the step that changed, not the pin that now passes:\n%s", out)
	}
	if got := chains[0].items[0].wantGot(); got != "pinned got=-1, now got=1" {
		t.Errorf("a pin reads as pinned and now: %q", got)
	}
}

func TestANotAsPinnedSummaryPrintsEachReasonOnceAndLeadsWithTheFixtureLine(t *testing.T) {
	notSent := `not sent: ${create_order.order.id_order} reads step "create_order", which was refused`
	rec := &runner.Record{Chain: "red", Status: runner.StatusFailed, KeptRed: runner.KeptRedNotAsPinned,
		FailedSteps: []string{"create_order", "confirm_order"},
		Failure:     "kept_red: ran every step, as -keep-going does; 2 of 3 steps did not pass\nstep \"confirm_order\": " + notSent,
		KeptRedNote: `kept_red pins confirm_order status.code, but step "confirm_order" was not sent (why is on its line), so its pinned failure was not seen`,
		KeptRedNew:  runner.NewFailurePrefix + "create_order refused at transport: a; b; c; d"}
	out := runSummary(nil, rec, false, true, "fixture collision: step \"create_order\" ...", false)
	if strings.Contains(out, notSent) {
		t.Errorf("with the step lines shown above, the summary does not repeat their reasons:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 || !strings.HasPrefix(strings.TrimSpace(lines[2]), "fixture collision") {
		t.Errorf("the fixture line comes right after the verdict and the new failure:\n%s", out)
	}
	if quiet := runSummary(nil, rec, false, false, "", false); !strings.Contains(quiet, notSent) {
		t.Errorf("under -quiet no step line was printed, so the summary keeps the reason:\n%s", quiet)
	}
	if err := runVerdict(rec); err == nil || !strings.Contains(err.Error(), "and 3 more (listed above)") {
		t.Errorf("the exit message stays short: %v", err)
	}
}

func TestAKeptRedNoteOnACollisionIsOneShortSentence(t *testing.T) {
	note := keptRedNotJudged(`kept_red pins restock_a qty_on_hand got=10, but step "create_product_a" failed where nothing is pinned`)
	if !strings.HasPrefix(note, "not judged: kept_red pins restock_a qty_on_hand got=10,") || strings.Contains(note, "create_product_a") {
		t.Fatalf("after a fixture collision the step-by-step note says nothing about the defect: %q", note)
	}
}

func TestANotAsPinnedRunPrintsEachUnpinnedFailureOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "gadget"})
		}
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, filepath.Join(".shrt", "chains", "red.yaml"), `name: red
kept_red:
    - {step: create, path: id, got: thing-1}
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A, idempotency_key: "${uuid}"}
      expect:
          - {path: id, equals: thing-9}
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - {path: name, equals: widget}
`)
	var err error
	out := captureStdout(t, func() {
		err = runRun(context.Background(), []string{"red"})
	})
	if err == nil {
		t.Fatalf("a failure outside the pin is not as pinned:\n%s", out)
	}
	if n := strings.Count(out, "want=widget got=gadget"); n != 1 {
		t.Fatalf("with the step lines shown, the unpinned failure is printed once, got %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "NEW FAILURE outside the pinned defect: fetch") {
		t.Errorf("the new-failure line still names the step:\n%s", out)
	}
	out = captureStdout(t, func() {
		err = runRun(context.Background(), []string{"red", "-quiet"})
	})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "kept red (not_as_pinned)") && strings.Contains(line, "got=gadget") {
			t.Fatalf("the kept red line names the step and path and leaves the values to the NEW FAILURE line:\n%s", out)
		}
	}
	if !strings.Contains(out, "NEW FAILURE outside the pinned defect: fetch name want=widget got=gadget") {
		t.Fatalf("under -quiet no step line was printed, so the NEW FAILURE line carries the values:\n%s", out)
	}
}

func TestAKeptRedTotalRecomputedFromAnUnassertedPriceIsFiledUnderThePrice(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, filepath.Join(".shrt", "chains", "oversell.yaml"), `name: oversell
kept_red:
    - {step: fetch, path: order.lines.0.qty, got: "4"}
steps:
    - id: create_p1
      call: shop.catalog.v1.ProductService/CreateProduct
      body: {name: a, price_minor: 1000, sku: s-a}
      expect:
          - {path: status.code, equals: SUCCESS}
      export: {p1: product.id_product}
    - id: create_order
      call: shop.orders.v1.OrderService/CreateOrder
      body:
          id_customer: c1
          idempotency_key: ${uuid}
          lines: [{id_product: "${p1}", qty: 4}]
      expect:
          - {path: order.total_minor, equals: 4000}
      export: {oid: order.id_order}
    - id: fetch
      call: shop.orders.v1.OrderService/FetchOrder
      body: {id_order: "${oid}"}
      expect:
          - {path: order.lines.0.qty, equals: 9}
`)
	side := filepath.Join(t.TempDir(), "side.json")
	t.Setenv(gateReportEnv, side)
	run := func() (string, error) {
		var err error
		out := captureStdout(t, func() { err = runRun(context.Background(), []string{"oversell", "-quiet"}) })
		return out, err
	}
	if out, err := run(); err != nil || !strings.Contains(out, "FAILED AS PINNED") {
		t.Fatalf("the reference run fails as pinned: %v\n%s", err, out)
	}
	shop.priceBug = true
	if out, err := run(); err == nil || !strings.Contains(out, "NOT AS PINNED") {
		t.Fatalf("a lower total is a new failure: %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(side)
	var got gateSidecar
	if err := json.Unmarshal(raw, &got); err != nil || !got.PinsHeld || got.KeptRed != runner.KeptRedNotAsPinned {
		t.Fatalf("pins held: %v %s", err, raw)
	}
	if len(got.Items) != 2 || got.Items[0].Step != "create_p1" || got.Items[0].Path != "product.price_minor" || got.Items[0].Want != "1000" || got.Items[0].Got != "999" ||
		got.Items[1].Step != "create_order" || got.Items[1].suspect() != "create_p1" {
		t.Errorf("the price create_p1 answered unlike the reference run leads, and the total is filed under it: %+v", got.Items)
	}
}

func TestAnUnattributedItemElsewhereDoesNotClearAKeptRedItemsSuspect(t *testing.T) {
	const total = "order.total_minor"
	chains := []*gateChain{
		{name: "slice-a", failed: true, keptRed: runner.KeptRedNotAsPinned, pinsHeld: true, items: []gateItem{
			{Step: "create_p1", Call: shopCreate, Path: "product.price_minor", Want: "1250", Got: "1249", Failed: true},
			{Step: "create_order", Call: shopOrder, Path: total, Want: "6649", Got: "6644", Reason: reason{Kind: reasonWrite, Step: "create_p1", RPC: shopCreate}, Failed: true}}},
		{name: "slice-b", failed: true, keptRed: runner.KeptRedNotAsPinned, pinsHeld: true, items: []gateItem{
			{Step: "order_too_big", Call: shopOrder, Path: total, Want: "4250", Got: "4246", Failed: true}}},
	}
	settleGate(chains)
	if it := chains[0].items[1]; it.suspect() != "create_p1" {
		t.Errorf("a total recomputed from a changed price stays under the price: %+v", it)
	}
	if want := "create_order (OrderService/CreateOrder) order.total_minor want=6649 got=6644; suspect write create_p1 (ProductService/CreateProduct)"; chains[0].first != want {
		t.Errorf("got %q, want %q", chains[0].first, want)
	}
}

func TestAMovedPinOnAListWhoseOtherPinHeldIsAReorder(t *testing.T) {
	const list = "shop.orders.v1.OrderService/ListOrders"
	st := shopStep("list_cancelled", list, `{"orders":[{"id_order":"o3"},{"id_order":"o2"},{"id_order":"o1"}]}`).failing("orders.0.id_order", "o2", "o3")
	st.Expect = append(st.Expect, chain.ExpectResult{Path: "orders.1", Rule: "exists", Want: false, Got: true})
	rec := shopRecord(st)
	if own := runAttribution(nil, rec).of("list_cancelled", "orders.0.id_order").Kind; own != reasonSet {
		t.Fatalf("judged against its expectations, the extra item is another set, got %q", own)
	}
	held := map[string]bool{"list_cancelled orders.1": true}
	if own := pinnedAttribution(nil, rec, held).of("list_cancelled", "orders.0.id_order").Kind; own != reasonOrder {
		t.Fatalf("the extra item is the pinned defect, unchanged, so what moved is the order, got %q", own)
	}
}

const (
	maskList    = "shop.orders.v1.OrderService/ListOrders"
	maskConfirm = "shop.orders.v1.OrderService/ConfirmOrder"
	maskGet     = "shop.catalog.v1.ProductService/GetProduct"
)

func TestRunExitsZeroOnlyWhenAKeptRedChainFailsAsPinned(t *testing.T) {
	for _, tc := range []struct {
		status, kept string
		ok           bool
		says, head   string
	}{
		{runner.StatusFailed, runner.KeptRedAsPinned, true, "", "FAILED AS PINNED"},
		{runner.StatusFailed, runner.KeptRedNotAsPinned, false, "did not fail as pinned", "NOT AS PINNED"},
		{runner.StatusPassed, runner.KeptRedGone, false, "defect is gone", "PINNED DEFECT GONE"},
	} {
		rec := &runner.Record{Chain: "red", Status: tc.status, KeptRed: tc.kept, KeptRedNote: "kept_red pins s p"}
		err := runVerdict(rec)
		var coded *exitError
		if tc.ok != (err == nil) || (err != nil && (errors.As(err, &coded) || !strings.Contains(err.Error(), tc.says))) {
			t.Fatalf("%s/%s: must exit 0 only as pinned, else 1 saying %q, got %v", tc.status, tc.kept, tc.says, err)
		}
		if head, _, _ := strings.Cut(summary(rec, false), "\n"); !strings.Contains(head, tc.head) || strings.Contains(head, "PASSED in") {
			t.Fatalf("%s/%s: headline %q", tc.status, tc.kept, head)
		}
	}
}
