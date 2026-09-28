package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestTheHeadlineLeadsWithAStepsTransportError(t *testing.T) {
	report := &diff.Report{Changes: []diff.Change{
		{Step: "get", Path: "status", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusError},
		{Step: "get", Path: "customer", Kind: diff.KindType, Want: nil, Got: map[string]any{}},
		{Step: "list", Path: "total", Kind: diff.KindChanged, Want: 1, Got: 2},
	}}
	if first, steps := firstChange(report, nil); first == nil || first.Kind != diff.KindStatus || steps != 2 {
		t.Fatalf("a transport error leads, got %+v over %d step(s)", first, steps)
	}
	report.Changes[0].Got = runner.StatusFailed
	if first, _ := firstChange(report, nil); first.Path != "customer" {
		t.Fatalf("a plain failure yields to the step's first change, got %+v", first)
	}
}

func TestAReadNoLongerRefusedIsItsOwnSuspect(t *testing.T) {
	chain.SetEnvelope("status.code", "SUCCESS")
	defer chain.SetEnvelope("", "")
	const get = "shop.customers.v1.CustomerService/GetCustomer"
	rec := shopRecord(shopStep("get_unknown", get, `{"customer":{"name":""},"status":{"code":"SUCCESS"}}`).failing("status.code", "REJECTED", "SUCCESS"))
	if own := runAttribution(nil, rec).of("get_unknown", "customer").own; own != "GetCustomer answers SUCCESS where it answered REJECTED" {
		t.Fatalf("got %q", own)
	}
}

func TestTheHeadlineLeadsWithTheFirstFailingStepOverAnEarlierDrift(t *testing.T) {
	report := &diff.Report{Changes: []diff.Change{
		{Step: "login", Path: "expires_at", Kind: diff.KindChanged, Want: 1, Got: 1000},
		{Step: "add", Path: "qty_on_hand", Kind: diff.KindChanged, Want: 0, Got: 9},
		{Step: "add", Path: "status.code", Kind: diff.KindStatus, Want: runner.StatusPassed, Got: runner.StatusFailed},
	}}
	rec := &runner.Record{Steps: []*runner.StepRecord{{ID: "login", Status: runner.StatusPassed}, {ID: "add", Status: runner.StatusFailed}}}
	if first, steps := firstChange(report, rec); first == nil || first.Step != "add" || first.Path != "qty_on_hand" || steps != 2 {
		t.Fatalf("the failing step leads, got %+v over %d step(s)", first, steps)
	}
	if first, _ := firstChange(report, nil); first.Step != "login" {
		t.Fatalf("without a record the first change leads, got %+v", first)
	}
}

func TestVerifyLeadsWithTheChangedWriteAFailingReadObserves(t *testing.T) {
	rec := shopRecord(
		shopStep("create_order", shopOrder, `{"order":{"id_order":"o1","total_minor":"7"}}`),
		shopStep("fetch_order", shopFetch, `{"order":{"id_order":"o1","total_minor":"7"}}`, "create_order").failing("order.total_minor", "9", "7"),
	)
	report := &diff.Report{Changes: []diff.Change{
		{Step: "create_order", Path: "order.total_minor", Kind: diff.KindChanged, Want: "9", Got: "7"},
		{Step: "fetch_order", Path: "order.total_minor", Kind: diff.KindChanged, Want: "9", Got: "7"},
	}}
	if c, _ := firstChange(report, rec); c == nil || c.Step != "create_order" {
		t.Errorf("first change %+v, want the write the failing read observes", c)
	}
}
