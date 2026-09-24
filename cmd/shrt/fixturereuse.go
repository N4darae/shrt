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
	step   string
	index  int
	why    string
	vars   []string
	run    string
	repeat string
	before []string
}

func (f *fixtureReuse) finding() bool {
	return f != nil && f.repeat != ""
}

func (f *fixtureReuse) verdict() string {
	if f.run == "" {
		return "fixture collision"
	}
	return "fixture reused"
}

func (f *fixtureReuse) line() string {
	if f.finding() {
		return fmt.Sprintf("step %q was refused as a uniqueness conflict (%s) on a field built from %s, and the previous run "+
			"%s of this chain was refused there the same way with %s, a different value no recorded run had created: two "+
			"fresh values in a row cannot both collide with leftover fixtures, so the backend refuses the create itself. "+
			"This is a finding about the backend, not a fixture collision",
			f.step, f.why, strings.Join(f.vars, ", "), f.repeat, strings.Join(f.before, ", "))
	}
	if f.run == "" {
		return fmt.Sprintf("fixture collision: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
			"and no recorded run of this chain used that value, so the record it collides with was created by something else "+
			"(another chain with the same value, another client, or a shared backend)",
			f.step, f.why, strings.Join(f.vars, ", "))
	}
	return fmt.Sprintf("fixture reused: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
		"and run %s of this chain already used that value, so the backend still holds what that run created",
		f.step, f.why, strings.Join(f.vars, ", "), f.run)
}

func (f *fixtureReuse) fresh() string {
	fill := "<a value no run used>"
	if f.run == "" {
		fill = "<a value nothing on this backend used>"
	}
	names := []string{}
	for _, v := range f.vars {
		name, _, _ := strings.Cut(v, "=")
		names = append(names, "-var "+name+"="+fill)
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
	var req any
	if err := json.Unmarshal(first.Request, &req); err != nil {
		return nil
	}
	fields := []fixtureField{}
	visitLeaves(req, "", func(path string) {
		if !named(first.ID, path) {
			return
		}
		v, ok := requestTemplate(c, first.ID, path)
		text, isText := v.(string)
		if !ok || !isText {
			return
		}
		f := fixtureField{path: path}
		if sent, ok := chain.Get(req, path); ok {
			f.sent = fmt.Sprint(sent)
		}
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			if n := varName.FindStringSubmatch(strings.TrimSpace(m[1])); n != nil {
				f.vars = append(f.vars, n[1])
			}
		}
		fields = append(fields, f)
	})
	fed := map[string]bool{}
	for _, f := range conflictingFields(fields, why) {
		for _, n := range f.vars {
			fed[n] = true
		}
	}
	if len(fed) == 0 {
		return nil
	}
	ids, _ := e.store.ListRuns(rec.Chain)
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
		if err != nil || prev.DryRun || !createdBy(prev, first.ID) {
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
	current := []string{}
	for _, n := range names {
		if v, ok := rec.Vars[n]; ok && fmt.Sprint(v) != pathmask.MaskRedacted {
			current = append(current, fmt.Sprintf("%s=%v", n, v))
		}
	}
	if len(current) == 0 {
		return nil
	}
	f := &fixtureReuse{step: first.ID, index: index, why: why, vars: current}
	if prev := previousRunSending(e, rec, first.ID); prev != nil {
		if st, ok := prev.Step(first.ID); ok && st.Call == first.Call && uniquenessConflict.MatchString(stepRefusalText(st)) {
			f.before = freshValuesOf(e, rec, prev, first.ID, names)
			if f.before != nil {
				f.repeat = prev.RunID
			}
		}
	}
	return f
}

func freshValuesOf(e *env, rec, prev *runner.Record, step string, names []string) []string {
	out := []string{}
	for _, n := range names {
		was, ok := prev.Vars[n]
		if !ok || fmt.Sprint(was) == pathmask.MaskRedacted || fmt.Sprint(was) == fmt.Sprint(rec.Vars[n]) {
			return nil
		}
		out = append(out, fmt.Sprintf("%s=%v", n, was))
	}
	ids, _ := e.store.ListRuns(rec.Chain)
	for _, id := range ids {
		if id == prev.RunID || id == rec.RunID {
			continue
		}
		other, err := e.store.LoadRun(rec.Chain, id)
		if err != nil || other.DryRun || !ranBefore(other, prev) || !createdBy(other, step) {
			continue
		}
		for _, n := range names {
			if v, ok := other.Vars[n]; ok && fmt.Sprint(v) == fmt.Sprint(prev.Vars[n]) {
				return nil
			}
		}
	}
	return out
}

type fixtureField struct {
	path string
	sent string
	vars []string
}

func conflictingFields(fields []fixtureField, why string) []fixtureField {
	byValue, byName := []fixtureField{}, []fixtureField{}
	folded := foldName(why)
	for _, f := range fields {
		if f.sent != "" && strings.Contains(why, f.sent) {
			byValue = append(byValue, f)
		}
		leaf := f.path
		if i := strings.LastIndex(leaf, "."); i >= 0 {
			leaf = leaf[i+1:]
		}
		if name := foldName(leaf); name != "" && strings.Contains(folded, name) {
			byName = append(byName, f)
		}
	}
	if len(byValue) > 0 {
		return byValue
	}
	if len(byName) > 0 {
		return byName
	}
	return fields
}

func foldName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func createdBy(rec *runner.Record, step string) bool {
	for _, st := range rec.Steps {
		if st == nil || st.ID != step {
			continue
		}
		return st.Transport == nil && len(st.Response) > 0 && st.Status != runner.StatusSkipped && stepRefusalText(st) == ""
	}
	return false
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
		if i, ok := index[c.Step]; ok && i < from && !runner.NotAnsweredByService(rec.Steps[i]) {
			return true
		}
	}
	return false
}
