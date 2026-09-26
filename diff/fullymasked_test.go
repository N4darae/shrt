package diff_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func TestDiffWarnsWhenAVolatilePatternMasksEveryField(t *testing.T) {
	a := runOf("a", stepAs("get", runner.StatusPassed, `{"product":{"price_minor":"250","name":"Widget"}}`))
	b := runOf("b", stepAs("get", runner.StatusPassed, `{"product":{"price_minor":"99999","name":"Widget"}}`))
	a.Volatile = []string{"**"}
	b.Volatile = []string{"**"}
	rep := diff.CompareRuns(a, b)
	if len(rep.FullyMasked) != 1 || rep.FullyMasked[0] != "get" {
		t.Fatalf("every field of step get is volatile, so nothing of it was compared: %+v", rep.FullyMasked)
	}
	text := rep.Text()
	if !strings.Contains(text, "WARNING") || !strings.Contains(text, "compared nothing") {
		t.Fatalf("\"no differences\" with only a not-shown count reads as a clean result; warn loudly:\n%s", text)
	}

	a.Volatile, b.Volatile = []string{"**.created_at"}, []string{"**.created_at"}
	if rep := diff.CompareRuns(a, b); len(rep.FullyMasked) != 0 || strings.Contains(rep.Text(), "WARNING") {
		t.Fatalf("a narrow pattern masks nothing wholesale: %+v\n%s", rep.FullyMasked, rep.Text())
	}
}
