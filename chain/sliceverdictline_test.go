package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestARerunVerdictReplacesTheHypothesisAndTheEarlierRerun(t *testing.T) {
	description := "Slice of src reproducing step t: 2 of 3 steps, mode closure.\n\n" +
		"Computed by 'shrt chain slice'.\n" +
		"\nThis slice is a HYPOTHESIS until it is run. A dependency that is state rather than a\n" +
		"reference leaves no trace in the YAML, so a slice can be too small and still go green.\n" +
		"\n2 dropped step(s) WRITE: a, b.\n\n" +
		"RE-RUN by 'shrt chain slice -verify': reproduced on 2026-09-23: run r1.\n"
	got := chain.RecordRerun(description, "not reproduced on 2026-09-24: run r2")
	if strings.Contains(got, "HYPOTHESIS") || strings.Contains(got, "run r1") {
		t.Fatalf("the re-run verdict replaces the hypothesis and the earlier re-run:\n%s", got)
	}
	if strings.Count(got, "RE-RUN by") != 1 || !strings.Contains(got, "run r2") || !strings.Contains(got, "2 dropped step(s) WRITE") {
		t.Fatalf("one re-run line, the rest kept:\n%s", got)
	}
	again := chain.RecordRerun(got, "reproduced on 2026-09-25: run r3")
	if strings.Count(again, "RE-RUN by") != 1 || strings.Contains(again, "run r2") {
		t.Fatalf("a later re-run replaces the earlier one:\n%s", again)
	}
}
