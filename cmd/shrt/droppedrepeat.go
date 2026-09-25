package main

import (
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
}

func (u *unansweredRepeat) line() string {
	return fmt.Sprintf("%s at %s (step %d %s) in this run and in run %s, the previous run that sent it, while the backend "+
		"answered %d later step(s) in this run and %d in that one: the backend fails this rpc every time while answering "+
		"others, so it is not an outage or a restart. This is a finding about the backend at %s",
		u.how, u.step.Call, u.step.Index, u.step.ID, u.repeat, u.later, u.before, u.step.Call)
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
	if how == "" || later == 0 || answeredElsewhere(rec, st) {
		return nil
	}
	prev := previousRunAttempting(e, rec, step)
	if prev == nil {
		return nil
	}
	was, before := answeredAfter(prev, step)
	if was == nil || was.Call != st.Call || unansweredKind(was) != how || before == 0 || answeredElsewhere(prev, was) {
		return nil
	}
	return &unansweredRepeat{step: st, how: how, later: later, before: before, repeat: prev.RunID}
}

func previousRunAttempting(e *env, rec *runner.Record, step string) *runner.Record {
	ids, _ := e.store.ListRuns(rec.Chain)
	var best *runner.Record
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		if best != nil && runStamp(ids[i]) < runStamp(best.RunID) {
			break
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err != nil || prev.DryRun || !ranBefore(prev, rec) || !attempted(prev, step) {
			continue
		}
		if best == nil || ranBefore(best, prev) {
			best = prev
		}
	}
	return best
}

func attempted(rec *runner.Record, step string) bool {
	st, ok := rec.Step(step)
	return ok && st.Status != runner.StatusSkipped && (st.HTTPStatus != 0 || len(st.Response) > 0 || unansweredKind(st) != "")
}

func answeredElsewhere(rec *runner.Record, at *runner.StepRecord) bool {
	for _, st := range rec.Steps {
		if st != nil && st != at && st.Call == at.Call && answeredByService(st) {
			return true
		}
	}
	return false
}
