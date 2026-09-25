package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

var serverErrorCodes = map[string]bool{
	"internal": true, "unknown": true, "resource_exhausted": true, "data_loss": true, "aborted": true, "deadline_exceeded": true,
}

type flakyStep struct {
	step     *runner.StepRecord
	sameRun  []string
	moved    string
	answered string
	repeated string
	resent   bool
}

func (f flakyStep) sufficient() bool { return len(f.sameRun) > 0 || f.moved != "" || f.resent }

type intermittentFailure struct {
	steps  []flakyStep
	weak   []flakyStep
	hidden []string
	rate   string
}

func serverError(st *runner.StepRecord) string {
	if st == nil || st.Transport == nil || (st.Status != runner.StatusFailed && st.Status != runner.StatusError) {
		return ""
	}
	if runner.NotAnsweredByService(st) || st.HTTPStatus == 0 {
		return ""
	}
	code := strings.ToLower(st.Transport.Code)
	if !serverErrorCodes[code] && st.HTTPStatus < 500 {
		return ""
	}
	return errorText(st)
}

func resentAnswered(st *runner.StepRecord) bool {
	if st == nil || st.FirstAttempt == nil || serverError(st) != "" || !answeredByService(st) {
		return false
	}
	return serverAttempt(st.FirstAttempt)
}

func serverAttempt(a *runner.Attempt) bool {
	code := strings.ToLower(a.Code)
	return code == "unavailable" || serverErrorCodes[code] || a.HTTPStatus >= 500
}

func stepOf(rec *runner.Record, id string) (*runner.StepRecord, bool) {
	if rec == nil {
		return nil, false
	}
	return rec.Step(id)
}

func errorText(st *runner.StepRecord) string {
	if st == nil || st.Transport == nil {
		return ""
	}
	if st.Transport.Message == "" {
		return st.Transport.Code
	}
	return st.Transport.Code + ": " + st.Transport.Message
}

func answeredAsExpected(st *runner.StepRecord) bool {
	return st != nil && st.Status == runner.StatusPassed && answeredByService(st) && serverError(st) == "" &&
		(st.Transport == nil || !serverErrorCodes[strings.ToLower(st.Transport.Code)])
}

func sameRequest(a, b *runner.StepRecord) bool {
	if a.Call != b.Call {
		return false
	}
	var x, y any
	if json.Unmarshal(a.Request, &x) != nil || json.Unmarshal(b.Request, &y) != nil {
		return bytes.Equal(bytes.TrimSpace(a.Request), bytes.TrimSpace(b.Request))
	}
	ex, _ := json.Marshal(x)
	ey, _ := json.Marshal(y)
	return bytes.Equal(ex, ey)
}

func latestRunBefore(e *env, rec *runner.Record) *runner.Record {
	ids, _ := e.store.ListRuns(rec.Chain)
	var best *runner.Record
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		if best != nil && runStamp(ids[i]) < runStamp(best.RunID) {
			break
		}
		prev, err := loadRunNamedAs(e, rec, ids[i])
		if err != nil || prev.DryRun || !ranBefore(prev, rec) {
			continue
		}
		if best == nil || ranBefore(best, prev) {
			best = prev
		}
	}
	return best
}

func runKind(rec *runner.Record) string {
	if rec.ReplayOf != "" {
		return "verify"
	}
	return "run"
}

func detectIntermittent(e *env, rec *runner.Record) *intermittentFailure {
	if rec == nil || rec.DryRun {
		return nil
	}
	var last *runner.Record
	loaded := false
	out := &intermittentFailure{}
	for _, st := range rec.Steps {
		if resentAnswered(st) {
			f := flakyStep{step: st, resent: true}
			if !loaded {
				last, loaded = latestRunBefore(e, rec), true
			}
			if was, ok := stepOf(last, st.ID); ok && was.FirstAttempt != nil && was.FirstAttempt.Text() == st.FirstAttempt.Text() {
				f.repeated = fmt.Sprintf("run %s, the previous %s of this chain, failed at the same step(s) the same way", last.RunID, runKind(last))
			}
			out.steps = append(out.steps, f)
			continue
		}
		why := serverError(st)
		if why == "" || rec.KeptRed != "" {
			continue
		}
		f := flakyStep{step: st}
		for _, o := range rec.Steps {
			if o != st && answeredAsExpected(o) && sameRequest(o, st) {
				f.sameRun = append(f.sameRun, fmt.Sprintf("step %d %s", o.Index, o.ID))
			}
		}
		if !loaded {
			last, loaded = latestRunBefore(e, rec), true
		}
		if last != nil {
			if was, ok := last.Step(st.ID); ok && serverError(was) == why {
				f.repeated = fmt.Sprintf("run %s, the previous %s of this chain, failed at the same step(s) the same way", last.RunID, runKind(last))
			}
			if was, ok := last.Step(st.ID); ok && answeredAsExpected(was) {
				for _, o := range last.Steps {
					if o.ID != st.ID && serverError(o) == why {
						f.moved = fmt.Sprintf("run %s, the previous %s of this chain, failed at step %d %s instead with the same error, "+
							"and answered step %s as expected", last.RunID, runKind(last), o.Index, o.ID, st.ID)
						break
					}
				}
			}
		}
		if prev := previousRunAttempting(e, rec, st.ID); prev != nil {
			if was, ok := prev.Step(st.ID); ok && was.Call == st.Call && answeredAsExpected(was) {
				f.answered = fmt.Sprintf("run %s, the previous run that sent step %s, had it answered as expected", prev.RunID, st.ID)
			}
		}
		switch {
		case f.sufficient():
			out.steps = append(out.steps, f)
		case f.answered != "":
			out.weak = append(out.weak, f)
		}
	}
	if len(out.steps) == 0 && len(out.weak) == 0 {
		return nil
	}
	failed := map[string]bool{}
	for _, f := range append(append([]flakyStep{}, out.steps...), out.weak...) {
		failed[f.step.ID] = !f.resent
	}
	if len(out.steps) > 0 {
		out.rate = callRate(rec, out.steps[0].step.Call)
	}
	for _, st := range rec.Steps {
		if src, _ := heldBackBy(st); failed[st.ID] || failed[src] {
			out.hidden = append(out.hidden, st.ID)
		}
	}
	return out
}

