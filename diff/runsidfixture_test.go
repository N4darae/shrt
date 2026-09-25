package diff_test

import (
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func unknownCustomerRun(id, tag string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "c", Status: runner.StatusPassed, Steps: []*runner.StepRecord{
		{ID: "get_customer_unknown", Call: "S/GetCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"id_customer":"cus-missing-` + tag + `"}`),
			Response: []byte(`{"status":{"code":"REJECTED","message":"no customer cus-missing-` + tag + `"}}`)},
	}}
}

func TestDiffMasksARefusalMessageEchoingAFixtureSentInAnIDShapedField(t *testing.T) {
	fixture := func(step, path string) bool { return step == "get_customer_unknown" && path == "id_customer" }
	rep := diff.CompareRunsSkipping(unknownCustomerRun("a", "ci17-a-1"), unknownCustomerRun("b", "ci17-b-1"), nil, diff.Fixtures{Named: fixture})
	if len(rep.Changes) != 0 || rep.FixtureEchoed != 1 {
		t.Fatalf("the refusal message only echoes the id the run sent, built from the fixture var, as verify masks it: %+v\n%s", rep.Changes, rep.Text())
	}
}
