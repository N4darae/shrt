package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
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
	unique []string
	chain  string
	unsure bool
}

func (f *fixtureReuse) finding() bool {
	return f != nil && f.repeat != "" && len(f.unique) > 0
}

func (f *fixtureReuse) builtFrom() string {
	parts := append([]string{}, f.vars...)
	if len(f.unique) > 0 {
		parts = append(parts, "${uuid} or a clock value ("+strings.Join(f.unique, ", ")+")")
	}
	return strings.Join(parts, ", ")
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
			"%s of this chain was refused there the same way with %s: a value built from ${uuid} or a clock value is unique to "+
			"its run, so neither can collide with leftover fixtures or another client, and the backend refuses the create itself. "+
			"This is a finding about the backend, not a fixture collision",
			f.step, f.why, f.builtFrom(), f.repeat, strings.Join(f.before, ", "))
	}
	if f.run == "" && f.repeat != "" {
		return fmt.Sprintf("fixture collision: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
			"and the previous run %s of this chain was refused there the same way with %s, also a value no recorded run had "+
			"created: a repeat with fresh values points at the backend unless another client uses the same values (two "+
			"pipelines deriving the tag from one commit SHA do), and shrt cannot tell which, so this is not a finding. Build "+
			"the field from ${uuid} to have a repeat reported as one",
			f.step, f.why, f.builtFrom(), f.repeat, strings.Join(f.before, ", "))
	}
	if f.run == "" {
		return fmt.Sprintf("fixture collision: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
			"and no recorded run of this chain used that value, so the record it collides with was created by something else "+
			"(another chain with the same value, another client, or a shared backend)",
			f.step, f.why, f.builtFrom())
	}
	held := "so the backend still holds what that run created"
	if f.unsure {
		held = "and whether the call took effect is unknown (it was sent and got no answer), so the backend most likely holds " +
			"what that run created"
	}
	if f.chain != "" {
		return fmt.Sprintf("fixture reused: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
			"and run %s of chain %s already sent that value, %s",
			f.step, f.why, strings.Join(f.vars, ", "), f.run, f.chain, held)
	}
	return fmt.Sprintf("fixture reused: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
		"and run %s of this chain already sent that value, %s",
		f.step, f.why, strings.Join(f.vars, ", "), f.run, held)
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

func (f *fixtureReuse) rerun(command, name string) string {
	if len(f.vars) == 0 {
		return fmt.Sprintf("a plain re-run generates a fresh value: shrt %s %s", command, name)
	}
	return fmt.Sprintf("re-run with a fresh value: shrt %s %s %s", command, name, f.fresh())
}

