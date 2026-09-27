package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

func TestSliceWithoutVerifyNamesTheFieldAnUnchangedLeftOutWriteMoves(t *testing.T) {
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"s.v1.Stock/AddBatch": {Effects: contract.Effects{"qty_on_hand": {Increase: "lines.qty"}}},
			"s.v1.Order/Confirm":  {Effects: contract.Effects{"qty_on_hand": {Decrease: "lines.qty"}}},
		},
	}})
	rec := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "batch", Call: "s.v1.Stock/AddBatch"},
		{ID: "confirm", Call: "s.v1.Order/Confirm", Expect: []chain.ExpectResult{{Path: "status.code", Got: "REJECTED"}}},
		{ID: "read", Call: "s.v1.Product/Get", Expect: []chain.ExpectResult{{Path: "product.qty_on_hand", Got: 3}}},
		{ID: "name", Call: "s.v1.Product/Get", Expect: []chain.ExpectResult{{Path: "product.name", Got: "x"}}},
	}}
	same := func(s string) string { return s }
	if got := movedFieldsRead(lib, same, rec, []string{"batch"}, []string{"confirm"}); len(got) != 1 || got[0] != "qty_on_hand" {
		t.Fatalf("Confirm decreases the level the batch increases: %v", got)
	}
	if got := movedFieldsRead(lib, same, rec, []string{"batch"}, []string{"read"}); len(got) != 1 {
		t.Fatalf("a failed expectation on qty_on_hand reads what the batch moves: %v", got)
	}
	if got := movedFieldsRead(lib, same, rec, []string{"batch"}, []string{"name"}); len(got) != 0 {
		t.Fatalf("a failure on name reads nothing the batch moves: %v", got)
	}

	v := &withoutVerdict{Without: []string{"batch"}, SourceRun: "r1", Cleared: []string{"read"}, StillFail: []string{"confirm"},
		LikelyFault: "read", asBefore: "answered as in run r0", Stores: []string{"qty_on_hand"}}
	out := v.text()
	for _, want := range []string{"so it is a precondition, or it stores other than it answers: it moves qty_on_hand", "  still fail: confirm\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "not the fault") || strings.Contains(out, "another cause") {
		t.Fatalf("answering as before does not clear a write whose stored effect the failures read:\n%s", out)
	}
}

func TestSliceWithoutVerifySaysStillFailingStepsGotOtherValues(t *testing.T) {
	v := &withoutVerdict{Without: []string{"confirm"}, SourceRun: "r1", StillFail: []string{"a", "b"}, OtherValues: []string{"a", "b"}}
	out := v.text()
	if !strings.Contains(out, "still fail in the run without it, with other values: a, b (the left-out write moves them)") {
		t.Fatalf("the left-out write moved what they read:\n%s", out)
	}
	v.OtherValues = []string{"b"}
	out = v.text()
	if !strings.Contains(out, "still fail in the run without it: a\n") || !strings.Contains(out, "still fail, with other values: b") {
		t.Fatalf("only b moved:\n%s", out)
	}
}
