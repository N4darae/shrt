package runner

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestPinsHeldWhenEveryPinFailsAsPinnedAndOnlyOtherStepsFail(t *testing.T) {
	got := "${create.thing.id}"
	c := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create"}, {ID: "list"}},
		KeptRed: []chain.Pin{{Step: "list", Path: "things.0.id", Got: &got}}}
	rec := func(listed string) *Record {
		return &Record{KeptRed: KeptRedNotAsPinned, Steps: []*StepRecord{
			{ID: "create", Status: StatusFailed, Response: json.RawMessage(`{"thing":{"id":"t1","price":"249"}}`),
				Expect: []chain.ExpectResult{{Path: "thing.price", Rule: "equals", Want: "250", Got: "249"}}},
			{ID: "list", Status: StatusFailed, Response: json.RawMessage(`{"things":[{"id":"` + listed + `"}]}`),
				Expect: []chain.ExpectResult{{Path: "things.0.id", Rule: "equals", Want: "t0", Got: listed}}},
		}}
	}
	if !PinsHeld(c, rec("t1")) {
		t.Error("the pin failed as pinned, with its got resolved from the record; only an unpinned step failed besides")
	}
	if PinsHeld(c, rec("t9")) {
		t.Error("a pinned step that now gets another value did not fail as pinned")
	}
}
