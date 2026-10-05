package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const handReproChain = `apiVersion: shrt/v1
name: repro-confirm
vars:
    tag: rc
steps:
  - id: create_product
    call: ProductService/CreateProduct
    body: {sku: "sku-${vars.tag}", price_minor: "100"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - id: add_stock
    call: StockService/AddStock
    body: {id_product: "${create_product.product.id_product}", qty: "10"}
    expect:
      - {path: qty_on_hand, equals: "10"}
  - id: create_customer
    call: CustomerService/CreateCustomer
    body: {email: "rc-${vars.tag}@example.test"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - id: create_order
    call: OrderService/CreateOrder
    body:
      id_customer: ${create_customer.customer.id_customer}
      lines:
        - {id_product: "${create_product.product.id_product}", qty: "2"}
      idempotency_key: ${uuid}
    expect:
      - {path: status.code, equals: SUCCESS}
  - id: peek
    call: ProductService/GetProduct
    body: {id_product: "${create_product.product.id_product}"}
    expect:
      - {path: product.qty_on_hand, equals: "10"}
  - id: confirm_order
    call: OrderService/ConfirmOrder
    body: {id_order: "${create_order.order.id_order}"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - id: get_after_confirm
    call: ProductService/GetProduct
    body: {id_product: "${create_product.product.id_product}"}
    expect:
      - {path: product.qty_on_hand, equals: "8"}
  - id: restock
    call: StockService/AddStock
    body: {id_product: "${create_product.product.id_product}", qty: "1"}
    expect:
      - {path: qty_on_hand, equals: "9"}
`

const handReproPath = ".shrt/scratch/repro-confirm.yaml"

func handReproShop(t *testing.T, bug bool, chainText string) *fakeShop {
	t.Helper()
	shop := newFakeShop()
	shop.stockInProduct, shop.confirmExtraUnit = true, bug
	chdirToFakeShop(t, shop)
	writeFile(t, handReproPath, chainText)
	return shop
}

func runOut(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), args) })
	return out + errText(err), exitCodeOf(err)
}

func TestRunRepeatProvesAHandWrittenChainAsWrittenAndLeavesItUnchanged(t *testing.T) {
	handReproShop(t, true, handReproChain)
	out, code := runOut(t, handReproPath, "-repeat", "3")
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	for _, want := range []string{
		"repeat 2 of 3: run ", "get_after_confirm, restock, as run 1",
		"reproduced 3/3: get_after_confirm, restock failed the same way in every run\n",
		"  get_after_confirm (GetProduct): answered status.code \"SUCCESS\"\n",
		"    failed: product.qty_on_hand want=8 got=7\n",
		"  restock (AddStock): answered status.code \"SUCCESS\"\n",
		"in .shrt/runs/repro-confirm\nexit 0: reproduced 3/3\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if raw := string(mustRead(t, handReproPath)); raw != handReproChain {
		t.Fatalf("-repeat changed the chain:\n%s", raw)
	}
	if ids := runIDsOf(t, "repro-confirm"); len(ids) != 3 {
		t.Fatalf("each repeat is a saved run, got %v", ids)
	}
	out, code = runOut(t, handReproPath, "-repeat", "2", "-keep-going=false", "-quiet")
	if code != 0 || !strings.Contains(out, "reproduced 2/2: get_after_confirm failed the same way in every run") ||
		!strings.Contains(out, "  not run: 1 later step (restock)") || strings.Contains(out, "qty_on_hand want=9") || strings.Contains(out, "repeat 1 of 2") {
		t.Fatalf("-keep-going=false stops each run at its first failed step and says what it left unchecked; -quiet drops the per-repeat lines: exit %d\n%s", code, out)
	}
	var v repeatVerdict
	raw, code := runOut(t, handReproPath, "-repeat", "2", "-json")
	if err := json.Unmarshal([]byte(raw), &v); err != nil || code != 0 || v.Outcome != sliceReproduced || v.Same != 2 || len(v.Runs) != 2 || v.Failed[0] != "get_after_confirm" {
		t.Fatalf("-json: exit %d, %v, %+v\n%s", code, err, v, raw)
	}
}

