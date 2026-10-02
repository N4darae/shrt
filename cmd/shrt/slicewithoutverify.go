package main

import (
	"context"
	"fmt"
	"slices"
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
	readsOut  bool
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
	v.readsOut = len(v.StillFail) > 0 && !slices.ContainsFunc(v.StillFail, func(id string) bool { return !readsLeftOut(rec, res, id) })
	return v, nil
}

func readsLeftOut(rec *runner.Record, res *chain.WithoutResult, step string) bool {
	mentions := entityFactsOf(rec, step).mentions
	for _, r := range res.Removed {
		for id := range entityFactsOf(rec, r.ID).acts {
			if mentions[id] && assertsWritten(rec, r.ID, step, nil) {
				return true
			}
		}
	}
	return false
}

func assertsWritten(rec *runner.Record, writer, reader string, ids map[string]bool) bool {
	w, wrote := rec.Step(writer)
	r, read := rec.Step(reader)
	if !wrote || !read {
		return false
	}
	_, response := decodedRecordStep(w)
	for _, e := range r.Expect {
		segs := chain.SplitPath(e.Path)
		asserts := e.Rule == "equals" && ids != nil || !e.Passed && ids == nil
		if asserts && len(segs) > 0 && segs[0] != "transport" && ownsField(response, "", segs[len(segs)-1], ids) {
			return true
		}
	}
	return false
}

func ownsField(v any, parent, field string, ids map[string]bool) bool {
	items, _ := v.([]any)
	if t, ok := v.(map[string]any); ok {
		if item := t[field]; item != nil && item != "" && item != "0" && item != false && item != float64(0) {
			if key := primaryIDKey(parent, t); ids == nil || key != "" && ids[t[key].(string)] {
				return true
			}
		}
		for k, item := range t {
			if k != envelopeParent() && ownsField(item, k, field, ids) {
				return true
			}
		}
	}
	for _, item := range items {
		if ownsField(item, parent, field, ids) {
			return true
		}
	}
	return false
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
	case len(v.Cleared) == 0 && v.readsOut:
		fmt.Fprintf(&b, "verify INCONCLUSIVE without %s: the %d step(s) that failed in source run %s still fail, but they read what the left-out steps write: %s\n",
			without, counted, v.SourceRun, capList(v.StillFail, 5))
	case len(v.Cleared) == 0:
		verb := "is"
		if len(v.Without) > 1 {
			verb = "are"
		}
		fmt.Fprintf(&b, "verify STILL FAILS without %s: the %d step(s) that failed in source run %s still fail (%s), so %s %s not their cause\n",
			without, counted, v.SourceRun, capList(v.StillFail, 5), without, verb)
	default:
		fmt.Fprintf(&b, "verify without %s: %d of %d step(s) that failed in source run %s pass without it: %s\n",
			without, len(v.Cleared), counted, v.SourceRun, capList(v.Cleared, 5))
		if why := "so another cause"; len(v.StillFail) > 0 {
			if v.readsOut {
				why = "they read what the left-out steps write"
			}
			fmt.Fprintf(&b, "  still fail, %s: %s\n", why, capList(v.StillFail, 5))
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
	case v.readsOut && len(v.Cleared) == 0:
		return exitWith(3, "INCONCLUSIVE without %s", without)
	case v.readsOut:
		return exitWith(3, "INCONCLUSIVE without %s: %d failing step(s) still fail and read what the left-out steps write", without, len(v.StillFail))
	case len(v.StillFail) > 0 && len(v.Cleared) == 0:
		return exitWith(1, "STILL FAILS without %s", without)
	case len(v.StillFail) > 0:
		return exitWith(1, "without %s %d failing step(s) still fail", without, len(v.StillFail))
	case len(v.Cleared) == 0 && len(v.NewFail) > 0:
		return exitWith(1, "without %s %d step(s) fail that passed in source run %s", without, len(v.NewFail), v.SourceRun)
	}
	return nil
}
