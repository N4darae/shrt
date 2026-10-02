package main

import (
	"strings"
	"testing"
)

func TestARegressionStatesTheFactsAndOnlyVSaysHowToApproveIt(t *testing.T) {
	shop := newFakeShop()
	chdirToFakeShop(t, shop)
	writeFile(t, ".shrt/chains/probe-orders.yaml", strings.Replace(cancelConfirmedChain, "vars:\n    tag: probe\n", "", 1))
	shrtOut(t, "run", "probe-orders")
	shrtOut(t, "confirm", "probe-orders", "-note", "orders flow")
	if _, code := shrtOut(t, "confirm", "probe-orders", "-approve", "-by", "alice@example.test"); code != 0 {
		t.Fatal("approve failed")
	}
	shop.cancelConfirmedBug = true
	out, code := shrtOut(t, "verify", "probe-orders")
	if code != 1 || !strings.Contains(out, "DRIFT (regression)") || strings.Contains(out, "-supersede") {
		t.Errorf("a regression keeps the facts only, exit %d:\n%s", code, out)
	}
	out, _ = shrtOut(t, "verify", "probe-orders", "-v")
	if !strings.Contains(out, "If intended, a person approves a passing run: shrt confirm probe-orders -supersede") {
		t.Errorf("-v says how an intended change is approved:\n%s", out)
	}
}
