package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/hollow"
)

func TestHollowRecordCountsSplitIntoDisjointParts(t *testing.T) {
	line := recordsCounted(&hollow.Report{Records: 37, ReplayRecords: 9, KeptRedRecords: 10, FailedRecords: 15, FailedKeptRedRecords: 10})
	for _, want := range []string{"28 shrt run and 9 verify replay(s)", "22 passed and 15 did not pass", "of chains kept red: 0 passed, 10 did not pass"} {
		if !strings.Contains(line, want) {
			t.Errorf("want %q, so each split adds up to the record count and kept red is a named part of it: %s", want, line)
		}
	}
	if strings.Contains(line, "10 of chains kept red, 15 that did not pass") {
		t.Errorf("kept red and did-not-pass overlap, so they cannot be listed as if they were separate counts: %s", line)
	}
}
