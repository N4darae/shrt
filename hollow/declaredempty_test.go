package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

func TestExistsFalseOnAWholeRowDeclaresTheEmptyAnswer(t *testing.T) {
	no := false
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0", Rule: "exists", Want: no, Got: false, Passed: true},
	})
	if rep.EnvelopeOnly != 0 || rep.Unallowed != 0 {
		t.Fatalf("'assets.0 exists: false' fails the moment the read returns a row, like 'total equals 0', "+
			"so it is the author pinning emptiness, not a read asserting only the envelope. got %+v", rep)
	}
}

func TestExistsFalseInsideARowStillDoesNotRescueAnEmptyRead(t *testing.T) {
	no := false
	rep := recordWith(t, []chain.ExpectResult{
		{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true},
		{Path: "assets.0.deleted_at", Rule: "exists", Want: no, Got: false, Passed: true},
	})
	if rep.Unallowed != 1 {
		t.Fatalf("absence of a field inside a row passes on a full list whose rows lack it and on an "+
			"empty one alike, so it does not say emptiness was the answer; got %+v", rep)
	}
}

func TestAPinnedRefusalIsNotAHollowRead(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		Chain: "catalog-refusals", RunID: "r1", Status: "passed",
		Steps: []*runner.StepRecord{
			step("get_product_unknown", "/shop.catalog.v1.ProductService/GetProduct",
				`{"status":{"code":"REJECTED","details":[{"app_code":1204,"reason":"ProductNotFound"}]},"product":null}`,
				[]chain.ExpectResult{
					{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true},
					{Path: "status.details.0.app_code", Rule: "equals", Want: 1204, Got: 1204, Passed: true},
				}),
			step("get_product_ok_empty", "/shop.catalog.v1.ProductService/GetProduct",
				`{"status":{"code":"SUCCESS"},"product":null}`,
				[]chain.ExpectResult{
					{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
				}),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 1 || rep.Findings[0].Step != "get_product_ok_empty" {
		t.Fatalf("a step pinning a non-OK verdict asserted a refusal, and a refusal's empty body is the "+
			"pass by construction; only the OK-envelope read that found nothing is hollow. got %+v", rep)
	}
}

func TestNotEqualOKOnTheEnvelopeAlsoDeclaresARefusal(t *testing.T) {
	if !hollow.DeclaresRefusal([]chain.ExpectResult{{Path: "error.code", Rule: "not_equal", Want: "OK", Passed: true}}) {
		t.Error("not_equal OK on the envelope says the call was refused")
	}
	if hollow.DeclaresRefusal([]chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "OK", Passed: true}}) {
		t.Error("equals OK is the success verdict, not a refusal")
	}
	if hollow.DeclaresRefusal([]chain.ExpectResult{{Path: "error.code", Rule: "not_equal", Want: "NEVER", Passed: true}}) {
		t.Error("not_equal against a value other than OK passes on success too")
	}
}

func TestChainSourceAgreesAboutDeclaredEmptiness(t *testing.T) {
	no := false
	c := &chain.Chain{Name: "sweep", Steps: []*chain.Step{
		{ID: "row_absent", Call: "ThingService/Fetch", Expect: []chain.Expectation{{Path: "rows.0", Exists: &no}}},
		{ID: "refused", Call: "ThingService/Fetch", Expect: []chain.Expectation{{Path: "error.code", Equals: "not_found"}}},
		{ID: "field_absent", Call: "ThingService/Fetch", Expect: []chain.Expectation{{Path: "rows.0.id", Exists: &no}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	asserted := hollow.DataAsserted([]*chain.Chain{c})
	if !asserted["sweep\x00row_absent"] || !asserted["sweep\x00refused"] {
		t.Errorf("the chain-source predicate must agree with the record one, got %v", asserted)
	}
	if asserted["sweep\x00field_absent"] {
		t.Error("absence of a field inside a row declares nothing")
	}
}
