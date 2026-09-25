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

const inNewFailure = "named on the NEW FAILURE line"

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
	unsentPinned, unsentOther, unpinnedFail := []string{}, []string{}, []string{}
	for _, step := range c.Steps {
		want := pins[step.ID]
		sr, ok := rec.Step(step.ID)
		if !ok || sr.Status == StatusSkipped {
			switch {
			case len(want) > 0 && ok:
				unsentPinned = append(unsentPinned, step.ID)
			case len(want) > 0:
				problems = append(problems, fmt.Sprintf("step %q was never answered, so its pinned failure was not seen", step.ID))
			case ok:
				unsentOther = append(unsentOther, step.ID)
			}
			continue
		}
		if sr.Status == StatusPassed {
			if len(want) > 0 {
				problems = append(problems, fmt.Sprintf("step %q passed although it is pinned failing on %s", step.ID, pinPaths(want)))
			}
			continue
		}
		if len(want) == 0 && step.AllowFail && sr.Status == StatusFailed && !sr.AssertionFailed() && !sr.Drift {
			continue
		}
		mismatch, fresh, unpinned := stepMismatch(step.ID, sr, want)
		problems = append(problems, mismatch...)
		found = append(found, fresh...)
		if unpinned {
			unpinnedFail = append(unpinnedFail, step.ID)
		}
	}
	if len(unpinnedFail) > 0 {
		problems = append(problems, fmt.Sprintf("%s failed where nothing is pinned (%s)", stepList(unpinnedFail), inNewFailure))
	}
	switch len(unsentPinned) {
	case 0:
	case 1:
		problems = append(problems, fmt.Sprintf("step %q was not sent (why is on its line), so its pinned failure was not seen", unsentPinned[0]))
	default:
		problems = append(problems, fmt.Sprintf("pinned %s were not sent (why is on their lines), so their pinned failures were not seen", stepList(unsentPinned)))
	}
	switch len(unsentOther) {
	case 0:
	case 1:
		problems = append(problems, fmt.Sprintf("step %q was not sent (why is on its line), so a regression there would not be seen", unsentOther[0]))
	default:
		problems = append(problems, fmt.Sprintf("%d other steps were not sent (%s), so a regression there would not be seen", len(unsentOther), capSteps(unsentOther, 5)))
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

func stepMismatch(id string, sr *StepRecord, want []chain.Pin) ([]string, []string, bool) {
	if sr.Status == StatusError {
		return []string{fmt.Sprintf("step %q is error (%s)", id, inNewFailure)}, []string{id + " is error: " + firstLine(sr.Error)}, false
	}
	if sr.Drift || !sr.AssertionFailed() {
		return []string{fmt.Sprintf("step %q failed with no failed expectation (%s)", id, inNewFailure)}, []string{id + " failed: " + firstLine(sr.Error)}, false
	}
	if refusal := pinnedRefusal(sr, want); refusal != "" {
		return []string{fmt.Sprintf("step %q: the pinned step was refused at transport, so its pinned failure was not seen (%s)", id, inNewFailure)},
			[]string{id + " refused at transport: " + refusal}, false
	}
	if sr.Transport != nil && len(want) == 0 {
		refusal := firstLine(strings.TrimSpace(sr.Transport.Code + ": " + sr.Transport.Message))
		return []string{fmt.Sprintf("step %q was refused at transport where nothing is pinned (%s)", id, inNewFailure)},
			[]string{id + " refused at transport: " + refusal}, false
	}
	out, fresh := []string{}, []string{}
	unpinned := false
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
			unpinned = true
			fresh = append(fresh, id+" "+chain.DescribeFailure(ex))
		}
	}
	for i, k := range want {
		if !seen[i] {
			out = append(out, fmt.Sprintf("step %q held on pinned path %s", id, k.Path))
		}
	}
	return out, fresh, unpinned
}

func stepList(ids []string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	if len(ids) == 1 {
		return "step " + quoted[0]
	}
	return "steps " + strings.Join(quoted, ", ")
}

func capSteps(ids []string, max int) string {
	if len(ids) <= max {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(ids[:max], ", "), len(ids)-max)
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
