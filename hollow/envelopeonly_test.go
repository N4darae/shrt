package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestARefusalProbePinningAnAppCodeIsNotCountedAsEnvelopeOnly(t *testing.T) {
	defer chain.SetEnvelope("", "")
	chain.SetEnvelope("status.code", "SUCCESS")
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		Chain: "catalog-refusals", RunID: "r1", Status: "passed",
		Steps: []*runner.StepRecord{
			step("get_unknown", "/shop.catalog.v1.ProductService/GetProduct",
				`{"status":{"code":"REJECTED","details":[{"app_code":1204,"reason":"ProductNotFound"}]}}`,
				[]chain.ExpectResult{
					{Path: "status.code", Rule: "equals", Want: "REJECTED", Got: "REJECTED", Passed: true},
					{Path: "status.details.0.app_code", Rule: "equals", Want: 1204, Got: 1204, Passed: true},
					{Path: "status.details.0.reason", Rule: "equals", Want: "ProductNotFound", Got: "ProductNotFound", Passed: true},
				}),
			step("get_ok", "/shop.catalog.v1.ProductService/GetProduct",
				`{"status":{"code":"SUCCESS","message":"ok"},"product":{"id":"p1"}}`,
				[]chain.ExpectResult{
					{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
					{Path: "status.message", Rule: "not_empty", Got: "ok", Passed: true},
				}),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.ReadSteps != 2 {
		t.Fatalf("want 2 read steps, got %+v", rep)
	}
	if rep.EnvelopeOnly != 1 {
		t.Fatalf("only get_ok asserts nothing but the envelope; a probe pinning app_code and reason asserts the "+
			"refusal's detail, got %d envelope-only", rep.EnvelopeOnly)
	}
}
