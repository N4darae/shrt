package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/runner"
)

type sessionLoss struct {
	step    *runner.StepRecord
	index   int
	repeat  string
	visible string
	restart string
	passed  bool
}

func (s *sessionLoss) line() string {
	if s.repeat != "" {
		return fmt.Sprintf("auth refused at %s (step %d %s) with a token accepted elsewhere in this run, and run %s was refused "+
			"at the same step the same way: a restart does not land on the same call run after run, so the refusal is "+
			"specific to that rpc. This is a finding about the backend, not a restart", s.step.Call, s.step.Index, s.step.ID, s.repeat)
	}
	if s.passed {
		return fmt.Sprintf("a read was re-sent after a refused token at step %d %s: the backend refused a token it had accepted "+
			"earlier in this run, and after a fresh login the re-sent call was accepted", s.step.Index, s.step.ID)
	}
	out := fmt.Sprintf("the backend refused a token it had accepted earlier in this run at step %d %s: it likely restarted mid-run, "+
		"losing its sessions and whatever this run had created before it; re-run", s.step.Index, s.step.ID)
	if s.restart != "" {
		return out + fmt.Sprintf(". %s, which a restart explains, so this is not a finding even when the previous run was "+
			"refused at the same step", s.restart)
	}
	if s.visible != "" {
		out += fmt.Sprintf(". Data created before it is still there after the re-login (step %s answered with a value created before it), so unless the "+
			"backend keeps its data across a restart, the refusal is specific to %s: a re-run refused at the same step is "+
			"reported as a finding", s.visible, s.step.Call)
	}
	return out
}

func (s *sessionLoss) finding() bool {
	return s != nil && s.repeat != ""
}

func detectSessionLoss(rec *runner.Record) *sessionLoss {
	if rec == nil || rec.DryRun {
		return nil
	}
	accepted := map[string]bool{}
	for i, st := range rec.Steps {
		profile := st.AuthProfile
		if profile == "" || profile == runner.NoAuthProfile {
			continue
		}
		if st.AuthRetry != "" && runner.RefusedFreshToken(st) {
			return nil
		}
		if st.AuthRetry != "" && accepted[profile] && refusedEarly(st) {
			return nil
		}
		if st.AuthRetry != "" && accepted[profile] {
			return &sessionLoss{step: st, index: i, passed: rec.Passed() && resentAccepted(st)}
		}
		if st.AuthRetry == "" && st.Status != runner.StatusSkipped && !runner.NotAnsweredByService(st) && (st.HTTPStatus != 0 || len(st.Response) > 0) {
			accepted[profile] = true
		}
	}
	return nil
}

func examineSessionLoss(e *env, rec *runner.Record) *sessionLoss {
	s := detectSessionLoss(rec)
	if s == nil {
		return nil
	}
	s.visible = readBackAfter(rec, s.index)
	s.restart = restartEvidence(rec, s.index)
	if s.restart != "" {
		return s
	}
	if prev := previousRunSending(e, rec, s.step.ID); prev != nil {
		if again := detectSessionLoss(prev); again != nil && again.step.ID == s.step.ID && again.step.Call == s.step.Call &&
			refusalKind(again.step) == refusalKind(s.step) && restartEvidence(prev, again.index) == "" {
			s.repeat = prev.RunID
		}
	}
	return s
}

func previousRunSending(e *env, rec *runner.Record, step string) *runner.Record {
	return previousRun(e, rec, true, func(prev *runner.Record) bool { return sent(prev, step) })
}

func ranBefore(a, b *runner.Record) bool {
	if !a.StartedAt.Equal(b.StartedAt) {
		return a.StartedAt.Before(b.StartedAt)
	}
	return a.RunID < b.RunID
}

func runStamp(id string) string {
	stamp, _, _ := strings.Cut(id, "-")
	return stamp
}

func sent(rec *runner.Record, step string) bool {
	for _, st := range rec.Steps {
		if st != nil && st.ID == step {
			return st.Status != runner.StatusSkipped && (st.HTTPStatus != 0 || len(st.Response) > 0)
		}
	}
	return false
}

func readBackAfter(rec *runner.Record, from int) string {
	scan := newPriorScan(rec, from)
	for _, st := range rec.Steps[from+1:] {
		if !answeredCleanly(st) || st.AuthRetry != "" {
			continue
		}
		for _, value := range scan.createdValues(st) {
			quoted, _ := json.Marshal(value)
			if strings.Contains(string(st.Response), string(quoted)) {
				return st.ID
			}
		}
	}
	return ""
}

