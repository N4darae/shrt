package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

type withoutVerify struct {
	vars varFlags
	sent bool
}

type withoutVerdict struct {
	Without   []string `json:"without"`
	SourceRun string   `json:"source_run"`
	Run       string   `json:"run,omitempty"`
	Cleared   []string `json:"no_longer_fail"`
	StillFail []string `json:"still_fail"`
	NewFail   []string `json:"fail_only_without,omitempty"`
	newer     string
}

func verifyWithout(ctx context.Context, e *env, res *chain.WithoutResult, rec *runner.Record, named []string, a *withoutVerify, persist, quiet bool) (*withoutVerdict, error) {
	if !quiet {
		fmt.Println()
	}
	run, err := executeChain(ctx, e, res.Chain, runner.Options{
		Vars: a.vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, KeepGoing: true,
	}, quiet, false)
	if err != nil {
		return nil, exitWith(3, "DID NOT RUN: could not run %s without %s: %v", res.Source, capList(named, 3), err)
	}
	a.sent = true
	v := &withoutVerdict{Without: named, SourceRun: rec.RunID, Cleared: []string{}, StillFail: []string{}}
	if other := newerFailing(e, rec); other != nil {
		v.newer = fmt.Sprintf("; newer record %s: %s, pass -run %s", other.RunID, failedCount(other), other.RunID)
	}
	if persist {
		if _, err := e.store.SaveRun(run); err == nil {
			v.Run = run.RunID
		}
	}
	for _, st := range res.Chain.Steps {
		src, reached := rec.Step(st.ID)
		now, ran := run.Step(st.ID)
		switch {
		case !reached || !failing(src):
			if ran && failing(now) {
				v.NewFail = append(v.NewFail, st.ID)
			}
		case ran && now.Status == runner.StatusPassed:
			v.Cleared = append(v.Cleared, st.ID)
		default:
			v.StillFail = append(v.StillFail, st.ID)
		}
	}
	return v, nil
}

func failing(sr *runner.StepRecord) bool {
	return sr.Status == runner.StatusFailed || sr.Status == runner.StatusError
}

func (v *withoutVerdict) text() string {
	var b strings.Builder
	without := capList(v.Without, 3)
	counted := len(v.Cleared) + len(v.StillFail)
	switch {
	case counted == 0:
		fmt.Fprintf(&b, "verify without %s: no step left in failed in source run %s%s\n", without, v.SourceRun, v.newer)
	case len(v.Cleared) == 0:
		fmt.Fprintf(&b, "verify NOT REPRODUCED without %s: the %d step(s) that failed in source run %s still fail: %s\n",
			without, counted, v.SourceRun, capList(v.StillFail, 5))
	default:
		fmt.Fprintf(&b, "verify without %s: %d of %d step(s) that failed in source run %s pass without it: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, capList(v.Cleared, 5))
		if len(v.StillFail) > 0 {
			fmt.Fprintf(&b, "  still fail, so another cause: %s\n", capList(v.StillFail, 5))
		}
	}
	if len(v.NewFail) > 0 {
		fmt.Fprintf(&b, "  fail only without it: %s, they need what the left-out steps did\n", capList(v.NewFail, 5))
	}
	return b.String()
}

func (v *withoutVerdict) err() error {
	if v == nil {
		return nil
	}
	without := capList(v.Without, 3)
	switch {
	case len(v.StillFail) > 0 && len(v.Cleared) == 0:
		return exitWith(1, "NOT REPRODUCED without %s", without)
	case len(v.StillFail) > 0:
		return exitWith(1, "without %s %d failing step(s) still fail", without, len(v.StillFail))
	case len(v.Cleared) == 0 && len(v.NewFail) > 0:
		return exitWith(1, "without %s %d step(s) fail that passed in source run %s", without, len(v.NewFail), v.SourceRun)
	}
	return nil
}
