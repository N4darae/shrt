package runner

import (
	"encoding/json"
	"fmt"
	"slices"
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

func keptRedVerdict(c *chain.Chain, rec *Record, scope *chain.Scope) (string, string, string) {
	if len(c.KeptRed) == 0 || rec.Status == StatusError {
		return "", "", ""
	}
	if rec.Status == StatusPassed {
		return KeptRedGone, "every step passed, so the defect kept_red pins (" + pinSummary(c.KeptRed) +
			") is gone: check the fix is the one intended, then remove kept_red and assert the corrected behaviour", ""
	}
	pins := pinsByStep(c.KeptRed)
	problems, found := []string{}, []string{}
	unsentPinned, unsentOther, unpinnedFail := []string{}, []string{}, []string{}
	maskedBy := ""
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
			for _, k := range resolvedPins(want, scope) {
				problems = append(problems, passedPin(step.ID, sr, k))
				if len(unpinnedFail) > 0 && maskedBy == "" {
					maskedBy = unpinnedFail[0]
				}
			}
			continue
		}
		if len(want) == 0 && step.AllowFail && sr.Status == StatusFailed && !sr.AssertionFailed() && !sr.Drift {
			continue
		}
		mismatch, fresh, unpinned := stepMismatch(step.ID, sr, resolvedPins(want, scope))
		problems = append(problems, mismatch...)
		found = append(found, fresh...)
		if unpinned {
			unpinnedFail = append(unpinnedFail, step.ID)
		}
	}
	if len(unpinnedFail) > 0 {
		problems = append(problems, fmt.Sprintf("%s failed where nothing is pinned (%s)", stepList(unpinnedFail), inNewFailure))
	}
	if maskedBy != "" {
		problems = append(problems, fmt.Sprintf("the pins that now pass come after %q failed, so they may be masked by that failure rather than fixed", maskedBy))
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
		problems = append(problems, fmt.Sprintf("%d other steps were not sent (%s), so a regression there would not be seen", len(unsentOther), capIDs(unsentOther, 5)))
	}
	if len(problems) == 0 {
		return KeptRedAsPinned, "failed exactly as kept_red pins: " + pinSummary(c.KeptRed), ""
	}
	finding := ""
	if len(found) > 0 {
		finding = NewFailurePrefix + strings.Join(found, "; ")
	}
	if len(problems) == 1 {
		return KeptRedNotAsPinned, "kept_red pins " + PinCount(len(c.KeptRed)) + ", but " + problems[0], finding
	}
	return KeptRedNotAsPinned, "kept_red pins " + PinCount(len(c.KeptRed)) + ", but:\n" + strings.Join(problems, "\n"), finding
}

func recordScope(rec *Record) *chain.Scope {
	scope := chain.NewScope(rec.Vars)
	for _, st := range rec.Steps {
		if st == nil {
			continue
		}
		var req, resp any
		_ = json.Unmarshal(st.Request, &req)
		_ = json.Unmarshal(st.Response, &resp)
		scope.Record(st.ID, req, resp)
	}
	return scope
}

func PinChanges(c *chain.Chain, rec *Record) (map[string]string, bool) {
	if rec == nil || rec.KeptRed != KeptRedNotAsPinned && rec.KeptRed != KeptRedGone {
		return nil, false
	}
	pins := pinsByStep(c.KeptRed)
	scope := recordScope(rec)
	changed, held := map[string]string{}, true
	for _, step := range c.Steps {
		want := pins[step.ID]
		if len(want) == 0 {
			continue
		}
		sr, ok := rec.Step(step.ID)
		if !ok || sr.Status == StatusSkipped || sr.Transport != nil {
			held = false
			continue
		}
		for _, k := range resolvedPins(want, scope) {
			seen := false
			for _, ex := range sr.Expect {
				if !namecase.Equal(k.Path, ex.Path) {
					continue
				}
				seen = seen || !ex.Passed
				if ex.Rule != unevaluatedRule && k.Got != nil && gotText(ex.Got) != *k.Got {
					changed[step.ID+" "+ex.Path] = *k.Got
				}
			}
			held = held && seen
		}
	}
	return changed, held && len(changed) == 0
}

