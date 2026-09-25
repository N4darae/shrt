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
	if err == nil || !strings.HasSuffix(strings.TrimSpace(out), "pin it: shrt chain pin probe-orders") {
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
		"wrote .shrt/chains/probe-orders.yaml: the pinned step(s) left out",
		"verify reproduced",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n > 5 {
		t.Fatalf("pin prints one line per written file and the verdict, got %d lines:\n%s", n, out)
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