func answeredCleanly(st *runner.StepRecord) bool {
	return st != nil && st.Status != runner.StatusSkipped && st.Transport == nil && len(st.Response) > 0 && stepRefusalText(st) == ""
}

func restartEvidence(rec *runner.Record, index int) string {
	for _, st := range rec.Steps[:index] {
		if unansweredCall(st) {
			return fmt.Sprintf("Step %s before it got no answer from the service", st.ID)
		}
	}
	for _, st := range rec.Steps {
		if resentAccepted(st) {
			return fmt.Sprintf("Step %s was refused at authentication and, re-sent after a fresh login in this run, accepted", st.ID)
		}
	}
	refused := rec.Steps[index]
	scan := newPriorScan(rec, index)
	for _, st := range rec.Steps[index+1:] {
		if st == nil || st.Status == runner.StatusSkipped || st.AuthRetry != "" || st.HTTPStatus == 0 && len(st.Response) == 0 || unansweredCall(st) {
			continue
		}
		if st.Call == refused.Call && st.AuthProfile == refused.AuthProfile && answeredCleanly(st) {
			return fmt.Sprintf("Step %s, the same rpc, was accepted after the fresh login, so the refusal did not persist", st.ID)
		}
		if prior := scan.shrunkList(st); prior != "" {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s lists fewer items than step %s did before the refusal)", st.ID, prior)
		}
		if scan.conflictVanished(st) {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s expected a refusal over a value created before it and was accepted)", st.ID)
		}
		why := stepRefusalText(st)
		if why == "" || st.Status == runner.StatusPassed || st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated") {
			continue
		}
		for _, value := range scan.createdValues(st) {
			if strings.Contains(why, value) || notFound(why) {
				return fmt.Sprintf("Data created before it was gone after the re-login (step %s: %s)", st.ID, why)
			}
		}
	}
	return ""
}

func resentAccepted(st *runner.StepRecord) bool {
	return st != nil && st.AuthRetry == runner.AuthRetryResent && st.Status != runner.StatusSkipped && !runner.RefusedFreshToken(st) &&
		len(st.Response) > 0 && !(st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated")) &&
		!runner.NotAnsweredByService(st)
}

func refusalKind(st *runner.StepRecord) string {
	code := ""
	if st.Transport != nil {
		code = strings.ToLower(st.Transport.Code)
	}
	return fmt.Sprintf("%d %s", st.HTTPStatus, code)
}

func unansweredCall(st *runner.StepRecord) bool {
	if st == nil {
		return false
	}
	return runner.NotAnsweredByService(st) ||
		st.Status == runner.StatusError && st.Request != nil && st.HTTPStatus == 0 && len(st.Response) == 0 && !st.Drift
}

func notFound(text string) bool {
	folded := strings.NewReplacer("_", "", " ", "", "-", "").Replace(strings.ToLower(text))
	return strings.Contains(folded, "notfound")
}

type freshRefusal struct {
	step   *runner.StepRecord
	index  int
	repeat string
}

func (f *freshRefusal) line() string {
	return fmt.Sprintf("auth refused at %s (step %d %s) with a token a login in this run had just issued, and run %s was "+
		"refused at the same step the same way with the token its own login had just issued: the credentials work and "+
		"the refusal repeats at that rpc, so it refuses valid tokens there. This is a finding about the backend, not a "+
		"credentials problem or a restart", f.step.Call, f.step.Index, f.step.ID, f.repeat)
}

func repeatedFreshRefusal(e *env, rec *runner.Record) *freshRefusal {
	if rec == nil || rec.DryRun {
		return nil
	}
	f := &freshRefusal{index: -1}
	for i, st := range rec.Steps {
		if runner.RefusedFreshToken(st) {
			f.step, f.index = st, i
			break
		}
	}
	if f.step == nil {
		return nil
	}
	prev := previousRunSending(e, rec, f.step.ID)
	if prev == nil {
		return nil
	}
	if f.step.AuthRetry != runner.AuthRetryResent {
		return nil
	}
	if st, ok := prev.Step(f.step.ID); ok && st.Call == f.step.Call && runner.RefusedFreshToken(st) && st.AuthRetry == runner.AuthRetryResent &&
		refusalKind(st) == refusalKind(f.step) {
		f.repeat = prev.RunID
		return f
	}
	return nil
}
