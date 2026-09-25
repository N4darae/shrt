package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

type idempotentReplay struct {
	step    string
	index   int
	field   string
	value   string
	spot    string
	literal bool
	vars    []string

	answered string
	id       string
}

func (r *idempotentReplay) line() string {
	if r.literal {
		return fmt.Sprintf("step %q sent %s=%s, the literal the confirmed run %s sent there too, so the backend answered "+
			"with that run's result instead of performing the call (an idempotent replay: it answered with the confirmed run's "+
			"%s %s): the changes at and after it are that replay, not a regression. This is a defect in the chain, and a fresh "+
			"-var does not help: build the key from ${uuid}, fresh per run", r.step, r.field, r.value, r.spot, r.answered, r.id)
	}
	return fmt.Sprintf("fixture reused: step %q sent %s=%s, built from %s, the value the confirmed run %s sent there too, "+
		"so the backend answered with that run's result instead of performing the call (an idempotent replay: it answered "+
		"with the confirmed run's %s %s): the changes at and after it are that replay, not a regression; building the key "+
		"from ${uuid} makes it fresh every run", r.step, r.field, r.value, strings.Join(r.vars, ", "), r.spot, r.answered, r.id)
}

func (r *idempotentReplay) fresh() string {
	out := []string{}
	for _, v := range r.vars {
		out = append(out, "-var "+v+"=<a value no run used>")
	}
	return strings.Join(out, " ")
}

func detectIdempotentReplay(c *chain.Chain, spot *store.SafeSpot, rec *runner.Record, report *diff.Report) *idempotentReplay {
	if c == nil || rec == nil || rec.DryRun || report.Clean() {
		return nil
	}
	changed := map[string]bool{}
	for _, ch := range report.Changes {
		if ch.Kind != diff.KindNotReached {
			changed[ch.Step] = true
		}
	}
	was := map[string]*runner.StepRecord{}
	for _, st := range spot.Steps {
		if st != nil {
			was[st.ID] = st
		}
	}
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		if prior := was[st.ID]; prior != nil {
			if r := replayedKey(c, st, prior); r != nil {
				if r.answered, r.id = sameIDAnswered(st, prior); r.answered != "" {
					r.index, r.spot = i, spot.RunID
					return r
				}
			}
		}
		if changed[st.ID] {
			return nil
		}
	}
	return nil
}

func replayedKey(c *chain.Chain, now, was *runner.StepRecord) *idempotentReplay {
	var a, b any
	if json.Unmarshal(now.Request, &b) == nil && json.Unmarshal(was.Request, &a) == nil {
		paths := []string{}
		visitLeaves(b, "", func(path string) {
			if chain.IdempotencyKeyName(leafName(path)) {
				paths = append(paths, path)
			}
		})
		sort.Strings(paths)
		for _, path := range paths {
			x, okA := chain.Get(a, path)
			y, okB := chain.Get(b, path)
			template, ok := requestTemplate(c, now.ID, path)
			text, isText := template.(string)
			if okA && okB && ok && isText {
				if r := sameKey(now.ID, path, fmt.Sprint(x), fmt.Sprint(y), text); r != nil {
					return r
				}
			}
		}
	}
	names := make([]string, 0, len(now.Headers))
	for k := range now.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !chain.IdempotencyKeyName(k) {
			continue
		}
		prior, had := was.Headers[k]
		template, ok := requestTemplate(c, now.ID, diff.HeadersPathPrefix+k)
		text, isText := template.(string)
		if had && ok && isText {
			if r := sameKey(now.ID, "header "+k, prior, now.Headers[k], text); r != nil {
				return r
			}
		}
	}
	return nil
}

func sameKey(step, field, was, now, template string) *idempotentReplay {
	if was == "" || was != now || was == pathmask.MaskRedacted {
		return nil
	}
	refs := requestRef.FindAllStringSubmatch(template, -1)
	r := &idempotentReplay{step: step, field: field, value: now, literal: len(refs) == 0}
	for _, m := range refs {
		n := varName.FindStringSubmatch(strings.TrimSpace(m[1]))
		if n == nil {
			return nil
		}
		r.vars = append(r.vars, n[1])
	}
	return r
}

func sameIDAnswered(now, was *runner.StepRecord) (string, string) {
	var a, b, req, prior any
	if json.Unmarshal(was.Response, &a) != nil || json.Unmarshal(now.Response, &b) != nil {
		return "", ""
	}
	_ = json.Unmarshal(now.Request, &req)
	_ = json.Unmarshal(was.Request, &prior)
	sent := map[string]bool{}
	visitStrings(req, func(v string) { sent[v] = true })
	visitStrings(prior, func(v string) { sent[v] = true })
	paths := []string{}
	visitLeaves(b, "", func(path string) {
		if diff.IDNamedPath(path) {
			paths = append(paths, path)
		}
	})
	sort.Strings(paths)
	for _, path := range paths {
		x, okA := chain.Get(a, path)
		y, okB := chain.Get(b, path)
		id, isText := y.(string)
		if okA && okB && isText && len(id) >= 4 && fmt.Sprint(x) == id && !sent[id] {
			return path, id
		}
	}
	return "", ""
}
