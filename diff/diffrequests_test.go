package diff_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func listStep(call, profile, request string) *runner.StepRecord {
	return &runner.StepRecord{ID: "list", Call: call, Procedure: "/shop.catalog.v1.ProductService/ListProducts",
		AuthProfile: profile, Status: runner.StatusPassed, Request: json.RawMessage(request), Response: json.RawMessage(`{"n":1}`)}
}

func TestRunDiffReportsRequestAndPrincipalDifferences(t *testing.T) {
	a := runOf("run-a", listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	b := runOf("run-b", listStep("ListProducts", "clerk", `{"sku_prefix":"zzz9"}`))
	rep := diff.CompareRunsMasking(a, b, nil)
	text := rep.Text()
	if rep.Same() || strings.Contains(text, "no differences") {
		t.Fatalf("the runs sent different requests as different principals:\n%s", text)
	}
	if !strings.Contains(text, "sku_prefix") || !strings.Contains(text, "zzz2") || !strings.Contains(text, "auth_profile") {
		t.Fatalf("name the request and principal differences:\n%s", text)
	}
}

func TestARespelledCallIsTheSameCall(t *testing.T) {
	a := runOf("run-a", listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	b := runOf("run-b", listStep("shop.catalog.v1.ProductService/ListProducts", "default", `{"sku_prefix":"zzz2"}`))
	if rep := diff.CompareRunsMasking(a, b, nil); !rep.Same() {
		t.Fatalf("the same rpc spelled two ways is no difference:\n%s", rep.Text())
	}
	spot := &store.SafeSpot{Chain: "thing-flow", RunID: "spot", Steps: []*runner.StepRecord{listStep("ListProducts", "default", `{"sku_prefix":"zzz2"}`)}}
	now := &chain.Chain{Name: "thing-flow", Steps: []*chain.Step{{ID: "list", Call: "shop.catalog.v1.ProductService/ListProducts"}}}
	if changes := diff.ChainChanges(spot, now); len(changes) != 0 {
		t.Fatalf("respelling the call is not a chain change: %+v", changes)
	}
	rep := diff.CompareMasking(spot, b, nil)
	if !rep.Clean() {
		t.Fatalf("verify must not report the respelled call as an order change:\n%s", rep.Text())
	}
}