func (f flakyStep) evidence() string {
	parts := []string{}
	if len(f.sameRun) > 0 {
		parts = append(parts, "the backend answered the same request at "+strings.Join(f.sameRun, ", ")+" in this run")
	}
	if f.moved != "" {
		parts = append(parts, f.moved)
	}
	if f.answered != "" && f.moved == "" {
		parts = append(parts, f.answered)
	}
	return strings.Join(parts, "; ")
}

func (i *intermittentFailure) otherFailures(rec *runner.Record) []string {
	flaky := map[string]bool{}
	for _, f := range i.steps {
		flaky[f.step.ID] = true
	}
	out := []string{}
	for _, st := range rec.Steps {
		if (st.Status == runner.StatusFailed || st.Status == runner.StatusError) && !flaky[st.ID] {
			out = append(out, st.ID)
		}
	}
	return out
}

func (i *intermittentFailure) finding() bool { return i != nil && len(i.steps) > 0 }

func (i *intermittentFailure) calls() string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range i.steps {
		if !seen[f.step.Call] {
			seen[f.step.Call] = true
			out = append(out, shortRPC(f.step.Call))
		}
	}
	return strings.Join(out, ", ")
}

func (i *intermittentFailure) line() string {
	each, repeated := []string{}, true
	for _, f := range i.steps {
		if f.resent {
			each = append(each, fmt.Sprintf("step %d %s got %s, and its re-send was answered and judged", f.step.Index, f.step.ID, f.step.FirstAttempt.Text()))
		} else {
			each = append(each, fmt.Sprintf("step %d %s got %s, but %s", f.step.Index, f.step.ID, errorText(f.step), f.evidence()))
		}
		repeated = repeated && f.repeated != ""
	}
	hidden := ""
	if len(i.hidden) > 0 {
		hidden = "; the errors hid the checks of " + capList(i.hidden, 6)
	}
	if repeated {
		return fmt.Sprintf("repeated failure at %s: %s, so the backend fails this rpc at the same calls every run, not by chance: "+
			"a defect in the backend, and a re-run fails the same way%s; %s",
			i.calls(), i.steps[0].repeated, hidden, strings.Join(each, "; "))
	}
	how := "the backend fails this rpc on some calls and answers it on others"
	if i.rate != "" {
		how = i.rate
	}
	return fmt.Sprintf("intermittent failure at %s: %s, a backend defect (flaky under load, an exhausted pool, a race), "+
		"not a deterministic regression at that step; a re-run may pass and does not clear it%s; %s", i.calls(), how, hidden, strings.Join(each, "; "))
}

func callRate(rec *runner.Record, call string) string {
	var failedAt []int
	n := 0
	for _, st := range rec.Steps {
		if st == nil || st.Call != call {
			continue
		}
		if a := st.FirstAttempt; a != nil {
			n++
			if serverAttempt(a) {
				failedAt = append(failedAt, n)
			}
		}
		if st.HTTPStatus == 0 && st.Transport == nil {
			continue
		}
		n++
		if serverError(st) != "" {
			failedAt = append(failedAt, n)
		}
		n += len(st.LatencyResent)
	}
	if len(failedAt) < 2 {
		return ""
	}
	out := fmt.Sprintf("it failed %d of %d calls in this run", len(failedAt), n)
	gap := failedAt[1] - failedAt[0]
	for k := 2; k < len(failedAt); k++ {
		if failedAt[k]-failedAt[k-1] != gap {
			gap = 0
		}
	}
	if gap > 1 && len(failedAt) > 2 {
		out += ", every " + ordinal(gap)
	}
	return out + ", and answered the others"
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func (i *intermittentFailure) notes() []string {
	if i == nil {
		return nil
	}
	out := []string{}
	for _, f := range i.weak {
		out = append(out, fmt.Sprintf("step %d %s failed with a server error (%s) and %s: this looks intermittent, "+
			"though a backend change between the runs looks the same. Re-run: if it then passes here, or fails at another step "+
			"with the same error, it is reported as an intermittent failure; the same failure at the same step is a regression",
			f.step.Index, f.step.ID, errorText(f.step), f.answered))
	}
	return out
}

func (i *intermittentFailure) explainsAll(report *diff.Report) bool {
	if !i.finding() || report == nil {
		return false
	}
	flaky := map[string]bool{}
	for _, f := range i.steps {
		flaky[f.step.ID] = true
	}
	expectOnly := len(report.RequestChanges) > 0 && report.OnlyExpectationsEdited()
	if len(report.RequestChanges) > 0 && !expectOnly {
		return false
	}
	n := 0
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		if !flaky[c.Step] {
			if expectOnly && c.WithInput {
				continue
			}
			return false
		}
		n++
	}
	return n > 0
}
