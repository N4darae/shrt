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

func TestSliceWithoutVerifySaysALeftOutWriteHadNoEffectItsContractPromises(t *testing.T) {
	lib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/contract/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"s.v1.Order/Cancel": {Effects: contract.Effects{"qty_on_hand": {Restore: "CONFIRMED"}}},
			"s.v1.Order/Note":   {},
		},
	}})
	rec := &runner.Record{Steps: []*runner.StepRecord{
		{ID: "cancel", Call: "s.v1.Order/Cancel"},
		{ID: "note", Call: "s.v1.Order/Note"},
		{ID: "read", Call: "s.v1.Product/Get", Expect: []chain.ExpectResult{{Path: "product.qty_on_hand", Got: 5}}},
		{ID: "name", Call: "s.v1.Product/Get", Expect: []chain.ExpectResult{{Path: "product.name", Got: "x"}}},
	}}
	same := func(s string) string { return s }
	got := noEffectOf(lib, same, rec, []string{"cancel", "note"}, []string{"read", "name"})
	if len(got) != 1 || got[0].Step != "cancel" || got[0].Field != "qty_on_hand" || len(got[0].Reads) != 1 || got[0].Reads[0] != "read" {
		t.Fatalf("cancel restores qty_on_hand, which read gets unchanged: %+v", got)
	}
	if got := noEffectOf(lib, same, rec, []string{"note"}, []string{"read"}); len(got) != 0 {
		t.Fatalf("note moves nothing: %+v", got)
	}

	v := &withoutVerdict{Without: []string{"cancel"}, SourceRun: "r1", StillFail: []string{"read", "name"}, NoEffect: got}
	out := v.text()
	for _, want := range []string{
		"verify without cancel: cancel had no effect on qty_on_hand: read read the same value without it, though its contract says qty_on_hand: {restore: CONFIRMED}\n",
		"  still fail: name\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NOT REPRODUCED") || !strings.Contains(v.err().Error(), "cancel had no effect on qty_on_hand") {
		t.Fatalf("a write that did not do what its contract says is not a bare NOT REPRODUCED:\n%s\n%v", out, v.err())
	}
	v.NoEffect = nil
	if !strings.Contains(v.text(), "verify NOT REPRODUCED without cancel") {
		t.Fatalf("without a contract effect the verdict stays:\n%s", v.text())
	}
}
