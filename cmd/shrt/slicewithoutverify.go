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
	ref  string
	sent bool
}

type withoutVerdict struct {
	Without   []string `json:"without"`
	SourceRun string   `json:"source_run"`
	Run       string   `json:"run,omitempty"`
	Cleared   []string `json:"no_longer_fail"`
	Changed   []string `json:"fail_differently,omitempty"`
	Changes   []string `json:"differences,omitempty"`
	StillFail []string `json:"still_fail"`
	NewFail   []string `json:"fail_only_without,omitempty"`
	Next      string   `json:"next,omitempty"`
	newer     string
	readsOut  bool
	state     []string
	noun      string
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
	same := sameUpToFixtures(rec.Vars, run.Vars)
	for _, st := range res.Chain.Steps {
		src, reached := rec.Step(st.ID)
		now, ran := run.Step(st.ID)
		var changes []string
		if reached && ran {
			changes = compareVerdictsAt(st, verdictOf(src), verdictOf(now), same, "without it")
		}
		switch {
		case !reached || !failing(src):
			if ran && failing(now) {
				v.NewFail = append(v.NewFail, st.ID)
			}
		case ran && now.Status == runner.StatusPassed:
			v.Cleared = append(v.Cleared, st.ID)
		case len(changes) > 0:
			v.Changed = append(v.Changed, st.ID)
			for _, c := range changes {
				v.Changes = append(v.Changes, st.ID+" "+c)
			}
		default:
			v.StillFail = append(v.StillFail, st.ID)
		}
	}
	if len(v.Changed) > 0 && a.ref != "" {
		v.Next = fmt.Sprintf("shrt chain slice %s -step %s -verify -run %s", a.ref, v.Changed[0], rec.RunID)
	}
	v.readsOut = len(v.StillFail) > 0 && !slices.ContainsFunc(v.StillFail, func(id string) bool { return !readsLeftOut(rec, res, id) })
	v.state, v.noun = leftState(rec, res, v.Cleared)
	return v, nil
}

func leftState(rec *runner.Record, res *chain.WithoutResult, cleared []string) ([]string, string) {
	acted := map[string]string{}
	for _, r := range res.Removed {
		for id := range entityFactsOf(rec, r.ID).acts {
			if acted[id] == "" {
				acted[id] = r.ID
			}
		}
	}
	var out []string
	noun, wrote := "", false
	for _, sr := range rec.Steps {
		if sr == nil || !slices.Contains(cleared, sr.ID) {
			continue
		}
		write := isWrite(sr)
		for _, id := range sortedKeys(entityFactsOf(rec, sr.ID).mentions) {
			if acted[id] != "" && (write || wrote) {
				wrote = wrote || write
				out = append(out, sr.ID)
				if noun == "" {
					noun = recordNoun(rec, acted[id], id)
				}
				break
			}
		}
	}
	if !wrote {
		return nil, ""
	}
	return out, noun
}

func recordNoun(rec *runner.Record, step, id string) string {
	sr, ok := rec.Step(step)
	if !ok {
		return "record"
	}
	request, response := decodedRecordStep(sr)
	if top, ok := response.(map[string]any); ok {
		for _, k := range sortedKeys(top) {
			if obj, ok := top[k].(map[string]any); ok {
				if key := primaryIDKey(k, obj); key != "" && obj[key] == id {
					return k
				}
			}
		}
	}
	for _, body := range []any{request, response} {
		top, _ := body.(map[string]any)
		for _, k := range sortedKeys(top) {
			if top[k] == id && isIDKey(k) {
				if n := strings.TrimSuffix(strings.TrimPrefix(k, "id_"), "_id"); n != "" && n != k {
					return n
				}
			}
		}
	}
	return "record"
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
	return slices.ContainsFunc(items, func(item any) bool { return ownsField(item, parent, field, ids) })
}

