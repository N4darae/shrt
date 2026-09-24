package runner

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
)

const (
	KeptRedAsPinned    = "as_pinned"
	KeptRedNotAsPinned = "not_as_pinned"
	KeptRedGone        = "defect_gone"
)

func keptRedVerdict(c *chain.Chain, rec *Record) (string, string) {
	if len(c.KeptRed) == 0 || rec.Status == StatusError {
		return "", ""
	}
	if rec.Status == StatusPassed {
		return KeptRedGone, "every step passed, so the defect kept_red pins (" + pinSummary(c.KeptRed) +
			") is gone: check the fix is the one intended, then remove kept_red and assert the corrected behaviour"
	}
	pins := map[string][]chain.Pin{}
	for _, k := range c.KeptRed {
		pins[k.Step] = append(pins[k.Step], k)
	}
	problems := []string{}
	for _, step := range c.Steps {
		want := pins[step.ID]
		sr, ok := rec.Step(step.ID)
		if !ok || sr.Status == StatusSkipped {
			switch {
			case len(want) > 0:
				problems = append(problems, fmt.Sprintf("step %q was never answered, so its pinned failure was not seen", step.ID))
			case ok:
				problems = append(problems, fmt.Sprintf("step %q was not sent (%s), so a regression there would not be seen", step.ID, firstLine(sr.Error)))
			}
			continue
		}
		if sr.Status == StatusPassed {
			if len(want) > 0 {
				problems = append(problems, fmt.Sprintf("step %q passed, but kept_red pins it failing on %s", step.ID, pinPaths(want)))
			}
			continue
		}
		if len(want) == 0 && step.AllowFail && sr.Status == StatusFailed && !sr.AssertionFailed() && !sr.Drift {
			continue
		}
		problems = append(problems, stepMismatch(step.ID, sr, want)...)
	}
	if len(problems) == 0 {
		return KeptRedAsPinned, "failed exactly as kept_red pins: " + pinSummary(c.KeptRed)
	}
	return KeptRedNotAsPinned, "kept_red pins " + pinSummary(c.KeptRed) + ", but " + strings.Join(problems, "; ")
}

func stepMismatch(id string, sr *StepRecord, want []chain.Pin) []string {
	if sr.Status == StatusError {
		return []string{fmt.Sprintf("step %q is error: %s", id, sr.Error)}
	}
	if sr.Drift || !sr.AssertionFailed() {
		return []string{fmt.Sprintf("step %q failed with no failed expectation: %s", id, sr.Error)}
	}
	out := []string{}
	seen := map[int]bool{}
	for _, ex := range sr.Expect {
		if ex.Passed {
			continue
		}
		matched := false
		for i, k := range want {
			if k.Path != ex.Path {
				continue
			}
			matched = true
			seen[i] = true
			if k.Got != nil && gotText(ex.Got) != *k.Got {
				out = append(out, fmt.Sprintf("step %q failed on %s with got=%s, not the pinned got=%s", id, ex.Path, gotText(ex.Got), *k.Got))
			}
		}
		if !matched {
			out = append(out, fmt.Sprintf("step %q failed where nothing is pinned: %s", id, strings.TrimPrefix(ex.String(), "FAIL ")))
		}
	}
	for i, k := range want {
		if !seen[i] {
			out = append(out, fmt.Sprintf("step %q held on pinned path %s", id, k.Path))
		}
	}
	return out
}

func gotText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func pinPaths(pins []chain.Pin) string {
	paths := make([]string, 0, len(pins))
	for _, k := range pins {
		paths = append(paths, k.Path)
	}
	return strings.Join(paths, ", ")
}

func pinSummary(pins []chain.Pin) string {
	parts := make([]string, 0, len(pins))
	for _, k := range pins {
		s := k.Step + " " + k.Path
		if k.Got != nil {
			s += " got=" + *k.Got
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
