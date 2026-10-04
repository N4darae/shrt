package main

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

type unansweredRepeat struct {
	step   *runner.StepRecord
	how    string
	later  int
	before int
	repeat string
	others []string
}

func (u *unansweredRepeat) line() string {
	if len(u.others) == 0 {
		return fmt.Sprintf("%s at %s (step %d %s) in this run and in run %s, the previous run that sent it, while the backend "+
			"answered %d later step(s) in this run and %d in that one: the backend fails this rpc every time while answering "+
			"others, so it is not an outage or a restart. This is a finding about the backend at %s",
			u.how, u.step.Call, u.step.Index, u.step.ID, u.repeat, u.later, u.before, u.step.Call)
	}
	return fmt.Sprintf("%s at step %d %s (%s, request %s) in this run and in run %s, the previous run that sent it, while the "+
		"backend answered %d later step(s) in this run and %d in that one, and answered %s at %s: the backend fails this step's "+
		"request every time while answering other calls, so it is not an outage or a restart. This is a finding about the "+
		"backend at step %s (%s with this request)",
		u.how, u.step.Index, u.step.ID, u.step.Call, cmp.Or(capText(strings.TrimSpace(string(u.step.Request)), 160), "{}"), u.repeat, u.later, u.before, u.step.Call,
		strings.Join(u.others, ", "), u.step.ID, u.step.Call)
}

func answeredIn(rec *runner.Record, at *runner.StepRecord) (string, bool) {
	if rec == nil || at == nil {
		return "", false
	}
	st, ok := rec.Step(at.ID)
	if !ok || st.Call != at.Call || !answeredByService(st) {
		return "", false
	}
	if st.HTTPStatus != 0 {
		return fmt.Sprintf("HTTP %d", st.HTTPStatus), true
	}
	return "answered", true
}

func answeredCallsElsewhere(rec *runner.Record, at *runner.StepRecord) []string {
	out := []string{}
	for _, st := range rec.Steps {
		if st != nil && st != at && st.Call == at.Call && answeredByService(st) {
			out = append(out, st.ID)
		}
	}
	return out
}

func unansweredKind(st *runner.StepRecord) string {
	switch {
	case st == nil || st.Status != runner.StatusError || st.HTTPStatus != 0 || len(st.Response) > 0:
		return ""
	case strings.Contains(st.Error, transport.NoAnswerBeforeTimeout):
		return "sent and got no answer before target.timeout"
	case strings.Contains(st.Error, "closed the connection"):
		return "the backend closed the connection before answering"
	}
	return ""
}

func answeredAfter(rec *runner.Record, id string) (*runner.StepRecord, int) {
	var at *runner.StepRecord
	n := 0
	for _, st := range rec.Steps {
		if at != nil && answeredByService(st) {
			n++
		}
		if at == nil && st != nil && st.ID == id {
			at = st
		}
	}
	return at, n
}

func repeatedUnanswered(e *env, rec *runner.Record, step string) *unansweredRepeat {
	if rec == nil || rec.DryRun {
		return nil
	}
	st, later := answeredAfter(rec, step)
	how := unansweredKind(st)
	if how == "" || later == 0 {
		return nil
	}
	prev := previousRunAttempting(e, rec, step)
	if prev == nil {
		return nil
	}
	was, before := answeredAfter(prev, step)
	if was == nil || was.Call != st.Call || unansweredKind(was) != how || before == 0 {
		return nil
	}
	return &unansweredRepeat{step: st, how: how, later: later, before: before, repeat: prev.RunID, others: answeredCallsElsewhere(rec, st)}
}

func previousRunAttempting(e *env, rec *runner.Record, step string) *runner.Record {
	return previousRun(e, rec, true, func(prev *runner.Record) bool { return attempted(prev, step) })
}

func attempted(rec *runner.Record, step string) bool {
	st, ok := rec.Step(step)
	return ok && st.Status != runner.StatusSkipped && (st.HTTPStatus != 0 || len(st.Response) > 0 || unansweredKind(st) != "")
}
