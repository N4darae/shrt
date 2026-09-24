package main

import (
	"fmt"

	"github.com/N4darae/shrt/runner"
)

type sessionLoss struct {
	step  *runner.StepRecord
	index int
}

func (s *sessionLoss) line() string {
	return fmt.Sprintf("the backend refused a token it had accepted earlier in this run at step %d %s: it likely restarted mid-run, "+
		"losing its sessions and whatever this run had created before it; re-run", s.step.Index, s.step.ID)
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