func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
	generated := generatedRequestPath(c)
	visitLeaves(req, "", func(path string) {
		unique := generated(first.ID, path)
		if !named(first.ID, path) && !unique {
			return
		}
		v, ok := requestTemplate(c, first.ID, path)
		text, isText := v.(string)
		if !ok || !isText {
			return
		}
		f := fixtureField{path: path, unique: unique}
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
	if refusalBlamesAnotherField(req, why, fields) {
		return nil
	}
	fed := map[string]bool{}
	conflicting := conflictingFields(fields, why)
	if inRunRepeatAcceptedBefore(e, rec, index, conflicting) {
		return nil
	}
	unique := []string{}
	for _, f := range conflicting {
		for _, n := range f.vars {
			fed[n] = true
		}
		if f.unique {
			unique = append(unique, f.path+"="+f.sent)
		}
	}
	if len(unique) != len(conflicting) {
		unique = nil
	}
	if len(unique) > 0 && !namesAField(fields, why) {
		return nil
	}
	if len(unique) > 0 {
		return uniqueCollision(e, rec, first, index, why, fields, conflicting, unique)
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
		prev, err := loadRunNamedAs(e, rec, ids[i])
		if err != nil || prev.DryRun || !usedBy(prev, first.ID) {
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
			return &fixtureReuse{step: first.ID, index: index, why: why, vars: same, run: prev.RunID, unsure: !createdBy(prev, first.ID)}
		}
	}
	sentValues := []string{}
	for _, f := range conflicting {
		if f.sent != "" {
			sentValues = append(sentValues, f.sent)
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
	if other, run, unsure := usedByAnotherChain(e, rec, sentValues); run != "" {
		f.chain, f.run, f.unsure = other, run, unsure
		return f
	}
	if prev := previousRunSending(e, rec, first.ID); prev != nil {
		if st, ok := prev.Step(first.ID); ok && st.Call == first.Call && refusedSameWay(why, fields, conflicting, st) {
			f.before = freshValuesOf(e, rec, prev, first.ID, names, conflicting)
			if f.before != nil {
				f.repeat = prev.RunID
			}
		}
	}
	return f
}

func uniqueCollision(e *env, rec *runner.Record, first *runner.StepRecord, index int, why string, all, fields []fixtureField, unique []string) *fixtureReuse {
	f := &fixtureReuse{step: first.ID, index: index, why: why, unique: unique}
	for _, field := range fields {
		for _, n := range field.vars {
			if v, ok := rec.Vars[n]; ok && fmt.Sprint(v) != pathmask.MaskRedacted {
				f.vars = append(f.vars, fmt.Sprintf("%s=%v", n, v))
			}
		}
	}
	prev := previousRunSending(e, rec, first.ID)
	if prev == nil {
		return f
	}
	st, ok := prev.Step(first.ID)
	if !ok || st.Call != first.Call || !refusedSameWay(why, all, fields, st) {
		return f
	}
	var req any
	if json.Unmarshal(st.Request, &req) != nil {
		return f
	}
	for _, field := range fields {
		if v, ok := chain.Get(req, field.path); ok {
			f.before = append(f.before, fmt.Sprintf("%s=%v", field.path, v))
		}
	}
	if len(f.before) > 0 {
		f.repeat = prev.RunID
	}
	return f
}

func freshValuesOf(e *env, rec, prev *runner.Record, step string, names []string, fields []fixtureField) []string {
	out := []string{}
	for _, n := range names {
		was, ok := prev.Vars[n]
		if !ok || fmt.Sprint(was) == pathmask.MaskRedacted || fmt.Sprint(was) == fmt.Sprint(rec.Vars[n]) {
			return nil
		}
		out = append(out, fmt.Sprintf("%s=%v", n, was))
	}
	if st, ok := prev.Step(step); ok {
		var req any
		sent := []string{}
		if json.Unmarshal(st.Request, &req) == nil {
			for _, f := range fields {
				if v, ok := chain.Get(req, f.path); ok {
					sent = append(sent, fmt.Sprint(v))
				}
			}
		}
		if _, run, _ := usedByAnotherChain(e, prev, sent); run != "" {
			return nil
		}
	}
	ids, _ := e.store.ListRuns(rec.Chain)
	for _, id := range ids {
		if id == prev.RunID || id == rec.RunID {
			continue
		}
		other, err := loadRunNamedAs(e, rec, id)
		if err != nil || other.DryRun || !ranBefore(other, prev) || !usedBy(other, step) {
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

func refusedSameWay(why string, all, conflicting []fixtureField, prev *runner.StepRecord) bool {
	was := stepRefusalText(prev)
	var req any
	if !uniquenessConflict.MatchString(was) || json.Unmarshal(prev.Request, &req) != nil {
		return false
	}
	then := make([]fixtureField, 0, len(all))
	for _, f := range all {
		f.sent = ""
		if v, ok := chain.Get(req, f.path); ok {
			f.sent = fmt.Sprint(v)
		}
		then = append(then, f)
	}
	return refusalCodes(was, then) == refusalCodes(why, all) && fieldPaths(conflictingFields(then, was)) == fieldPaths(conflicting)
}

func refusalBlamesAnotherField(req any, why string, fields []fixtureField) bool {
	fixture := map[string]bool{}
	for _, f := range fields {
		fixture[f.path] = true
	}
	others := []fixtureField{}
	visitLeaves(req, "", func(path string) {
		if fixture[path] {
			return
		}
		sent, _ := chain.Get(req, path)
		text, _ := sent.(string)
		if len(text) < 3 {
			text = ""
		}
		others = append(others, fixtureField{path: path, sent: text})
	})
	quoted := func(set []fixtureField) bool {
		for _, f := range set {
			if f.sent != "" && strings.Contains(why, f.sent) {
				return true
			}
		}
		return false
	}
	named := func(set []fixtureField, shortest int) bool {
		folded := foldName(why)
		for _, f := range set {
			if name := foldName(leafName(f.path)); name != "" && len(name) >= shortest && strings.Contains(folded, name) {
				return true
			}
		}
		return false
	}
	switch {
	case quoted(fields):
		return false
	case quoted(others):
		return true
	case named(fields, 1):
		return false
	}
	return named(others, 3)
}

func inRunRepeatAcceptedBefore(e *env, rec *runner.Record, index int, conflicting []fixtureField) bool {
	if len(conflicting) == 0 || index <= 0 || index >= len(rec.Steps) {
		return false
	}
	first := rec.Steps[index]
	for _, f := range conflicting {
		if f.sent == "" {
			return false
		}
		repeated := false
		for j := 0; j < index && !repeated; j++ {
			st := rec.Steps[j]
			if st == nil || st.Call != first.Call || !createdStep(st) {
				continue
			}
			var before any
			if json.Unmarshal(st.Request, &before) != nil {
				continue
			}
			if got, ok := chain.Get(before, f.path); ok && fmt.Sprint(got) == f.sent && repeatAcceptedBefore(e, rec, first.ID, st.ID, f.path) {
				repeated = true
			}
		}
		if !repeated {
			return false
		}
	}
	return true
}

func fieldPaths(fields []fixtureField) string {
	paths := make([]string, 0, len(fields))
	for _, f := range fields {
		paths = append(paths, f.path)
	}
	sort.Strings(paths)
	return strings.Join(paths, ",")
}

func refusalCodes(why string, fields []fixtureField) string {
	codes := []string{}
	for _, part := range strings.Split(why, ": ") {
		if part = strings.TrimSpace(part); part == "" || strings.ContainsAny(part, " \t\n") || quotesSent(part, fields) {
			continue
		}
		codes = append(codes, part)
	}
	return strings.Join(codes, " ")
}

func quotesSent(text string, fields []fixtureField) bool {
	for _, f := range fields {
		if f.sent != "" && strings.Contains(text, f.sent) {
			return true
		}
	}
	return false
}

type fixtureField struct {
	path   string
	sent   string
	vars   []string
	unique bool
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

func namesAField(fields []fixtureField, why string) bool {
	folded := foldName(why)
	for _, f := range fields {
		if f.sent != "" && strings.Contains(why, f.sent) {
			return true
		}
		if name := foldName(leafName(f.path)); name != "" && strings.Contains(folded, name) {
			return true
		}
	}
	return false
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

func usedByAnotherChain(e *env, rec *runner.Record, values []string) (string, string, bool) {
	if len(values) == 0 {
		return "", "", false
	}
	entries, err := os.ReadDir(e.store.RunsDir)
	if err != nil {
		return "", "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ids, _ := e.store.ListRuns(entry.Name())
		for i := len(ids) - 1; i >= 0; i-- {
			other, err := e.store.LoadRun(entry.Name(), ids[i])
			if err != nil || other.DryRun || other.Chain == rec.Chain {
				continue
			}
			for _, st := range other.Steps {
				if (createdStep(st) || sentUnknown(st)) && sendsAll(st, values) {
					return other.Chain, other.RunID, !createdStep(st)
				}
			}
		}
	}
	return "", "", false
}

func sentUnknown(st *runner.StepRecord) bool {
	return st != nil && st.Status == runner.StatusError && st.HTTPStatus == 0 && len(st.Response) == 0 && len(st.Request) > 0 &&
		(strings.Contains(st.Error, "whether the call took effect is unknown") || strings.Contains(st.Error, transport.NoAnswerBeforeTimeout))
}

func usedBy(rec *runner.Record, step string) bool {
	st, ok := rec.Step(step)
	return ok && (createdStep(st) || sentUnknown(st))
}

func sendsAll(st *runner.StepRecord, values []string) bool {
	var req any
	if json.Unmarshal(st.Request, &req) != nil {
		return false
	}
	for _, v := range values {
		found := false
		visitStrings(req, func(s string) {
			if s == v {
				found = true
			}
		})
		if !found {
			return false
		}
	}
	return true
}

func createdStep(st *runner.StepRecord) bool {
	return st != nil && st.Transport == nil && len(st.Response) > 0 && st.Status != runner.StatusSkipped && stepRefusalText(st) == ""
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