func PinsHeld(c *chain.Chain, rec *Record) bool {
	if rec == nil || rec.KeptRed != KeptRedNotAsPinned {
		return false
	}
	pins := pinsByStep(c.KeptRed)
	scope := recordScope(rec)
	for _, step := range c.Steps {
		sr, ok := rec.Step(step.ID)
		if !ok || sr.Status == StatusSkipped {
			return false
		}
		want := pins[step.ID]
		if len(want) == 0 {
			continue
		}
		if mismatch, _, _ := stepMismatch(step.ID, sr, resolvedPins(want, scope)); sr.Status == StatusPassed || len(mismatch) > 0 {
			return false
		}
	}
	return true
}

func pinsByStep(kept []chain.Pin) map[string][]chain.Pin {
	pins := map[string][]chain.Pin{}
	for _, k := range kept {
		pins[k.Step] = append(pins[k.Step], k)
	}
	return pins
}

func resolvedPins(pins []chain.Pin, scope *chain.Scope) []chain.Pin {
	out := make([]chain.Pin, 0, len(pins))
	for _, k := range pins {
		if k.Got != nil && scope != nil && strings.Contains(*k.Got, "${") {
			text := *k.Got + " (does not resolve)"
			if v, err := scope.ResolveValue(*k.Got); err == nil {
				text = gotText(v)
			}
			k.Got = &text
		}
		out = append(out, k)
	}
	return out
}

func PinCount(n int) string {
	if n == 1 {
		return "1 path"
	}
	return fmt.Sprintf("%d paths", n)
}

func stepMismatch(id string, sr *StepRecord, want []chain.Pin) ([]string, []string, bool) {
	if sr.Status == StatusError {
		return []string{fmt.Sprintf("step %q is error (%s)", id, inNewFailure)}, []string{id + " is error: " + firstLine(sr.Error)}, false
	}
	if sr.Drift || !sr.AssertionFailed() {
		return []string{fmt.Sprintf("step %q failed with no failed expectation (%s)", id, inNewFailure)}, []string{id + " failed: " + firstLine(sr.Error)}, false
	}
	if sr.Transport != nil && (len(want) == 0 || pinnedRefusal(sr, want)) {
		what := "step %q was refused at transport where nothing is pinned (%s)"
		if len(want) > 0 {
			what = "step %q: the pinned step was refused at transport, so its pinned failure was not seen (%s)"
		}
		return []string{fmt.Sprintf(what, id, inNewFailure)},
			[]string{id + " refused at transport: " + firstLine(strings.TrimSpace(sr.Transport.Code+": "+sr.Transport.Message))}, false
	}
	out, fresh := []string{}, []string{}
	unpinned := false
	seen := map[int]bool{}
	kinds := listChangeKinds(sr)
	kindOf := func(n int) string {
		if kinds[n] == "" {
			return ""
		}
		return " (" + kinds[n] + ")"
	}
	for n, ex := range sr.Expect {
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
				out = append(out, fmt.Sprintf("%s %s: pinned got=%s, now got=%s%s", id, ex.Path, *k.Got, gotText(ex.Got), kindOf(n)))
			}
		}
		if !matched {
			unpinned = true
			fresh = append(fresh, id+" "+chain.DescribeFailure(ex)+kindOf(n))
		}
	}
	for i, k := range want {
		if !seen[i] {
			out = append(out, passedPin(id, sr, k))
		}
	}
	return out, fresh, unpinned
}

func passedPin(id string, sr *StepRecord, k chain.Pin) string {
	for _, ex := range sr.Expect {
		if ex.Passed && k.Got != nil && namecase.Equal(k.Path, ex.Path) {
			return fmt.Sprintf("%s %s: pinned got=%s, now got=%s, which passes", id, k.Path, chain.EdgeQuoted(*k.Got), chain.EdgeQuoted(gotText(ex.Got)))
		}
	}
	return fmt.Sprintf("%s %s: pinned failing, now passes", id, k.Path)
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

const unevaluatedRule = "unevaluated"

func pinnedRefusal(sr *StepRecord, want []chain.Pin) bool {
	return slices.ContainsFunc(sr.Expect, func(ex chain.ExpectResult) bool {
		return ex.Rule == unevaluatedRule && slices.ContainsFunc(want, func(k chain.Pin) bool { return namecase.Equal(k.Path, ex.Path) })
	})
}

func gotText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
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
