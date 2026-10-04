package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
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
	sentAt string
	sentBy []string
	erred  *runner.StepRecord
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
	switch {
	case f.erred != nil:
		return "collision with this run's own record"
	case f.run == "":
		return "fixture collision"
	}
	return "fixture reused"
}

func (f *fixtureReuse) line() string {
	if f.erred != nil {
		return fmt.Sprintf("step %q was refused as a uniqueness conflict (%s), and step %d %s of this run sent that value "+
			"and got a server error (%s): the backend stored that create though it failed it, so the conflict is with this "+
			"run's own record, not a fixture", f.step, f.why, f.erred.Index, f.erred.ID, errorText(f.erred))
	}
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
	if f.sentAt != "" {
		by := ""
		if len(f.sentBy) > 0 {
			by = " with " + strings.Join(f.sentBy, ", ")
		}
		return fmt.Sprintf("fixture reused: step %q was refused as a uniqueness conflict (%s) on a field built from %s, "+
			"and run %s of this chain already sent that value at step %q%s, %s: two tags of this chain built the same value",
			f.step, f.why, strings.Join(f.vars, ", "), f.run, f.sentAt, by, held)
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
	if f.erred != nil {
		return "a fresh value collides the same way while that step stores what it fails"
	}
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
	first, index, why, req := uniquenessRefusal(c, rec)
	if first == nil {
		return nil
	}
	named := fixtureRequestPath(c)
	fields := []fixtureField{}
	generated := generatedRequestPath(c)
	eachLeaf(req, "", func(path string, _ any) {
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
		f.vars = append(f.vars, varRefs(text)...)
		fields = append(fields, f)
	})
	if refusalBlamesAnotherField(req, why, fields) {
		return nil
	}
	fed := map[string]bool{}
	conflicting, cited := conflictingFields(fields, why)
	if erred := erredBefore(rec, index, conflicting); erred != nil {
		return &fixtureReuse{step: first.ID, index: index, why: why, erred: erred}
	}
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
	if len(unique) > 0 && !cited {
		return nil
	}
	if len(unique) > 0 {
		return uniqueCollision(e, rec, first, index, why, fields, conflicting, unique)
	}
	if len(fed) == 0 {
		return nil
	}
	names := sortedKeys(fed)
	for prev := range newestRuns(e, rec.Chain, rec.RunID) {
		if prev = namedAs(prev, rec); !usedBy(prev, first.ID) {
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
			st, _ := prev.Step(first.ID)
			return &fixtureReuse{step: first.ID, index: index, why: why, vars: same, run: prev.RunID, unsure: !answeredCleanly(st)}
		}
	}
	sentValues := []string{}
	for _, f := range conflicting {
		if f.sent != "" {
			sentValues = append(sentValues, f.sent)
		}
	}
	current := varValues(rec.Vars, names)
	if len(current) == 0 {
		return nil
	}
	f := &fixtureReuse{step: first.ID, index: index, why: why, vars: current}
	if run, step, by, unsure := sentByThisChain(e, rec, names, sentValues); run != "" {
		f.run, f.sentAt, f.sentBy, f.unsure = run, step, by, unsure
		return f
	}
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

func uniquenessRefusal(c *chain.Chain, rec *runner.Record) (*runner.StepRecord, int, string, any) {
	if c == nil || rec == nil || rec.DryRun {
		return nil, -1, "", nil
	}
	for i, st := range rec.Steps {
		if !failing(st) {
			continue
		}
		why := stepRefusalText(st)
		var req any
		if why == "" || !uniquenessConflict.MatchString(why) || json.Unmarshal(st.Request, &req) != nil {
			return nil, -1, "", nil
		}
		return st, i, why, req
	}
	return nil, -1, "", nil
}

func uniqueCollision(e *env, rec *runner.Record, first *runner.StepRecord, index int, why string, all, fields []fixtureField, unique []string) *fixtureReuse {
	f := &fixtureReuse{step: first.ID, index: index, why: why, unique: unique}
	for _, field := range fields {
		f.vars = append(f.vars, varValues(rec.Vars, field.vars)...)
	}
	prev := previousRunSending(e, rec, first.ID)
	if prev == nil {
		return f
	}
	st, ok := prev.Step(first.ID)
	if !ok || st.Call != first.Call || !refusedSameWay(why, all, fields, st) {
		return f
	}
	req := decoded(st.Request)
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
		sent, req := []string{}, decoded(st.Request)
		for _, f := range fields {
			if v, ok := chain.Get(req, f.path); ok {
				sent = append(sent, fmt.Sprint(v))
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
	now, _ := conflictingFields(then, was)
	return refusalCodes(was, then) == refusalCodes(why, all) && fieldPaths(now) == fieldPaths(conflicting)
}

func refusalBlamesAnotherField(req any, why string, fields []fixtureField) bool {
	fixture := map[string]bool{}
	for _, f := range fields {
		fixture[f.path] = true
	}
	others := []fixtureField{}
	eachLeaf(req, "", func(path string, _ any) {
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
	named := func(set []fixtureField, shortest int) bool {
		folded := foldName(why)
		return slices.ContainsFunc(set, func(f fixtureField) bool {
			name := foldName(leafName(f.path))
			return name != "" && len(name) >= shortest && strings.Contains(folded, name)
		})
	}
	switch {
	case quotesSent(why, fields):
		return false
	case quotesSent(why, others):
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
			if st == nil || st.Call != first.Call || !answeredCleanly(st) {
				continue
			}
			if got, ok := chain.Get(decoded(st.Request), f.path); ok && fmt.Sprint(got) == f.sent && repeatAcceptedBefore(e, rec, first.ID, st.ID, f.path) {
				repeated = true
			}
		}
		if !repeated {
			return false
		}
	}
	return true
}

func erredBefore(rec *runner.Record, index int, conflicting []fixtureField) *runner.StepRecord {
	var values []string
	for _, f := range conflicting {
		if f.sent == "" {
			return nil
		}
		values = append(values, f.sent)
	}
	for _, st := range rec.Steps[:index] {
		if st != nil && st.Call == rec.Steps[index].Call && st.Transport != nil && len(values) > 0 && sendsAll(st, values) &&
			serverAttempt(&runner.Attempt{HTTPStatus: st.HTTPStatus, Code: st.Transport.Code}) {
			return st
		}
	}
	return nil
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
	return slices.ContainsFunc(fields, func(f fixtureField) bool { return f.sent != "" && strings.Contains(text, f.sent) })
}

type fixtureField struct {
	path   string
	sent   string
	vars   []string
	unique bool
}

func conflictingFields(fields []fixtureField, why string) ([]fixtureField, bool) {
	byValue, byName := []fixtureField{}, []fixtureField{}
	folded := foldName(why)
	for _, f := range fields {
		if f.sent != "" && strings.Contains(why, f.sent) {
			byValue = append(byValue, f)
		}
		if name := foldName(leafName(f.path)); name != "" && strings.Contains(folded, name) {
			byName = append(byName, f)
		}
	}
	if len(byValue) > 0 {
		return byValue, true
	}
	if len(byName) > 0 {
		return byName, true
	}
	return fields, false
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
		for other := range newestRuns(e, entry.Name()) {
			if other.Chain == rec.Chain {
				continue
			}
			for _, st := range other.Steps {
				if (answeredCleanly(st) || sentUnknown(st)) && sendsAll(st, values) {
					return other.Chain, other.RunID, !answeredCleanly(st)
				}
			}
		}
	}
	return "", "", false
}

func sentByThisChain(e *env, rec *runner.Record, names, values []string) (string, string, []string, bool) {
	if len(values) == 0 {
		return "", "", nil, false
	}
	for prev := range newestRuns(e, rec.Chain, rec.RunID) {
		prev = namedAs(prev, rec)
		for _, st := range prev.Steps {
			if !(answeredCleanly(st) || sentUnknown(st)) || !sendsAll(st, values) {
				continue
			}
			return prev.RunID, st.ID, varValues(prev.Vars, names), !answeredCleanly(st)
		}
	}
	return "", "", nil, false
}

func varValues(vars map[string]any, names []string) []string {
	var out []string
	for _, n := range names {
		if v, ok := vars[n]; ok && fmt.Sprint(v) != pathmask.MaskRedacted {
			out = append(out, fmt.Sprintf("%s=%v", n, v))
		}
	}
	return out
}

func sentUnknown(st *runner.StepRecord) bool {
	return st != nil && st.Status == runner.StatusError && st.HTTPStatus == 0 && len(st.Response) == 0 && len(st.Request) > 0 &&
		(strings.Contains(st.Error, "whether the call took effect is unknown") || strings.Contains(st.Error, transport.NoAnswerBeforeTimeout))
}

func usedBy(rec *runner.Record, step string) bool {
	st, ok := rec.Step(step)
	return ok && (answeredCleanly(st) || sentUnknown(st))
}

func sendsAll(st *runner.StepRecord, values []string) bool {
	var req any
	if json.Unmarshal(st.Request, &req) != nil {
		return false
	}
	sent := map[string]bool{}
	visitStrings(req, func(s string) { sent[s] = true })
	return !slices.ContainsFunc(values, func(v string) bool { return !sent[v] })
}

func stepRefusalText(st *runner.StepRecord) string {
	if st.Transport != nil {
		return strings.TrimSpace(st.Transport.Code + ": " + st.Transport.Message)
	}
	body := decoded(st.Response)
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

func visitStrings(v any, visit func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(t) {
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
