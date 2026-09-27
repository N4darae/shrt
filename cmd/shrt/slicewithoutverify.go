package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
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
	OtherValues []string `json:"still_fail_other_values,omitempty"`
	Stores      []string `json:"left_out_moves,omitempty"`
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
			if gotMoved(src, now) {
				v.OtherValues = append(v.OtherValues, st.ID)
			}
		}
	}
	if len(v.Cleared) > 0 {
		if v.asBefore = leftOutAsBefore(e, res, rec, a.source); v.asBefore != "" {
			v.LikelyFault = v.Cleared[0]
			if lib, err := e.library(); err == nil {
				v.Stores = movedFieldsRead(lib, rpcOfCall(e), rec, v.writers, append(append([]string{}, v.Cleared...), v.StillFail...))
			}
		}
	}
	return v, nil
}

func rpcOfCall(e *env) func(string) string {
	return func(call string) string {
		if m, err := e.cat.Lookup(call); err == nil {
			return m.FullName
		}
		return call
	}
}

func movesField(ef *contract.Effect) bool {
	return ef != nil && (ef.Increase != "" || ef.Decrease != "" || ef.Restore != "" || ef.Sum != "")
}

func movedFieldsRead(lib *contract.Library, rpc func(string) string, rec *runner.Record, writers, failed []string) []string {
	moved := map[string]bool{}
	for _, w := range writers {
		sr, ok := rec.Step(w)
		if !ok {
			continue
		}
		if c, ok := lib.Get(rpc(sr.Call)); ok {
			for field, ef := range c.Effects {
				if movesField(ef) {
					moved[field] = true
				}
			}
		}
	}
	read := map[string]bool{}
	for _, id := range failed {
		sr, ok := rec.Step(id)
		if !ok {
			continue
		}
		if c, ok := lib.Get(rpc(sr.Call)); ok {
			for field, ef := range c.Effects {
				if moved[field] && movesField(ef) {
					read[field] = true
				}
			}
		}
		for _, x := range sr.Expect {
			segs := strings.Split(x.Path, ".")
			if last := segs[len(segs)-1]; !x.Passed && moved[last] {
				read[last] = true
			}
		}
	}
	return sortedKeys(read)
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
		same := v.sameValues()
		if len(same) == 0 {
			fmt.Fprintf(&b, "verify NOT REPRODUCED without %s: the %d step(s) that failed in source run %s still fail in %s, with other values: %s (the left-out write moves them)\n",
				without, counted, v.SourceRun, in, capList(v.OtherValues, 5))
			break
		}
		fmt.Fprintf(&b, "verify NOT REPRODUCED without %s: the %d step(s) that failed in source run %s still fail in %s: %s\n",
			without, counted, v.SourceRun, in, capList(same, 5))
		v.otherValuesLine(&b)
	case v.LikelyFault != "":
		fmt.Fprintf(&b, "verify without %s: %d of %d step(s) that failed in source run %s need it, they pass in %s: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, in, capList(v.Cleared, 5))
		if len(v.Stores) > 0 {
			fmt.Fprintf(&b, "  %s %s, so it is a precondition, or it stores other than it answers: it moves %s, which the failing steps read; look first at %s\n",
				without, v.asBefore, strings.Join(v.Stores, ", "), v.LikelyFault)
		} else {
			fmt.Fprintf(&b, "  %s %s, so it is a precondition: look first at %s\n", without, v.asBefore, v.LikelyFault)
		}
		v.stillFailLines(&b)
	default:
		fmt.Fprintf(&b, "verify without %s: cause confirmed for %d of %d step(s) that failed in source run %s, they pass in %s: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, in, capList(v.Cleared, 5))
		v.stillFailLines(&b)
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

func (v *withoutVerdict) sameValues() []string {
	out := []string{}
	for _, id := range v.StillFail {
		if !slices.Contains(v.OtherValues, id) {
			out = append(out, id)
		}
	}
	return out
}

func (v *withoutVerdict) otherValuesLine(b *strings.Builder) {
	if len(v.OtherValues) > 0 {
		fmt.Fprintf(b, "  still fail, with other values: %s (the left-out write moves them)\n", capList(v.OtherValues, 5))
	}
}

func (v *withoutVerdict) stillFailLines(b *strings.Builder) {
	if same := v.sameValues(); len(same) > 0 && len(v.Stores) > 0 {
		fmt.Fprintf(b, "  still fail: %s\n", capList(same, 5))
	} else if len(same) > 0 {
		fmt.Fprintf(b, "  still fail, so another cause: %s\n", capList(same, 5))
	}
	v.otherValuesLine(b)
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
