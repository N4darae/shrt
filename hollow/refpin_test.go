package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

func TestAReferencePinOfTheOkValueIsEnvelopeOnly(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-ref1", Chain: "hol", Status: "passed",
		Steps: []*runner.StepRecord{
			step("list_b", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`, envelopeOK()),
		},
	})
	pin := func(vars map[string]any, want string) map[string]bool {
		return hollow.DataAsserted([]*chain.Chain{{
			Name: "hol", Vars: vars,
			Steps: []*chain.Step{{ID: "list_b", Expect: []chain.Expectation{{Path: "error.code", Equals: want}}}},
		}})
	}
	for name, asserted := range map[string]map[string]bool{
		"var holding the ok value": pin(map[string]any{"ok": "OK"}, "${vars.ok}"),
		"reference to a step":      pin(nil, "${create.error.code}"),
	} {
		rep := scan(t, runs, emptyAllow(t), asserted)
		if rep.Unallowed != 1 || rep.ChainFixed != 0 {
			t.Errorf("%s: the pin resolves to the ok value, or cannot be resolved without a run, so the step asserts "+
				"only the envelope and its empty read must be reported; got %+v", name, rep)
		}
	}
	rep := scan(t, runs, emptyAllow(t), pin(map[string]any{"bad": "not_found"}, "${vars.bad}"))
	if rep.ChainFixed != 1 {
		t.Errorf("a var holding a refusal code pins a refusal, which is not an envelope-only read; got %+v", rep)
	}
}
