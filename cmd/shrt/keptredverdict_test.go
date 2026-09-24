package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestRunExitsZeroOnlyWhenAKeptRedChainFailsAsPinned(t *testing.T) {
	for _, tc := range []struct {
		status, kept string
		ok           bool
		says         string
	}{
		{runner.StatusFailed, runner.KeptRedAsPinned, true, ""},
		{runner.StatusFailed, runner.KeptRedNotAsPinned, false, "did not fail as pinned"},
		{runner.StatusPassed, runner.KeptRedGone, false, "defect is gone"},
	} {
		err := runVerdict(&runner.Record{Chain: "red", Status: tc.status, KeptRed: tc.kept})
		if tc.ok != (err == nil) {
			t.Fatalf("%s/%s: got %v", tc.status, tc.kept, err)
		}
		var coded *exitError
		if err != nil && (errors.As(err, &coded) || !strings.Contains(err.Error(), tc.says)) {
			t.Fatalf("%s/%s: must exit 1 saying %q, got %v", tc.status, tc.kept, tc.says, err)
		}
	}
	rec := &runner.Record{Chain: "red", Status: runner.StatusFailed, KeptRed: runner.KeptRedAsPinned, KeptRedNote: "failed exactly as kept_red pins: s p"}
	if out := summary(rec, false); !strings.Contains(out, "FAILED AS PINNED") || !strings.Contains(out, "failed exactly as kept_red pins") {
		t.Fatalf("the summary must say the chain failed as pinned, got:\n%s", out)
	}
}
