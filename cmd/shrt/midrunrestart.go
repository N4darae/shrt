package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

type sessionLoss struct {
	step    *runner.StepRecord
	index   int
	repeat  string
	visible string
	restart string
}

func (s *sessionLoss) line() string {
	if s.repeat != "" {
		return fmt.Sprintf("auth refused at %s (step %d %s) with a token accepted elsewhere in this run, and run %s was refused "+
			"at the same step the same way: a restart does not land on the same call run after run, so the refusal is "+
			"specific to that rpc. This is a finding about the backend, not a restart", s.step.Call, s.step.Index, s.step.ID, s.repeat)
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
		if st.AuthRetry != "" && accepted[profile] {
			return &sessionLoss{step: st, index: i}
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
	ids, _ := e.store.ListRuns(rec.Chain)
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] >= rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err != nil || prev.DryRun || !sent(prev, s.step.ID) {
			continue
		}
		if again := detectSessionLoss(prev); again != nil && again.step.ID == s.step.ID && again.step.Call == s.step.Call &&
			restartEvidence(prev, again.index) == "" {
			s.repeat = prev.RunID
		}
		break
	}
	return s
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
	for _, st := range rec.Steps[from+1:] {
		if !answeredCleanly(st) || st.AuthRetry != "" {
			continue
		}
		for _, value := range createdValues(rec, from, st) {
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
	for _, st := range rec.Steps[index+1:] {
		if st == nil || st.Status == runner.StatusSkipped || st.AuthRetry != "" || st.HTTPStatus == 0 && len(st.Response) == 0 || unansweredCall(st) {
			continue
		}
		why := stepRefusalText(st)
		if why == "" || st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated") {
			continue
		}
		for _, value := range createdValues(rec, index, st) {
			if strings.Contains(why, value) || notFound(why) {
				return fmt.Sprintf("Data created before it was gone after the re-login (step %s: %s)", st.ID, why)
			}
		}
	}
	return ""
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

func createdValues(rec *runner.Record, from int, st *runner.StepRecord) []string {
	before := map[string]any{}
	for i, prior := range rec.Steps {
		if i >= from {
			break
		}
		if !answeredCleanly(prior) {
			continue
		}
		var body any
		if json.Unmarshal(prior.Response, &body) == nil {
			before[prior.ID] = body
		}
	}
	var out []string
	for _, text := range st.BodyRefs {
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			ref := chain.ParseRef(m[1])
			body, ok := before[ref.Head]
			if !ok || ref.Kind != chain.RefStep || strings.HasPrefix(ref.Rest, "request.") {
				continue
			}
			v, ok := chain.Get(body, strings.TrimPrefix(ref.Rest, "response."))
			if s, isText := v.(string); ok && isText && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
