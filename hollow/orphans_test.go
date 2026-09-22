package hollow_test

import (
	"testing"

	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/runner"
)

func passedRead(chainName, runID string) *runner.Record {
	return &runner.Record{
		Chain:  chainName,
		RunID:  runID,
		Status: "passed",
		Steps:  []*runner.StepRecord{step("fetch", "/acme.v1.ThingService/FetchThing", `{"rows":[]}`, envelopeOK())},
	}
}

func TestScanKnownSeparatesRunsOfChainsThatNoLongerExist(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, passedRead("live-chain", "20260101T000000Z-aaaaaaaa"))
	writeRecord(t, runs, passedRead("deleted-chain", "20260101T000000Z-bbbbbbbb"))
	writeRecord(t, runs, passedRead("deleted-chain", "20260102T000000Z-cccccccc"))

	rep, err := hollow.ScanKnown(runs, &hollow.Allowlist{}, nil, map[string]bool{"live-chain": true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Records != 1 {
		t.Fatalf("records = %d, want 1: deleting a chain leaves its run directory behind, and a "+
			"finding in one names a chain that no longer exists, so it cannot be acted on and must "+
			"not sit in a ratcheted total", rep.Records)
	}
	if rep.OrphanRecords != 2 {
		t.Errorf("orphan records = %d, want 2", rep.OrphanRecords)
	}
	if len(rep.Orphans) != 1 || rep.Orphans[0] != "deleted-chain" {
		t.Errorf("the orphaned directory must be named so it can be deleted: %v", rep.Orphans)
	}
	for _, f := range rep.Findings {
		if f.Chain == "deleted-chain" {
			t.Error("a finding against a chain that no longer exists cannot be acted on")
		}
	}
}

func TestScanCountsEverythingWhenTheChainsAreUnknown(t *testing.T) {
	runs := t.TempDir()
	writeRecord(t, runs, passedRead("live-chain", "20260101T000000Z-aaaaaaaa"))
	writeRecord(t, runs, passedRead("deleted-chain", "20260101T000000Z-bbbbbbbb"))

	rep, err := hollow.Scan(runs, &hollow.Allowlist{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Records != 2 {
		t.Fatalf("with no chain list to compare against, every record still counts: %d", rep.Records)
	}
	if len(rep.Orphans) != 0 {
		t.Errorf("nothing can be called an orphan without knowing which chains exist: %v", rep.Orphans)
	}
}