func TestRunRepeatSaysWhatDifferedWhenTheRunsDoNotFailAlike(t *testing.T) {
	handReproShop(t, false, strings.Replace(handReproChain, "rc-${vars.tag}@example.test", "rc-fixed@example.test", 1))
	out, code := runOut(t, handReproPath, "-repeat", "3")
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out)
	}
	for _, want := range []string{
		"NOT REPRODUCED: 1 of 3 runs failed as run 1 did\n",
		"): failed steps: source none, run 2 create_customer, get_after_confirm, restock\n",
		"repeat 3 of 3: run ", "failed at create_customer, get_after_confirm, restock, not as run 1",
		"\nexit 1: NOT REPRODUCED, 1 of 3 runs failed as run 1 did\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestRunRepeatGivesEachLaterRunAFreshFixtureAndCallsAllGreenNothingToReproduce(t *testing.T) {
	handReproShop(t, false, handReproChain)
	out, code := runOut(t, handReproPath, "-repeat", "3", "-var", "tag=mine")
	if code != 1 || !strings.Contains(out, "passed 3/3: no step failed in any run\n") ||
		!strings.Contains(out, "in .shrt/runs/repro-confirm\nexit 1: passed 3/3, nothing reproduced\n") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, code = runOut(t, handReproPath, "-repeat", "2")
	if code != 1 || !strings.Contains(out, "passed 2/2") {
		t.Fatalf("a declared fixture var gets a fresh value from run 2 on, so the email is not taken: exit %d\n%s", code, out)
	}
	for _, args := range [][]string{{"-repeat", "1"}, {"-repeat", "3", "-dry-run"}} {
		if out, code := runOut(t, append([]string{handReproPath}, args...)...); code != 1 || !strings.Contains(out, "-repeat compares the verdicts of 2 or more real runs") {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
}

func TestRunRepeatAgainstADeadTargetHasNoVerdict(t *testing.T) {
	handReproShop(t, true, handReproChain)
	writeFile(t, ".shrt/config.yaml", strings.Replace(string(mustRead(t, ".shrt/config.yaml")), "base_url: ", "base_url: "+deadCLITarget(t)+" #", 1))
	out, code := runOut(t, handReproPath, "-repeat", "3")
	if code != 3 || !strings.Contains(out, "DID NOT RUN: run 1 (") || strings.Contains(out, "reproduced") || strings.Contains(out, "repeat 2 of 3") ||
		!strings.Contains(out, "\nexit 3: DID NOT RUN\n") {
		t.Fatalf("exit %d, want 3 and no second run:\n%s", code, out)
	}
}

func TestRunRepeatSaysWhatAFailedStepAnsweredNotItsStatus(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	for _, c := range []struct {
		st   runner.StepRecord
		want string
	}{
		{runner.StepRecord{Status: runner.StatusFailed, Response: json.RawMessage(`{"status":{"code":"SUCCESS"}}`)}, `answered status.code "SUCCESS"`},
		{runner.StepRecord{Status: runner.StatusFailed, Response: json.RawMessage(`{"status":{"code":"REJECTED","message":"gone","details":[{"reason":"OrderNotFound"}]}}`)},
			`answered status.code "REJECTED" message="gone" reason=OrderNotFound`},
		{runner.StepRecord{Status: runner.StatusFailed, HTTPStatus: 500, Transport: &runner.TransportError{Code: "internal", Message: "boom"}}, "answered HTTP 500 internal: boom"},
		{runner.StepRecord{Status: runner.StatusError, Error: "connection refused\nre-run"}, "got no answer: connection refused"},
	} {
		if got := answeredText(&c.st); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestRunRefusesEveryChainErrorAtOnceAndCountsLintWarnings(t *testing.T) {
	broken := strings.Replace(handReproChain, "{path: qty_on_hand, equals: \"10\"}", "{path: product.qty_on_hand, equals: \"10\"}", 1)
	broken = strings.Replace(broken, "${create_customer.customer.id_customer}", "${create_custmer.customer.id_customer}", 1)
	broken = strings.Replace(broken, "@example.test\"}\n    expect:\n      - {path: status.code, equals: SUCCESS}\n", "@example.test\"}\n", 1)
	shop := handReproShop(t, false, broken)
	out, code := runOut(t, handReproPath)
	if code != 1 || !strings.Contains(out, "nothing was sent, 2 chain errors:\n  [add_stock] expect product.qty_on_hand: no such field") ||
		!strings.Contains(out, "\n  [create_order] ${create_custmer.customer.id_customer} names no step of this chain (did you mean \"create_customer\"?)") ||
		!strings.Contains(out, "\nchain lint has 1 warning too: shrt chain lint "+handReproPath) {
		t.Fatalf("exit %d\n%q", code, out)
	}
	if shop.next != 0 || shop.addCalls != 0 {
		t.Fatalf("a refused chain sends nothing, the shop saw %d creates and %d stock calls", shop.next, shop.addCalls)
	}
}

func TestRunRefusesAnExpectPathTheResponseHasNoFieldForAsAChainError(t *testing.T) {
	shop := handReproShop(t, false, strings.Replace(handReproChain, "{path: qty_on_hand, equals: \"10\"}", "{path: product.qty_on_hand, equals: \"10\"}", 1))
	for _, args := range [][]string{{handReproPath}, {handReproPath, "-dry-run"}, {handReproPath, "-repeat", "3"}} {
		out, code := runOut(t, args...)
		if code != 1 || !strings.Contains(out, "chain repro-confirm: nothing was sent, 1 chain error:\n  [add_stock] expect product.qty_on_hand: no such field in AddStockResponse") ||
			strings.Contains(out, "suspect") {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
	}
	if shop.next != 0 || shop.addCalls != 0 {
		t.Fatalf("a refused chain sends nothing, the shop saw %d creates and %d stock calls", shop.next, shop.addCalls)
	}
	if _, err := os.Stat(".shrt/runs/repro-confirm"); err == nil {
		t.Fatalf("a refused chain leaves no run record")
	}
}

func TestRunRepeatComparesTheVerdictOfAStepThatFailedInEveryRun(t *testing.T) {
	shop := handReproShop(t, true, handReproChain)
	shop.getProductFailAt = map[int]bool{4: true, 5: true}
	out, code := runOut(t, handReproPath, "-repeat", "3")
	if code != 1 || !strings.Contains(out, "NOT REPRODUCED: 2 of 3 runs failed as run 1 did") ||
		!strings.Contains(out, "): get_after_confirm: transport: source none, run 2 HTTP 500 internal: boom\n") || strings.Contains(out, "): get_after_confirm: transport: source none, run 3") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}
