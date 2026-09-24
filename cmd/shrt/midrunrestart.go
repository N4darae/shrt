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
	if prev := previousRunSending(e, rec, s.step.ID); prev != nil {
		if again := detectSessionLoss(prev); again != nil && again.step.ID == s.step.ID && again.step.Call == s.step.Call &&
			restartEvidence(prev, again.index) == "" {
			s.repeat = prev.RunID
		}
	}
	return s
}

func previousRunSending(e *env, rec *runner.Record, step string) *runner.Record {
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
		if err != nil || prev.DryRun || !ranBefore(prev, rec) || !sent(prev, step) {
			continue
		}
		if best == nil || ranBefore(best, prev) {
			best = prev
		}
	}
	return best
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
	for _, st := range rec.Steps {
		if resentAccepted(st) {
			return fmt.Sprintf("Step %s was refused at authentication and, re-sent after a fresh login in this run, accepted", st.ID)
		}
	}
	refused := rec.Steps[index]
	for _, st := range rec.Steps[index+1:] {
		if st == nil || st.Status == runner.StatusSkipped || st.AuthRetry != "" || st.HTTPStatus == 0 && len(st.Response) == 0 || unansweredCall(st) {
			continue
		}
		if st.Call == refused.Call && st.AuthProfile == refused.AuthProfile && answeredCleanly(st) {
			return fmt.Sprintf("Step %s, the same rpc, was accepted after the fresh login, so the refusal did not persist", st.ID)
		}
		if prior := shrunkList(rec, index, st); prior != "" {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s lists fewer items than step %s did before the refusal)", st.ID, prior)
		}
		if conflictVanished(rec, index, st) {
			return fmt.Sprintf("Data created before it was gone after the re-login (step %s expected a refusal over a value created before it and was accepted)", st.ID)
		}
		why := stepRefusalText(st)
		if why == "" || st.Status == runner.StatusPassed || st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated") {
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

func resentAccepted(st *runner.StepRecord) bool {
	return st != nil && st.AuthRetry == runner.AuthRetryResent && st.Status != runner.StatusSkipped && !runner.RefusedFreshToken(st) &&
		len(st.Response) > 0 && !(st.Transport != nil && strings.EqualFold(st.Transport.Code, "unauthenticated")) &&
		!runner.NotAnsweredByService(st)
}

func shrunkList(rec *runner.Record, index int, st *runner.StepRecord) string {
	if !answeredCleanly(st) {
		return ""
	}
	var after any
	if json.Unmarshal(st.Response, &after) != nil {
		return ""
	}
	for _, prior := range rec.Steps[:index] {
		if !answeredCleanly(prior) || prior.Call != st.Call || !jsonEqual(prior.Request, st.Request) {
			continue
		}
		var before any
		if json.Unmarshal(prior.Response, &before) == nil && listShrank(before, after) {
			return prior.ID
		}
	}
	return ""
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

func listShrank(before, after any) bool {
	switch b := before.(type) {
	case map[string]any:
		a, ok := after.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range b {
			if list, isList := v.([]any); isList && len(list) > 0 {
				other, _ := a[k].([]any)
				if len(other) < len(list) {
					return true
				}
				continue
			}
			if listShrank(v, a[k]) {
				return true
			}
		}
	}
	return false
}

func conflictVanished(rec *runner.Record, index int, st *runner.StepRecord) bool {
	if !answeredCleanly(st) || st.Status != runner.StatusFailed || !readsBefore(rec, index, st) {
		return false
	}
	for _, e := range st.Expect {
		if !e.Passed && e.Path == chain.EnvelopePath() && e.Want != nil && fmt.Sprint(e.Want) != chain.EnvelopeOK() {
			return true
		}
	}
	return false
}

func readsBefore(rec *runner.Record, index int, st *runner.StepRecord) bool {
	for _, text := range st.BodyRefs {
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			ref := chain.ParseRef(m[1])
			if ref.Kind != chain.RefStep {
				continue
			}
			for _, prior := range rec.Steps[:index] {
				if prior != nil && prior.ID == ref.Head && answeredCleanly(prior) {
					return true
				}
			}
		}
	}
	return false
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
	if st, ok := prev.Step(f.step.ID); ok && st.Call == f.step.Call && runner.RefusedFreshToken(st) {
		f.repeat = prev.RunID
		return f
	}
	return nil
}
