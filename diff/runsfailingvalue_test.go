package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func echoFailureRun(id, tag, got string) *runner.Record {
	return &runner.Record{RunID: id, Chain: "customers", Status: runner.StatusFailed, Vars: map[string]any{"tag": tag}, Steps: []*runner.StepRecord{
		{ID: "create_customer", Call: "S/CreateCustomer", Status: runner.StatusPassed,
			Request:  []byte(`{"email":"cust-` + tag + `@example.test","name":"Customer ` + tag + `"}`),
			Response: []byte(`{"customer":{"email":"cust-` + tag + `@example.test","name":"Customer ` + tag + `"}}`)},
		{ID: "get_customer", Call: "S/GetCustomer", Status: runner.StatusFailed,
			Response: []byte(`{"customer":{"email":"cust-` + tag + `@example.test","name":"` + got + `"}}`),
			Expect: []chain.ExpectResult{
				{Path: "customer.email", Rule: "equals", Want: "cust-" + tag + "@example.test", Got: "cust-" + tag + "@example.test", Passed: true},
				{Path: "customer.name", Rule: "equals", Want: "Customer " + tag, Got: got, Passed: false},
			}},
	}}
}

func TestDiffShowsTheFailingValuesWhenBothRunsFailedAtTheSameStep(t *testing.T) {
	fixture := func(step, path string) bool { return step == "create_customer" }
	a := echoFailureRun("a", "b6a", "cust-b6a@example.test")
	b := echoFailureRun("b", "b6b", "cust-b6b@example.test")
	text := diff.CompareRunsSkipping(a, b, nil, diff.Fixtures{Named: fixture}).Text()
	for _, want := range []string{
		"first failing step unchanged: get_customer, failing the same way in both: the values differ only by the fixture name each run sent",
		"A: customer.name want=Customer b6a got=cust-b6a@example.test",
		"B: customer.name want=Customer b6b got=cust-b6b@example.test",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("both runs failed at get_customer; the diff shows the failing values of each, masking the echo only in the comparison, want %q:\n%s", want, text)
		}
	}
	c := echoFailureRun("c", "b6c", "Somebody else")
	text = diff.CompareRunsSkipping(a, c, nil, diff.Fixtures{Named: fixture}).Text()
	if !strings.Contains(text, "first failing step unchanged: get_customer, failing differently") {
		t.Fatalf("a got that is not the other run's echo is a different failure:\n%s", text)
	}
}
