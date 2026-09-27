package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

type withoutVerify struct {
	vars   varFlags
	build  string
	sent   bool
	source *chain.Chain
}

type withoutVerdict struct {
	Without     []string `json:"without"`
	SourceRun   string   `json:"source_run"`
	Run         string   `json:"run,omitempty"`
	Cleared     []string `json:"no_longer_fail"`
	StillFail   []string `json:"still_fail"`
	NotCounted  []string `json:"not_counted,omitempty"`
	NotRun      []string `json:"not_run,omitempty"`
	NewFail     []string `json:"fail_only_without,omitempty"`
	Needed      []string `json:"fail_reading_left_out,omitempty"`
	OtherTarget string   `json:"other_target,omitempty"`
	LikelyFault string   `json:"likely_fault,omitempty"`
	writers     []string
	asBefore    string
}

func verifyWithout(ctx context.Context, e *env, res *chain.WithoutResult, rec *runner.Record, named []string, a *withoutVerify, persist, quiet bool) (*withoutVerdict, error) {
	if !quiet {
		fmt.Println()
	}
	run, err := executeChain(ctx, e, res.Chain, runner.Options{
		Vars: a.vars, Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: a.build, KeepGoing: true,
	}, quiet, false)
	if err != nil {
		return nil, fmt.Errorf("DID NOT RUN: could not run %s without %s: %w", res.Source, strings.Join(named, ", "), err)
	}
	a.sent = true
	v := &withoutVerdict{Without: named, SourceRun: rec.RunID, Cleared: []string{}, StillFail: []string{}, OtherTarget: sourceTargetDiffers(e, rec)}
	if persist {
		if _, err := e.store.SaveRun(run); err == nil {
			v.Run = run.RunID
		}
	}
	refused := refusedIn(rec)
	gone := map[string]bool{}
	for _, r := range res.Removed {
		gone[r.ID] = true
		sr, ok := rec.Step(r.ID)
		if !ok || chain.IsReadOnlyCall(sr.Call) || (sr.HTTPStatus == 0 && len(sr.Response) == 0) {
			continue
		}
		if _, wroteNothing := refused(r.ID); !wroteNothing {
			v.writers = append(v.writers, r.ID)
		}
	}
	for _, st := range res.Chain.Steps {
		src, reached := rec.Step(st.ID)
		now, ran := run.Step(st.ID)
		failedNow := ran && (now.Status == runner.StatusFailed || now.Status == runner.StatusError)
		if !reached || !failing(src) {
			switch {
			case failedNow && v.touchesLeftOut(rec, run, st.ID):
				v.Needed = append(v.Needed, st.ID)
			case failedNow:
				v.NewFail = append(v.NewFail, st.ID)
			}
			continue
		}
		switch {
		case !ran || now.Status == runner.StatusSkipped:
			v.NotRun = append(v.NotRun, st.ID)
		case now.Status == runner.StatusPassed:
			v.Cleared = append(v.Cleared, st.ID)
		case v.readsLeftOutWrite(rec, st.ID) && gotMoved(src, now):
			v.NotCounted = append(v.NotCounted, st.ID)
		default:
			v.StillFail = append(v.StillFail, st.ID)
		}
	}
	if len(v.Cleared) > 0 {
		if v.asBefore = leftOutAsBefore(e, res, rec, a.source); v.asBefore != "" {
			v.LikelyFault = v.Cleared[0]
		}
	}
	return v, nil
}

func leftOutAsBefore(e *env, res *chain.WithoutResult, rec *runner.Record, source *chain.Chain) string {
	ref := sliceReference(e, rec)
	var rep *diff.RunReport
	if ref != nil {
		rep = diff.CompareRunsSkipping(ref, rec, currentVolatile(e, ref.Chain), requestFixtures(source))
		rep.DropUnsentDefaults(ref, rec, unsentDefault(e))
	}
	for _, r := range res.Removed {
		sr, ok := rec.Step(r.ID)
		if !ok {
			continue
		}
		if failing(sr) {
			return ""
		}
		if rep == nil {
			continue
		}
		if _, inRef := ref.Step(r.ID); !inRef {
			continue
		}
		for _, ch := range rep.Changes {
			if ch.Step == r.ID {
				return ""
			}
		}
		for _, st := range append(rep.StatusChanges, rep.ErrorChanges...) {
			if st.Step == r.ID {
				return ""
			}
		}
	}
	if rep != nil {
		return "answered as in run " + ref.RunID
	}
	return "passed in source run " + rec.RunID
}

