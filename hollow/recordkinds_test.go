package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestScanSaysWhichKindsOfRecordItCounted(t *testing.T) {
	runs := t.TempDir()
	read := func() []*runner.StepRecord {
		return []*runner.StepRecord{step("fetch", "/acme.x.v1.S/FetchRows", `{"rows":[{"id":"1"}]}`, envelopeOK())}
	}
	writeRecord(t, runs, &runner.Record{RunID: "20260912T000000Z-run1", Chain: "c", Status: "passed", Steps: read()})
	writeRecord(t, runs, &runner.Record{RunID: "20260912T000001Z-rep1", Chain: "c", Status: "passed", ReplayOf: "20260912T000000Z-run1", Steps: read()})
	writeRecord(t, runs, &runner.Record{RunID: "20260912T000002Z-red1", Chain: "c", Status: "failed", KeptRed: runner.KeptRedAsPinned, Steps: read()})
	rep := scan(t, runs, emptyAllow(t), nil)
	if rep.Records != 3 || rep.ReplayRecords != 1 || rep.KeptRedRecords != 1 || rep.FailedRecords != 1 {
		t.Fatalf("records=%d replays=%d kept_red=%d failed=%d, want 3, 1, 1, 1",
			rep.Records, rep.ReplayRecords, rep.KeptRedRecords, rep.FailedRecords)
	}
}
