package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestAReadAssertingNothingIsCountedApartFromEnvelopeOnly(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		Chain: "catalog-reads", RunID: "r1", Status: "passed",
		Steps: []*runner.StepRecord{
			step("get_bare", "/shop.catalog.v1.ProductService/GetProduct", `{}`, nil),
			step("get_envelope", "/shop.catalog.v1.ProductService/GetProduct", `{}`,
				[]chain.ExpectResult{{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true}}),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.ReadSteps != 2 {
		t.Fatalf("want 2 read steps, got %+v", rep)
	}
	if rep.EnvelopeOnly != 1 || rep.AssertsNothing != 1 {
		t.Fatalf("get_bare asserts nothing, not the envelope verdict: want 1 envelope-only and 1 asserting "+
			"nothing, got %d and %d", rep.EnvelopeOnly, rep.AssertsNothing)
	}
	if rep.HollowRecords != 2 || rep.Unallowed != 2 {
		t.Fatalf("both reads passed with an empty body and are hollow, got %+v", rep)
	}
}
