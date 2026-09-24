package runner

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

const (
	KeptRedAsPinned    = "as_pinned"
	KeptRedNotAsPinned = "not_as_pinned"
	KeptRedGone        = "defect_gone"
)

const NewFailurePrefix = "NEW FAILURE outside the pinned defect: "

func keptRedVerdict(c *chain.Chain, rec *Record) (string, string, string) {
	if len(c.KeptRed) == 0 || rec.Status == StatusError {
		return "", "", ""
	}
	if rec.Status == StatusPassed {
		return KeptRedGone, "every step passed, so the defect kept_red pins (" + pinSummary(c.KeptRed) +
			") is gone: check the fix is the one intended, then remove kept_red and assert the corrected behaviour", ""
	}
	pins := map[string][]chain.Pin{}
	for _, k := range c.KeptRed {
		pins[k.Step] = append(pins[k.Step], k)
	}
	problems, found := []string{}, []string{}
	for _, step := range c.Steps {
		want := pins[step.ID]
		sr, ok := rec.Step(step.ID)
		if !ok || sr.Status == StatusSkipped {
			switch {
			case len(want) > 0 && ok:
				problems = append(problems, fmt.Sprintf("step %q was not sent (%s), so its pinned failure was not seen", step.ID, firstLine(sr.Error)))
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
		mismatch, fresh := stepMismatch(step.ID, sr, want)
		problems = append(problems, mismatch...)
		found = append(found, fresh...)
	}
	if len(problems) == 0 {
		return KeptRedAsPinned, "failed exactly as kept_red pins: " + pinSummary(c.KeptRed), ""
	}
	finding := ""
	if len(found) > 0 {
		finding = NewFailurePrefix + strings.Join(found, "; ")
	}
	return KeptRedNotAsPinned, "kept_red pins " + pinSummary(c.KeptRed) + ", but " + strings.Join(problems, "; "), finding
}

func stepMismatch(id string, sr *StepRecord, want []chain.Pin) ([]string, []string) {
	if sr.Status == StatusError {
		return []string{fmt.Sprintf("step %q is error: %s", id, sr.Error)}, []string{id + " is error: " + firstLine(sr.Error)}
	}
	if sr.Drift || !sr.AssertionFailed() {
		return []string{fmt.Sprintf("step %q failed with no failed expectation: %s", id, sr.Error)}, []string{id + " failed: " + firstLine(sr.Error)}
	}
	if refusal := pinnedRefusal(sr, want); refusal != "" {
		return []string{fmt.Sprintf("step %q: the pinned step was refused at transport: %s, so its pinned failure was not seen", id, refusal)},
			[]string{id + " refused at transport: " + refusal}
	}
	out, fresh := []string{}, []string{}
	seen := map[int]bool{}
	for _, ex := range sr.Expect {
		if ex.Passed {
			continue
		}
		matched := false
		for i, k := range want {
			if !namecase.Equal(k.Path, ex.Path) {
				continue
			}
			matched = true
			seen[i] = true
			if ex.Rule == unevaluatedRule {
				out = append(out, fmt.Sprintf("step %q: pinned path %s was not evaluated (%s), so its pinned failure was not seen", id, ex.Path, ex.Detail))
				continue
			}
			if k.Got != nil && gotText(ex.Got) != *k.Got {
				out = append(out, fmt.Sprintf("step %q failed on %s with got=%s, not the pinned got=%s", id, ex.Path, gotText(ex.Got), *k.Got))
			}
		}
		if !matched {
			out = append(out, fmt.Sprintf("step %q failed where nothing is pinned: %s", id, strings.TrimPrefix(ex.String(), "FAIL ")))
			fresh = append(fresh, id+" "+chain.DescribeFailure(ex))
		}
	}
	for i, k := range want {
		if !seen[i] {
			out = append(out, fmt.Sprintf("step %q held on pinned path %s", id, k.Path))
		}
	}
	return out, fresh
}

const unevaluatedRule = "unevaluated"

func pinnedRefusal(sr *StepRecord, want []chain.Pin) string {
	if sr.Transport == nil {
		return ""
	}
	for _, ex := range sr.Expect {
		if ex.Rule != unevaluatedRule {
			continue
		}
		for _, k := range want {
			if namecase.Equal(k.Path, ex.Path) {
				return firstLine(strings.TrimSpace(sr.Transport.Code + ": " + sr.Transport.Message))
			}
		}
	}
	return ""
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
