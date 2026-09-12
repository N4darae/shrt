package hollow_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

func writeRecord(t *testing.T, runsDir string, rec *runner.Record) {
	t.Helper()
	dir := filepath.Join(runsDir, rec.Chain)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rec.RunID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func envelopeOK() []chain.ExpectResult {
	return []chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "OK", Got: "OK", Passed: true}}
}

func step(id, procedure, body string, expect []chain.ExpectResult) *runner.StepRecord {
	return &runner.StepRecord{
		ID: id, Procedure: procedure, Status: "passed",
		Response: json.RawMessage(body), Expect: expect,
	}
}

func scan(t *testing.T, runsDir string, allow *hollow.Allowlist, asserted map[string]bool) *hollow.Report {
	t.Helper()
	rep, err := hollow.Scan(runsDir, allow, asserted)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return rep
}

func emptyAllow(t *testing.T) *hollow.Allowlist {
	t.Helper()
	a, err := hollow.LoadAllowlist(filepath.Join(t.TempDir(), "absent.txt"), false)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestScanReportsAPassingReadThatFoundNothing(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-aaaa", Chain: "sweep", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch_rows", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`, envelopeOK()),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 1 || len(rep.Findings) != 1 {
		t.Fatalf("want one reported finding, got %d reported of %d: %+v", rep.Unallowed, len(rep.Findings), rep.Findings)
	}
	f := rep.Findings[0]
	if f.Chain != "sweep" || f.Step != "fetch_rows" || f.RPC != "FetchRows" || f.RunID != "20260912T000000Z-aaaa" {
		t.Fatalf("finding does not name chain, step, rpc and run: %+v", f)
	}
}

func TestScanIgnoresWritesNonPassingStepsAndDataAssertions(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-bbbb", Chain: "mixed", Status: "passed",
		Steps: []*runner.StepRecord{
			step("create", "/acme.x.v1.S/CreateThing", `{"error":{"code":"OK"}}`, envelopeOK()),
			step("found_rows", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[{"id":"1"}]}`, envelopeOK()),
			step("asserts_data", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`,
				append(envelopeOK(), chain.ExpectResult{Path: "rows.0.id", Rule: "not_empty"})),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	rep.Findings = nil
	if rep.Unallowed != 0 {
		t.Fatalf("nothing should be reported here, got %d: %+v", rep.Unallowed, rep)
	}
	if rep.ReadSteps != 2 {
		t.Fatalf("want 2 passed read steps counted, got %d", rep.ReadSteps)
	}
	if rep.EnvelopeOnly != 1 {
		t.Fatalf("want 1 envelope-only read step, got %d", rep.EnvelopeOnly)
	}
	if rep.HollowRecords != 0 {
		t.Fatalf("want 0 hollow step records, got %d", rep.HollowRecords)
	}
}

func TestScanSkipsStepsWhoseStatusIsNotPassed(t *testing.T) {
	runs := t.TempDir()
	rec := &runner.Record{
		RunID: "20260912T000000Z-cccc", Chain: "failing", Status: "failed",
		Steps: []*runner.StepRecord{
			step("fetch_rows", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`, envelopeOK()),
		},
	}
	rec.Steps[0].Status = "failed"
	writeRecord(t, runs, rec)
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.ReadSteps != 0 || rep.Unallowed != 0 {
		t.Fatalf("a step that did not pass is not a hollow pass: %+v", rep)
	}
}

