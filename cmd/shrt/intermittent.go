package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

var serverErrorCodes = map[string]bool{
	"internal": true, "unknown": true, "resource_exhausted": true, "data_loss": true, "aborted": true, "deadline_exceeded": true,
}

type flakyStep struct {
	step      *runner.StepRecord
	sameRun   []string
	moved     string
	answered  string
	repeated  string
	alongside []string
	resent    bool
}

func (f flakyStep) sufficient() bool { return len(f.sameRun) > 0 || f.moved != "" || f.resent }

type intermittentFailure struct {
	rec    *runner.Record
	steps  []flakyStep
	weak   []flakyStep
	hidden []string
	rate   string
}

func serverError(st *runner.StepRecord) string {
	if st == nil || st.Transport == nil || !failing(st) {
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
	lastRun := sync.OnceValue(func() *runner.Record { return previousRun(e, rec, true, nil) })
	out := &intermittentFailure{rec: rec}
	for _, st := range rec.Steps {
		if resentAnswered(st) {
			f, last := flakyStep{step: st, resent: true}, lastRun()
			if last != nil {
				if was, ok := last.Step(st.ID); ok && was.FirstAttempt != nil && was.FirstAttempt.Text() == st.FirstAttempt.Text() {
					f.repeated = fmt.Sprintf("run %s, the previous %s of this chain, failed at the same step(s) the same way", last.RunID, runKind(last))
				}
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
		if last := lastRun(); last != nil {
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
			for _, o := range rec.Steps {
				if o != nil && o != st && o.Call == st.Call && failing(o) && serverError(o) == "" {
					f.alongside = append(f.alongside, o.ID)
				}
			}
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

func (i *intermittentFailure) otherFailures(e *env, rec *runner.Record) []string {
	flaky := map[string]bool{}
	for _, f := range i.steps {
		flaky[f.step.ID] = true
	}
	a, out := runAttribution(e, rec), []string{}
	for _, st := range rec.Steps {
		if failing(st) && !flaky[st.ID] && !flaky[a.of(st.ID, failedPath(st)).blamed(st.ID)] {
			out = append(out, st.ID)
		}
	}
	return out
}

func failedPath(st *runner.StepRecord) string {
	for _, ex := range st.Expect {
		if !ex.Passed && ex.Rule != "unevaluated" {
			return ex.Path
		}
	}
	return ""
}

func (i *intermittentFailure) classOf(report *diff.Report, a attribution) func(diff.Change) string {
	flaky := map[string]bool{}
	if i.finding() {
		for _, f := range i.steps {
			flaky[f.step.ID] = true
		}
	}
	return func(c diff.Change) string {
		if len(flaky) == 0 {
			return report.Class(c)
		}
		if w := a.of(c.Step, c.Path).blamed(c.Step); flaky[c.Step] || w != "" && flaky[w] {
			return i.kind()
		}
		return report.Class(c)
	}
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

func (i *intermittentFailure) repeated() bool {
	for _, f := range i.steps {
		if f.repeated == "" {
			return false
		}
	}
	return len(i.steps) > 0
}

func (i *intermittentFailure) kind() string {
	if i.repeated() {
		return "repeated"
	}
	return "intermittent"
}

func (i *intermittentFailure) short() string {
	parts := []string{}
	for _, r := range i.rates() {
		parts = append(parts, shortRPC(r.Call)+" ("+r.text()+")")
	}
	return i.kind() + " failure at " + strings.Join(parts, ", ")
}

func (i *intermittentFailure) rates() []gateFlaky {
	var out []gateFlaky
	seen := map[string]bool{}
	for _, f := range i.steps {
		if seen[f.step.Call] {
			continue
		}
		seen[f.step.Call] = true
		r := callCounts(i.rec, f.step.Call)
		r.Repeated = i.repeated()
		for _, g := range i.steps {
			if g.step.Call == f.step.Call && !g.resent {
				r.Steps = append(r.Steps, g.step.ID)
			}
		}
		out = append(out, r)
	}
	return out
}

func findingMeaning(repeated bool, where string) string {
	if repeated {
		return "the backend fails this rpc at the same calls every run, not by chance: a defect in the backend, and a re-run fails the same way"
	}
	return "a backend defect (flaky under load, an exhausted pool, a race), not a deterministic regression at " + where +
		"; a re-run may pass and does not clear it"
}

func (i *intermittentFailure) line(explain bool) string {
	var resent grouped[string]
	each := []string{}
	for _, f := range i.steps {
		at := fmt.Sprintf("step %d %s", f.step.Index, f.step.ID)
		if !f.resent {
			each = append(each, fmt.Sprintf("%s got %s, but %s", at, errorText(f.step), f.evidence()))
			continue
		}
		resent.add(f.step.FirstAttempt.Text(), at)
	}
	for _, why := range resent.keys {
		each = append(each, fmt.Sprintf("%s got %s, each re-send answered and judged", chain.ListSome(resent.of[why], 3), why))
	}
	each = each[:min(len(each), 4)]
	hidden := ""
	if len(i.hidden) > 0 {
		hidden = "; the errors hid the checks of " + chain.ListSome(i.hidden, 6)
	}
	if i.repeated() {
		why := ""
		if explain {
			why = ", so " + findingMeaning(true, "")
		}
		return fmt.Sprintf("repeated failure at %s: %s%s%s; %s", i.calls(), i.steps[0].repeated, why, hidden, strings.Join(each, "; "))
	}
	how := "the backend fails this rpc on some calls and answers it on others"
	if i.rate != "" {
		how = i.rate
	}
	if explain {
		how += ", " + findingMeaning(false, "that step")
	}
	return fmt.Sprintf("intermittent failure at %s: %s%s; %s", i.calls(), how, hidden, strings.Join(each, "; "))
}

func serverErrors(rec *runner.Record) []gateFlaky {
	if rec == nil {
		return nil
	}
	answered := map[string]bool{}
	for _, st := range rec.Steps {
		if st != nil && st.HTTPStatus != 0 && !runner.NotAnsweredByService(st) {
			answered[st.Call] = true
		}
	}
	var steps grouped[string]
	for _, st := range rec.Steps {
		if serverError(st) != "" || runner.NotAnsweredByService(st) && answered[st.Call] {
			steps.add(st.Call, st.ID)
		}
	}
	var out []gateFlaky
	for _, call := range steps.keys {
		r := callCounts(rec, call)
		r.Steps, r.Failed = steps.of[call], max(r.Failed, len(steps.of[call]))
		out = append(out, r)
	}
	return out
}

func callCounts(rec *runner.Record, call string) gateFlaky {
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
	out := gateFlaky{Call: call, Failed: len(failedAt), Calls: n}
	if len(failedAt) > 2 {
		gap := failedAt[1] - failedAt[0]
		for k := 2; k < len(failedAt); k++ {
			if failedAt[k]-failedAt[k-1] != gap {
				gap = 0
			}
		}
		if gap > 1 {
			out.Every = gap
		}
	}
	return out
}

func (r gateFlaky) text() string {
	out := fmt.Sprintf("failed %d of %d calls", r.Failed, r.Calls)
	if r.Every > 1 {
		out += ", every " + ordinal(r.Every)
	}
	return out
}

func callRate(rec *runner.Record, call string) string {
	r := callCounts(rec, call)
	if r.Failed < 2 {
		return ""
	}
	out := fmt.Sprintf("it failed %d of %d calls in this run", r.Failed, r.Calls)
	if r.Every > 1 {
		out += ", every " + ordinal(r.Every)
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
		if len(f.alongside) > 0 {
			out = append(out, fmt.Sprintf("step %d %s failed with a server error (%s) and %s, and %s also failed in this run "+
				"at %s, without a server error: a backend change at that rpc, not an intermittent failure",
				f.step.Index, f.step.ID, errorText(f.step), f.answered, shortRPC(f.step.Call), chain.ListSome(f.alongside, 3)))
			continue
		}
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
