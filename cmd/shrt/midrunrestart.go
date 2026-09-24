package main

import (
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
}

func (s *sessionLoss) line() string {
	if s.repeat != "" {
		return fmt.Sprintf("auth refused at %s (step %d %s) with a token accepted elsewhere in this run, and run %s was refused "+
			"at the same step the same way: a restart does not land on the same call run after run, so the refusal is "+
			"specific to that rpc. This is a finding about the backend, not a restart", s.step.Call, s.step.Index, s.step.ID, s.repeat)
	}
	out := fmt.Sprintf("the backend refused a token it had accepted earlier in this run at step %d %s: it likely restarted mid-run, "+
		"losing its sessions and whatever this run had created before it; re-run", s.step.Index, s.step.ID)
	if s.visible != "" {
		out += fmt.Sprintf(". Data created before it is still there after the re-login (step %s read it back), so unless the "+
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
	ids, _ := e.store.ListRuns(rec.Chain)
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] >= rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err != nil || prev.DryRun || !sent(prev, s.step.ID) {
			continue
		}
		if again := detectSessionLoss(prev); again != nil && again.step.ID == s.step.ID && again.step.Call == s.step.Call {
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
	before := map[string]bool{}
	for i, st := range rec.Steps {
		if i < from && answeredCleanly(st) {
			before[st.ID] = true
		}
	}
	for _, st := range rec.Steps[from+1:] {
		if !answeredCleanly(st) || st.AuthRetry != "" {
			continue
		}
		for _, text := range st.BodyRefs {
			for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
				ref := chain.ParseRef(m[1])
				step := ref.Head
				if step == "steps" {
					step, _, _ = strings.Cut(ref.Rest, ".")
				}
				if before[step] {
					return st.ID
				}
			}
		}
	}
	return ""
}

func answeredCleanly(st *runner.StepRecord) bool {
	return st != nil && st.Status != runner.StatusSkipped && st.Transport == nil && len(st.Response) > 0 && stepRefusalText(st) == ""
}