func TestScanSparesAStepWhoseChainNowAssertsADataPath(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-dddd", Chain: "sweep", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch_rows", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`, envelopeOK()),
		},
	})
	asserted := hollow.DataAsserted([]*chain.Chain{{
		Name: "sweep",
		Steps: []*chain.Step{{ID: "fetch_rows", Expect: []chain.Expectation{
			{Path: "error.code", Equals: "OK"},
			{Path: "rows.0.id_asset", NotEmpty: true},
		}}},
	}})
	rep := scan(t, runs, emptyAllow(t), asserted)
	if rep.Unallowed != 0 || rep.ChainFixed != 1 {
		t.Fatalf("want the step spared as chain-asserts-data, got %+v", rep)
	}
}

func TestScanCountsAnAllowlistedStepWithoutReportingIt(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-eeee", Chain: "probe", Status: "passed",
		Steps: []*runner.StepRecord{
			step("reject_bad_id", "/acme.x.v1.S/FetchRows", `{"error":{"code":"invalid_argument"},"rows":[]}`,
				[]chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "invalid_argument", Passed: true}}),
		},
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "allow.txt")
	if err := os.WriteFile(path, []byte("probe reject_bad_id the filter refuses before the read runs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	allow, err := hollow.LoadAllowlist(path, true)
	if err != nil {
		t.Fatal(err)
	}
	rep := scan(t, runs, allow, nil)
	if rep.Allowed != 1 || rep.Unallowed != 0 {
		t.Fatalf("want 1 allowlisted and 0 reported, got %+v", rep)
	}
	if rep.Findings[0].Reason == "" {
		t.Fatal("an allowlisted finding must carry the reason it was allowed")
	}
}

func TestScanFailsWhenItReadNoRecordsAtAll(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "absent")} {
		rep, err := hollow.Scan(dir, emptyAllow(t), nil)
		if !errors.Is(err, hollow.ErrNoRecords) {
			t.Fatalf("%s: want ErrNoRecords, got %v", dir, err)
		}
		if rep.Unallowed != 0 {
			t.Fatalf("%s: a report from no evidence must not claim findings", dir)
		}
	}
}

func TestBodyIsEmptyTreatsAProtoJSONZeroAsNothingFound(t *testing.T) {
	empty := []string{
		`{"error":{"code":"OK"}}`,
		`{"error":{"code":"OK"},"rows":[]}`,
		`{"error":{"code":"OK"},"page":{}}`,
		`{"error":{"code":"OK"},"snapshot":null}`,
		`{"error":{"code":"OK"},"name":""}`,
		`{"error":{"code":"OK"},"ok":false}`,
		`{"error":{"code":"OK"},"count":0}`,
		`{"error":{"code":"OK"},"total_qty":"0"}`,
		`{"error":{"code":"OK"},"opening":"0","closing":"0","line":[]}`,
	}
	for _, body := range empty {
		if !hollow.BodyIsEmpty(json.RawMessage(body)) {
			t.Errorf("want empty: %s", body)
		}
	}
	full := []string{
		`{"error":{"code":"OK"},"rows":[{}]}`,
		`{"error":{"code":"OK"},"total_qty":"1"}`,
		`{"error":{"code":"OK"},"id":"01a0"}`,
		`{"error":{"code":"OK"},"ok":true}`,
		`{"error":{"code":"OK"},"count":3}`,
		`{"error":{"code":"OK"},"page":{"n":1}}`,
	}
	for _, body := range full {
		if hollow.BodyIsEmpty(json.RawMessage(body)) {
			t.Errorf("want non-empty: %s", body)
		}
	}
}

func TestIsReadProcedure(t *testing.T) {
	for _, p := range []string{
		"/acme.x.v1.S/FetchRows", "/acme.x.v1.S/ListRows", "/acme.x.v1.S/GetRow",
		"/acme.x.v1.S/SearchRows", "/acme.x.v1.S/PreviewFee",
	} {
		if !hollow.IsReadProcedure(p) {
			t.Errorf("%s should be a read", p)
		}
	}
	for _, p := range []string{
		"/acme.x.v1.S/CreateRow", "/acme.x.v1.S/UpdateRow", "/acme.x.v1.S/RunDayEnd",
		"/acme.x.v1.S/ApproveThing", "/acme.x.v1.S/RefetchRow",
	} {
		if hollow.IsReadProcedure(p) {
			t.Errorf("%s should not be a read", p)
		}
	}
}

func TestScanReportsAReadThatAssertsNothingAtAll(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-cccc", Chain: "silent", Status: "passed",
		Steps: []*runner.StepRecord{
			step("no_expectations", "/acme.x.v1.S/FetchRows", `{"error":{"code":"OK"},"rows":[]}`, nil),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 1 {
		t.Fatalf("a read that asserted NOTHING and found nothing went unreported: it is strictly weaker "+
			"than one asserting only the envelope, which this gate exists to catch, so excluding it "+
			"exempted the worst case from the check built for the milder one. got %+v", rep)
	}
}

func TestScanSeesThroughACanonicalisedMessageOfZeroValues(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-dddd", Chain: "normalised", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch_level", "/acme.x.v1.S/FetchLevel",
				`{"error":{"code":"OK"},"level":{"available":"0","id_sku":"","on_hand":"0","reserved":"0"}}`,
				envelopeOK()),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 1 {
		t.Fatalf("a message the backend never populated came back filled with proto zero values, and the "+
			"map looked non-empty — shrt's own canonicalisation would then hide every Fetch returning a "+
			"message from the gate that exists to find them. got %+v", rep)
	}
}

func TestScanStillIgnoresAMessageThatCarriesRealData(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, &runner.Record{
		RunID: "20260912T000000Z-eeee", Chain: "populated", Status: "passed",
		Steps: []*runner.StepRecord{
			step("fetch_level", "/acme.x.v1.S/FetchLevel",
				`{"error":{"code":"OK"},"level":{"available":"0","id_sku":"sku-1","on_hand":"0"}}`,
				envelopeOK()),
		},
	})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Unallowed != 0 {
		t.Fatalf("one populated field is data: reporting this would make the gate cry wolf. got %+v", rep)
	}
}
