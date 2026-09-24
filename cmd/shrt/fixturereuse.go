package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

var uniquenessConflict = regexp.MustCompile(`(?i)(already (exists?|registered|taken|in use|used)|duplicate|not unique|unique constraint|already_exists|alreadyexists|[a-z]taken\b|\btaken\b|\bconflict\b)`)

type fixtureReuse struct {
	step  string
	index int
	why   string
	vars  []string
	run   string
}

func (f *fixtureReuse) line() string {
	return fmt.Sprintf("fixture reused: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
		"and run %s of this chain already used that value, so the backend still holds what that run created",
		f.step, f.why, strings.Join(f.vars, ", "), f.run)
}

func (f *fixtureReuse) fresh() string {
	names := []string{}
	for _, v := range f.vars {
		name, _, _ := strings.Cut(v, "=")
		names = append(names, "-var "+name+"=<a value no run used>")
	}
	return strings.Join(names, " ")
}

func detectFixtureReuse(e *env, c *chain.Chain, rec *runner.Record) *fixtureReuse {
	if c == nil || rec == nil || rec.DryRun {
		return nil
	}
	var first *runner.StepRecord
	index := -1
	for i, st := range rec.Steps {
		if st.Status == runner.StatusFailed || st.Status == runner.StatusError {
			first, index = st, i
			break
		}
	}
	if first == nil {
		return nil
	}
	why := stepRefusalText(first)
	if why == "" || !uniquenessConflict.MatchString(why) {
		return nil
	}
	named := fixtureRequestPath(c)
	fed := map[string]bool{}
	var req any
	if err := json.Unmarshal(first.Request, &req); err != nil {
		return nil
	}
	visitLeaves(req, "", func(path string) {
		if !named(first.ID, path) {
			return
		}
		if v, ok := requestTemplate(c, first.ID, path); ok {
			if text, ok := v.(string); ok {
				for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
					if n := varName.FindStringSubmatch(strings.TrimSpace(m[1])); n != nil {
						fed[n[1]] = true
					}
				}
			}
		}
	})
	if len(fed) == 0 {
		return nil
	}
	ids, err := e.store.ListRuns(rec.Chain)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(fed))
	for n := range fed {
		names = append(names, n)
	}
	sort.Strings(names)
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err != nil || prev.DryRun {
			continue
		}
		same := []string{}
		for _, n := range names {
			now, okNow := rec.Vars[n]
			was, okWas := prev.Vars[n]
			if okNow && okWas && fmt.Sprint(now) == fmt.Sprint(was) && fmt.Sprint(now) != pathmask.MaskRedacted {
				same = append(same, fmt.Sprintf("%s=%v", n, now))
			}
		}
		if len(same) > 0 {
			return &fixtureReuse{step: first.ID, index: index, why: why, vars: same, run: prev.RunID}
		}
	}
	return nil
}

func stepRefusalText(st *runner.StepRecord) string {
	if st.Transport != nil {
		return strings.TrimSpace(st.Transport.Code + ": " + st.Transport.Message)
	}
	var body any
	if err := json.Unmarshal(st.Response, &body); err != nil {
		return ""
	}
	code, ok := chain.Get(body, chain.EnvelopePath())
	if !ok || code == nil || fmt.Sprint(code) == chain.EnvelopeOK() || fmt.Sprint(code) == "" {
		return ""
	}
	parts := []string{fmt.Sprint(code)}
	envelope, _ := chain.Get(body, chain.EnvelopeField())
	visitStrings(envelope, func(s string) {
		if s != "" && s != fmt.Sprint(code) {
			parts = append(parts, s)
		}
	})
	return strings.Join(parts, ": ")
}

func visitLeaves(v any, path string, visit func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			visitLeaves(x, pathmask.Join(path, k), visit)
		}
	case []any:
		for i, x := range t {
			visitLeaves(x, pathmask.Join(path, pathmask.IndexKey(i)), visit)
		}
	default:
		visit(path)
	}
}

func visitStrings(v any, visit func(string)) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			visitStrings(t[k], visit)
		}
	case []any:
		for _, x := range t {
			visitStrings(x, visit)
		}
	case string:
		visit(t)
	}
}

func driftedBefore(rec *runner.Record, report *diff.Report, from int) bool {
	index := map[string]int{}
	for i, st := range rec.Steps {
		index[st.ID] = i
	}
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		if i, ok := index[c.Step]; ok && i < from {
			return true
		}
	}
	return false
}