func failing(sr *runner.StepRecord) bool {
	return sr.Status == runner.StatusFailed || sr.Status == runner.StatusError
}

func (v *withoutVerdict) text() string {
	var b strings.Builder
	without := capList(v.Without, 3)
	counted := len(v.Cleared) + len(v.Changed) + len(v.StillFail)
	verb := "is"
	if len(v.Without) > 1 {
		verb = "are"
	}
	switch {
	case counted == 0:
		fmt.Fprintf(&b, "verify without %s: no step left in failed in source run %s%s\n", without, v.SourceRun, v.newer)
	case len(v.Cleared) == 0 && len(v.Changed) > 0:
		some := fmt.Sprintf("the %d", counted)
		if len(v.Changed) < counted {
			some = fmt.Sprintf("%d of the %d", len(v.Changed), counted)
		}
		fmt.Fprintf(&b, "verify FAILS DIFFERENTLY without %s: %s step(s) that failed in source run %s still fail, but not as they did (%s), so %s %s involved: leaving it out changes their answer\n",
			without, some, v.SourceRun, capList(v.Changed, 5), without, verb)
	case len(v.Cleared) == 0 && v.readsOut:
		fmt.Fprintf(&b, "verify INCONCLUSIVE without %s: the %d step(s) that failed in source run %s still fail, but they read what the left-out steps write: %s\n",
			without, counted, v.SourceRun, capList(v.StillFail, 5))
	case len(v.Cleared) == 0:
		fmt.Fprintf(&b, "verify STILL FAILS without %s: the %d step(s) that failed in source run %s still fail exactly as they did (%s), so %s %s not their cause\n",
			without, counted, v.SourceRun, capList(v.StillFail, 5), without, verb)
	default:
		fmt.Fprintf(&b, "verify without %s: %d of %d step(s) that failed in source run %s pass without it: %s%s\n",
			without, len(v.Cleared), counted, v.SourceRun, capList(v.Cleared, 5), v.stateNote())
		if len(v.Changed) > 0 {
			fmt.Fprintf(&b, "  fail differently without it, so it changes them too: %s\n", capList(v.Changed, 5))
		}
	}
	for _, c := range v.Changes {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	if why := "so another cause"; len(v.StillFail) > 0 && counted > len(v.StillFail) {
		if v.readsOut {
			why = "they read what the left-out steps write"
		}
		fmt.Fprintf(&b, "  still fail as they did, %s: %s\n", why, capList(v.StillFail, 5))
	}
	if len(v.NewFail) > 0 {
		fmt.Fprintf(&b, "  fail only without it: %s, they need what the left-out steps did\n", capList(v.NewFail, 5))
	}
	if len(v.Changed) > 0 {
		fmt.Fprintf(&b, "  a step that expects what %s does cannot pass without it: this neither clears it nor proves it the cause\n", without)
	}
	if v.Next != "" {
		fmt.Fprintf(&b, "  next: %s\n", v.Next)
	}
	return b.String()
}

func (v *withoutVerdict) stateNote() string {
	if len(v.state) == 0 {
		return ""
	}
	without := capList(v.Without, 3)
	who, act, is := "they", "act", "is"
	if len(v.state) < len(v.Cleared) {
		who = capList(v.state, 3)
	}
	if len(v.state) == 1 {
		act = "acts"
		if who == "they" {
			who = "it"
		}
	}
	if len(v.Without) > 1 {
		is = "are"
	}
	return fmt.Sprintf(" (%s %s on the %s %s changed and may need that state: not proof %s %s the cause)", who, act, v.noun, without, without, is)
}

func (v *withoutVerdict) err() error {
	if v == nil {
		return nil
	}
	without := capList(v.Without, 3)
	switch {
	case len(v.Changed) > 0:
		return exitWith(3, "FAILS DIFFERENTLY without %s: %d failing step(s) fail otherwise than in source run %s", without, len(v.Changed), v.SourceRun)
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
