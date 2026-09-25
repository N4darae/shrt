package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/hollow"
)

func TestHollowGateSaysWhatRecordsItCounted(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "hollow-baseline")
	writeFile(t, baseline, "0\n")
	rep := &hollow.Report{Records: 19, ReplayRecords: 7, KeptRedRecords: 4, FailedRecords: 5}
	var err error
	out := captureStdout(t, func() { err = hollowGate(rep, baseline, ".shrt/hollow-allow", nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"from 19 run record(s)", "12 shrt run", "7 verify replay", "4 of chains kept red", "5 that did not pass"} {
		if !strings.Contains(out, want) {
			t.Fatalf("want %q in:\n%s", want, out)
		}
	}
}
