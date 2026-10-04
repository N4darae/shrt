package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

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
	recorded bool
	chain    string
}

func (r *idempotentReplay) source() string {
	switch {
	case r.chain != "":
		return fmt.Sprintf("run %s of chain %s", r.spot, r.chain)
	case r.recorded:
		return fmt.Sprintf("the recorded run %s of this chain", r.spot)
	}
	return "the confirmed run " + r.spot
}

func (r *idempotentReplay) whose() string {
	if r.recorded || r.chain != "" {
		return "that run's"
	}
	return "the confirmed run's"
}

func (r *idempotentReplay) headline() string {
	if r.recorded || r.chain != "" {
		return fmt.Sprintf("fixture reused: step %s sent the idempotency key %s already sent", r.step, r.source())
	}
	return fmt.Sprintf("fixture reused: step %s sent the confirmed run's idempotency key", r.step)
}

func (r *idempotentReplay) line() string {
	if r.literal {
		return fmt.Sprintf("step %q sent %s=%s, the literal %s sent there too, so the backend answered "+
			"with that run's result instead of performing the call (an idempotent replay: it answered with %s "+
			"%s %s): the changes at and after it are that replay, not a regression. This is a defect in the chain, and a fresh "+
			"-var does not help: build the key from ${uuid}, fresh per run", r.step, r.field, r.value, r.source(), r.whose(), r.answered, r.id)
	}
	return fmt.Sprintf("fixture reused: step %q sent %s=%s, built from %s, the value %s sent there too, "+
		"so the backend answered with that run's result instead of performing the call (an idempotent replay: it answered "+
		"with %s %s %s): the changes at and after it are that replay, not a regression; building the key "+
		"from ${uuid} makes it fresh every run", r.step, r.field, r.value, strings.Join(r.vars, ", "), r.source(), r.whose(), r.answered, r.id)
}

func (r *idempotentReplay) note() string {
	return fmt.Sprintf("note: step %q sent %s=%s, the key %s sent there too, and the backend answered with %s %s %s: "+
		"an idempotent replay, so the changes at and after %s may be that replay; the change(s) before it are not explained by it",
		r.step, r.field, r.value, r.source(), r.whose(), r.answered, r.id, r.step) + r.keyAdvice()
}

func (r *idempotentReplay) keyAdvice() string {
	if r.literal {
		return "; the key is a literal, a defect in the chain: build it from ${uuid}, fresh per run"
	}
	return "; build the key from ${uuid} to make it fresh every run"
}

func (r *idempotentReplay) fresh() string {
	out := []string{}
	for _, v := range r.vars {
		out = append(out, "-var "+v+"=<a value no run used>")
	}
	return strings.Join(out, " ")
}

func detectIdempotentReplay(e *env, c *chain.Chain, spot *store.SafeSpot, rec *runner.Record, report *diff.Report) *idempotentReplay {
	if c == nil || rec == nil || rec.DryRun || report.Clean() {
		return nil
	}
	was := map[string]*runner.StepRecord{}
	for _, st := range spot.Steps {
		if st != nil {
			was[st.ID] = st
		}
	}
	recorded := sync.OnceValue(func() []*runner.Record { return recordedRuns(e, rec, spot.RunID) })
	for i, st := range rec.Steps {
		if st == nil {
			continue
		}
		keyed := replayedKeys(c, st)
		if prior := was[st.ID]; prior != nil {
			if r := keyed(prior); r != nil {
				if r.answered, r.id = sameIDAnswered(st, prior); r.answered != "" {
					r.index, r.spot = i, spot.RunID
					return r
				}
			}
		}
		if e != nil && sendsIdempotencyKey(st) {
			if r := replayOfRecorded(keyed, st, rec.Chain, recorded()); r != nil {
				r.index = i
				return r
			}
		}
	}
	return nil
}

