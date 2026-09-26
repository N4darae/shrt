package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestTheNextCommandLeavesOutAKeptStepThatFailedInTheSourceRun(t *testing.T) {
	res := &chain.SliceResult{
		Source: "confirm-all-lines", Target: "stock_b", Mode: chain.SliceModeClosure,
		DroppedWrites: []chain.Dropped{{Index: 3, ID: "stock", Call: "StockService/AddStockBatch"}},
		UnderIncluded: true,
		Chain: &chain.Chain{Steps: []*chain.Step{
			{ID: "confirm", Call: "OrderService/ConfirmOrder"},
			{ID: "stock_b", Call: "ProductService/GetProduct"},
		}},
	}
	rec := &runner.Record{RunID: "r1", Steps: []*runner.StepRecord{
		{Index: 3, ID: "stock", Status: runner.StatusPassed},
		{Index: 6, ID: "confirm", Status: runner.StatusFailed},
		{Index: 7, ID: "stock_b", Status: runner.StatusFailed},
	}}
	v := &sliceVerdict{Step: "stock_b"}
	v.suggestKeep(res, rec, sliceVerifyArgs{keep: []string{"confirm"}}, []string{"stock"})
	if v.Next == "" {
		t.Fatalf("stock passed in the source run, so a next: command can keep it: %+v", v)
	}
	if strings.Contains(v.Next, "-keep confirm") || strings.Contains(v.Next, ",confirm") {
		t.Fatalf("confirm failed in the source run, so a next: command keeping it stops there and can only give DID NOT RUN: %s", v.Next)
	}
	if !strings.Contains(v.Next, "-keep stock ") {
		t.Fatalf("next: must keep the dropped write that passed: %s", v.Next)
	}
	if !strings.Contains(v.Reason, "confirm (failed)") || !strings.Contains(v.Reason, "-keep") {
		t.Fatalf("the reason must say the -keep id was left out and why: %s", v.Reason)
	}
}
