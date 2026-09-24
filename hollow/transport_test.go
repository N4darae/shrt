package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

func TestATransportAssertionIsNotEvidenceAReadReturnedData(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260924T000000Z-aaaa", Chain: "sweep", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch_rows", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`,
				[]chain.ExpectResult{{Path: "transport.code", Rule: "equals", Want: "ok", Got: "ok", Passed: true}}),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 1 {
		t.Fatalf("asserting only that the call was answered 200 says nothing about the rows; want it reported: %+v", rep.Findings)
	}
}

func TestARefusalProbeIsNotAHollowRead(t *testing.T) {
	runs := t.TempDir()
	probe := step("fetch_rows_no_token", "/acme.x.v1.S/FetchRows", `{"code":"unauthenticated","message":"no"}`,
		[]chain.ExpectResult{{Path: "transport.code", Rule: "equals", Want: "unauthenticated", Got: "unauthenticated", Passed: true}})
	probe.Transport = &runner.TransportError{Code: "unauthenticated", Message: "no"}
	writeRecord(t, runs, &runner.Record{
		RunID: "20260924T000000Z-bbbb", Chain: "probe", Status: "passed",
		Steps: []*runner.StepRecord{probe},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 0 || rep.ReadSteps != 0 {
		t.Fatalf("a read refused as the step asserted returned no rows by design, not by accident: %+v", rep)
	}
}

func TestATransportAssertionDoesNotMarkAChainAsAssertingData(t *testing.T) {
	c := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "s", Call: "S/FetchRows",
		Expect: []chain.Expectation{{Path: "transport.http_status", Equals: 200}}}}}
	if len(hollow.DataAsserted([]*chain.Chain{c})) != 0 {
		t.Fatal("a transport assertion is metadata, like the envelope")
	}
}