func failing(sr *runner.StepRecord) bool {
	return sr.Status == runner.StatusFailed || sr.Status == runner.StatusError
}

func (v *withoutVerdict) readsLeftOutWrite(rec *runner.Record, id string) bool {
	mentions := entityFactsOf(rec, id).mentions
	for _, w := range v.writers {
		for ent := range entityFactsOf(rec, w).acts {
			if mentions[ent] {
				return true
			}
		}
	}
	return false
}

func (v *withoutVerdict) touchesLeftOut(rec, run *runner.Record, id string) bool {
	facts := entityFactsOf(rec, id)
	if _, ok := rec.Step(id); !ok {
		facts = entityFactsOf(run, id)
	}
	for _, w := range v.writers {
		for ent := range entityFactsWith(rec, w, true).acts {
			if facts.mentions[ent] {
				return true
			}
		}
	}
	return false
}

func gotMoved(source, now *runner.StepRecord) bool {
	for _, n := range now.Expect {
		if n.Passed {
			continue
		}
		for _, s := range source.Expect {
			if s.Path == n.Path && s.Rule == n.Rule {
				if fmt.Sprint(s.Got) != fmt.Sprint(n.Got) {
					return true
				}
				break
			}
		}
	}
	return false
}

func (v *withoutVerdict) text() string {
	var b strings.Builder
	without := strings.Join(v.Without, ", ")
	in := "the run without it"
	if v.Run != "" {
		in = "run " + v.Run
	}
	counted := len(v.Cleared) + len(v.StillFail)
	switch {
	case counted == 0:
		fmt.Fprintf(&b, "verify without %s: no step left in failed in source run %s to compare\n", without, v.SourceRun)
	case len(v.Cleared) == 0:
		fmt.Fprintf(&b, "verify NOT REPRODUCED without %s: the %d step(s) that failed in source run %s still fail in %s: %s\n",
			without, counted, v.SourceRun, in, capList(v.StillFail, 5))
	case v.LikelyFault != "":
		fmt.Fprintf(&b, "verify without %s: %d of %d step(s) that failed in source run %s need it, they pass in %s: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, in, capList(v.Cleared, 5))
		fmt.Fprintf(&b, "  %s %s, so it is a precondition, not the fault: look first at %s\n", without, v.asBefore, v.LikelyFault)
		if len(v.StillFail) > 0 {
			fmt.Fprintf(&b, "  still fail, so another cause: %s\n", capList(v.StillFail, 5))
		}
	default:
		fmt.Fprintf(&b, "verify without %s: cause confirmed for %d of %d step(s) that failed in source run %s, they pass in %s: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, in, capList(v.Cleared, 5))
		if len(v.StillFail) > 0 {
			fmt.Fprintf(&b, "  still fail, so another cause: %s\n", capList(v.StillFail, 5))
		}
	}
	if len(v.NotCounted) > 0 {
		fmt.Fprintf(&b, "  not counted: %s still fail with other values, and read what left-out %s wrote in run %s, so their expected values can count on that write\n",
			capList(v.NotCounted, 5), capList(v.writers, 3), v.SourceRun)
	}
	if len(v.NotRun) > 0 {
		fmt.Fprintf(&b, "  not run: %s\n", capList(v.NotRun, 5))
	}
	if len(v.NewFail) > 0 {
		fmt.Fprintf(&b, "  fail only without it: %s, so they need what the left-out steps did\n", capList(v.NewFail, 5))
	}
	if len(v.Needed) > 0 {
		fmt.Fprintf(&b, "  fail only without it, as expected: %s read what %s wrote\n", capList(v.Needed, 5), capList(v.writers, 3))
	}
	if v.OtherTarget != "" {
		fmt.Fprintf(&b, "  %s\n", v.OtherTarget)
	}
	return b.String()
}

func (v *withoutVerdict) err() error {
	if v == nil {
		return nil
	}
	without := strings.Join(v.Without, ", ")
	switch {
	case len(v.StillFail) > 0 && len(v.Cleared) == 0:
		return exitWith(1, "NOT REPRODUCED without %s", without)
	case len(v.StillFail) > 0:
		return exitWith(1, "without %s %d failing step(s) still fail: %s", without, len(v.StillFail), capList(v.StillFail, 5))
	case len(v.NotCounted) > 0 || len(v.NotRun) > 0 || len(v.NewFail) > 0 || v.OtherTarget != "":
		return exitWith(3, "INCONCLUSIVE: without %s some steps were not counted, not run or fail only there", without)
	}
	return nil
}