func sendsIdempotencyKey(st *runner.StepRecord) bool {
	for k := range st.Headers {
		if chain.IdempotencyKeyName(k) {
			return true
		}
	}
	return len(leafPaths(decoded(st.Request), idempotencyKeyPath)) > 0
}

func idempotencyKeyPath(path string) bool { return chain.IdempotencyKeyName(leafName(path)) }

func leafPaths(v any, keep func(string) bool) []string {
	var paths []string
	eachLeaf(v, "", func(path string, _ any) {
		if keep(path) {
			paths = append(paths, path)
		}
	})
	sort.Strings(paths)
	return paths
}

func recordedRuns(e *env, rec *runner.Record, spot string) []*runner.Record {
	out := []*runner.Record{}
	add := func(name string, skip ...string) {
		for prev := range newestRuns(e, name, skip...) {
			if prev.RunID != rec.RunID {
				out = append(out, prev)
			}
		}
	}
	add(rec.Chain, rec.RunID, spot)
	entries, err := os.ReadDir(e.store.RunsDir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != rec.Chain {
			add(entry.Name())
		}
	}
	return out
}

func replayOfRecorded(keyed func(*runner.StepRecord) *idempotentReplay, st *runner.StepRecord, name string, runs []*runner.Record) *idempotentReplay {
	var found *idempotentReplay
	for _, prev := range runs {
		if found != nil && prev.Chain != name {
			return found
		}
		for _, prior := range prev.Steps {
			if prior == nil || prior.Call != st.Call || prev.Chain == name && prior.ID != st.ID {
				continue
			}
			r := keyed(prior)
			if r == nil || !answeredCleanly(prior) {
				continue
			}
			if r.answered, r.id = sameIDAnswered(st, prior); r.answered != "" {
				r.spot, r.recorded = prev.RunID, true
				if prev.Chain != name {
					r.chain = prev.Chain
					return r
				}
				found = r
				break
			}
		}
	}
	return found
}

func replayedKeys(c *chain.Chain, now *runner.StepRecord) func(was *runner.StepRecord) *idempotentReplay {
	type key struct{ field, name, value, template string }
	var body, headers []key
	b := decoded(now.Request)
	for _, path := range leafPaths(b, idempotencyKeyPath) {
		y, okB := chain.Get(b, path)
		template, ok := requestTemplate(c, now.ID, path)
		if text, isText := template.(string); okB && ok && isText {
			body = append(body, key{path, path, fmt.Sprint(y), text})
		}
	}
	for _, k := range sortedKeys(now.Headers) {
		if !chain.IdempotencyKeyName(k) {
			continue
		}
		template, ok := requestTemplate(c, now.ID, diff.HeadersPathPrefix+k)
		if text, isText := template.(string); ok && isText {
			headers = append(headers, key{"header " + k, k, now.Headers[k], text})
		}
	}
	return func(was *runner.StepRecord) *idempotentReplay {
		a := decoded(was.Request)
		for _, k := range body {
			if x, ok := chain.Get(a, k.name); ok {
				if r := sameKey(now.ID, k.field, fmt.Sprint(x), k.value, k.template); r != nil {
					return r
				}
			}
		}
		for _, k := range headers {
			if prior, had := was.Headers[k.name]; had {
				if r := sameKey(now.ID, k.field, prior, k.value, k.template); r != nil {
					return r
				}
			}
		}
		return nil
	}
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
	a, b := decoded(was.Response), decoded(now.Response)
	sent := map[string]bool{}
	visitStrings(decoded(now.Request), func(v string) { sent[v] = true })
	visitStrings(decoded(was.Request), func(v string) { sent[v] = true })
	for _, path := range leafPaths(b, diff.IDNamedPath) {
		x, okA := chain.Get(a, path)
		y, okB := chain.Get(b, path)
		id, isText := y.(string)
		if okA && okB && isText && len(id) >= 4 && fmt.Sprint(x) == id && !sent[id] {
			return path, id
		}
	}
	return "", ""
}
