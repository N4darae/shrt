package diff

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

const RunComparisonNote = "this compares two recorded runs with each other; it is not a verdict against a confirmed safe spot (that is 'shrt verify')"

type StepStatus struct {
	Step   string `json:"step"`
	A      string `json:"a"`
	B      string `json:"b"`
	ErrorA string `json:"error_a,omitempty"`
	ErrorB string `json:"error_b,omitempty"`
}

type VarChange struct {
	Name    string `json:"name"`
	A       any    `json:"a"`
	B       any    `json:"b"`
	Fixture bool   `json:"fixture,omitempty"`
}

type RunReport struct {
	Note              string       `json:"note"`
	Chain             string       `json:"chain"`
	RunA              string       `json:"run_a"`
	RunB              string       `json:"run_b"`
	StatusA           string       `json:"status_a"`
	StatusB           string       `json:"status_b"`
	TargetA           string       `json:"target_a,omitempty"`
	TargetB           string       `json:"target_b,omitempty"`
	BuildA            string       `json:"build_a,omitempty"`
	BuildB            string       `json:"build_b,omitempty"`
	KeepGoingA        bool         `json:"keep_going_a,omitempty"`
	KeepGoingB        bool         `json:"keep_going_b,omitempty"`
	VarChanges        []VarChange  `json:"var_changes,omitempty"`
	FirstFailureA     string       `json:"first_failure_a,omitempty"`
	FirstFailureB     string       `json:"first_failure_b,omitempty"`
	StatusChanges     []StepStatus `json:"status_changes,omitempty"`
	NoLongerReached   []string     `json:"no_longer_reached,omitempty"`
	NewlyReached      []string     `json:"newly_reached,omitempty"`
	WhyNotReached     []StepStatus `json:"why_not_reached,omitempty"`
	ErrorChanges      []StepStatus `json:"error_changes,omitempty"`
	SkippedKeepGoing  []string     `json:"skipped_with_keep_going,omitempty"`
	RequestChanges    []Change     `json:"request_changes,omitempty"`
	FixtureRequests   int          `json:"fixture_requests,omitempty"`
	FixtureEchoed     int          `json:"fixture_echoed,omitempty"`
	Changes           []Change     `json:"changes,omitempty"`
	Masked            int          `json:"masked"`
	FullyMasked       []string     `json:"fully_masked,omitempty"`
	RenamedSteps      []StepRename `json:"renamed_steps,omitempty"`
	UnsentDefaults    []string     `json:"unsent_defaults,omitempty"`
	UndeclaredSame    []string     `json:"undeclared_same,omitempty"`
	UndeclaredUnknown []string     `json:"undeclared_uncompared,omitempty"`
	SelectorA         string       `json:"selector_a,omitempty"`
	SelectorB         string       `json:"selector_b,omitempty"`
	StartedA          time.Time    `json:"-"`
	StartedB          time.Time    `json:"-"`

	compared     []comparedStep
	idPairs      []idPair
	winA, winB   *runWindow
	fixturePairs [][2]string
}

func (r *RunReport) Same() bool {
	return r.StatusA == r.StatusB && r.FirstFailureA == r.FirstFailureB &&
		len(r.StatusChanges) == 0 && len(r.NoLongerReached) == 0 &&
		len(r.NewlyReached) == 0 && len(r.ErrorChanges) == 0 && len(r.Changes) == 0 && len(r.RequestChanges) == 0
}

func CompareRuns(a, b *runner.Record) *RunReport {
	return CompareRunsMasking(a, b, nil)
}

func reached(rec *runner.Record, s *runner.StepRecord) bool {
	return StepReached(rec, s)
}

func stepError(s *runner.StepRecord) string {
	if s == nil || (s.Status != runner.StatusError && s.Status != runner.StatusSkipped && s.Transport == nil) {
		return ""
	}
	return firstLineOf(s.Error)
}

func CompareRunsMasking(a, b *runner.Record, extra []string) *RunReport {
	return CompareRunsSkipping(a, b, extra, Fixtures{})
}

func CompareRunsSkipping(a, b *runner.Record, extra []string, fx Fixtures) *RunReport {
	rep := &RunReport{
		Note: RunComparisonNote, Chain: a.Chain,
		RunA: a.RunID, RunB: b.RunID, StatusA: a.Status, StatusB: b.Status, StartedA: a.StartedAt, StartedB: b.StartedAt,
		FirstFailureA: firstFailure(a), FirstFailureB: firstFailure(b),
		winA: recordWindow(a), winB: recordWindow(b),
	}
	if !config.SameTarget(a.Target, b.Target) {
		rep.TargetA, rep.TargetB = a.Target, b.Target
	}
	if a.Build != b.Build {
		rep.BuildA, rep.BuildB = a.Build, b.Build
	}
	if a.KeepGoing != b.KeepGoing {
		rep.KeepGoingA, rep.KeepGoingB = a.KeepGoing, b.KeepGoing
	}
	rep.VarChanges = varChanges(a.Vars, b.Vars)
	if fx.Var != nil {
		for i := range rep.VarChanges {
			rep.VarChanges[i].Fixture = fx.Var(rep.VarChanges[i].Name)
		}
	}
	if renames := StepRenames(a.Steps, b.Steps); len(renames) > 0 {
		rep.RenamedSteps = renames
		cp := *a
		cp.Steps = RenameSteps(a.Steps, renames)
		a = &cp
	}
	byID := map[string]*runner.StepRecord{}
	allB := map[string]*runner.StepRecord{}
	for _, s := range b.Steps {
		allB[s.ID] = s
		if reached(b, s) {
			byID[s.ID] = s
		}
	}
	allA := map[string]*runner.StepRecord{}
	for _, s := range a.Steps {
		allA[s.ID] = s
	}
	inA := map[string]bool{}
	base := mergePatterns(a.Volatile, b.Volatile, extra)
	for _, sa := range a.Steps {
		if !reached(a, sa) {
			continue
		}
		inA[sa.ID] = true
		sb, ok := byID[sa.ID]
		if !ok {
			rep.NoLongerReached = append(rep.NoLongerReached, sa.ID)
			if why := stepError(allB[sa.ID]); why != "" {
				rep.WhyNotReached = append(rep.WhyNotReached, StepStatus{Step: sa.ID, A: sa.Status, B: allB[sa.ID].Status, ErrorB: why})
			}
			continue
		}
		if sa.Status != sb.Status {
			rep.StatusChanges = append(rep.StatusChanges, StepStatus{Step: sa.ID, A: sa.Status, B: sb.Status,
				ErrorA: stepError(sa), ErrorB: stepError(sb)})
		}
		masker := pathmask.NewMasker(mergePatterns(base, sa.Volatile, sb.Volatile))
		rep.compareRequests(sa, sb, masker, fx)
		rep.compareResponses(sa, sb, masker)
		if everyFieldMasked(masker, sa.Response) && everyFieldMasked(masker, sb.Response) {
			rep.FullyMasked = append(rep.FullyMasked, sa.ID)
		}
	}
	renames := idRenames(rep.idPairs)
	var renamed, echoed []Change
	rep.Changes, renamed = splitEchoes(rep.Changes, rep.compared, renames)
	rep.Masked += len(renamed)
	var stale []Change
	rep.Changes, echoed, stale = splitStaleEchoes(rep.Changes, rep.compared, append(rep.fixturePairs, renames...))
	rep.FixtureEchoed += len(echoed)
	rep.Changes = append(rep.Changes, stale...)
	for _, sa := range a.Steps {
		sb := allB[sa.ID]
		if sb == nil || sa.Status != runner.StatusError || sb.Status != runner.StatusError || reached(a, sa) || reached(b, sb) {
			continue
		}
		if ea, eb := firstLineOf(sa.Error), firstLineOf(sb.Error); ea != eb {
			rep.ErrorChanges = append(rep.ErrorChanges, StepStatus{Step: sa.ID, A: sa.Status, B: sb.Status, ErrorA: ea, ErrorB: eb})
		}
	}
	if a.KeepGoing != b.KeepGoing {
		kept, other := a, b
		if b.KeepGoing {
			kept, other = b, a
		}
		otherSteps := map[string]*runner.StepRecord{}
		for _, s := range other.Steps {
			otherSteps[s.ID] = s
		}
		for _, s := range kept.Steps {
			if o := otherSteps[s.ID]; s.Status == runner.StatusSkipped && (o == nil || !reached(other, o)) {
				rep.SkippedKeepGoing = append(rep.SkippedKeepGoing, s.ID)
			}
		}
	}
	for _, sb := range b.Steps {
		if reached(b, sb) && !inA[sb.ID] {
			rep.NewlyReached = append(rep.NewlyReached, sb.ID)
			if why := stepError(allA[sb.ID]); why != "" {
				rep.WhyNotReached = append(rep.WhyNotReached, StepStatus{Step: sb.ID, A: allA[sb.ID].Status, B: sb.Status, ErrorA: why})
			}
		}
	}
	return rep
}

func varChanges(a, b map[string]any) []VarChange {
	names := map[string]bool{}
	for k := range a {
		names[k] = true
	}
	for k := range b {
		names[k] = true
	}
	sorted := make([]string, 0, len(names))
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	out := []VarChange{}
	for _, k := range sorted {
		va, inA := a[k]
		vb, inB := b[k]
		if inA && inB && fmt.Sprint(va) == fmt.Sprint(vb) {
			continue
		}
		out = append(out, VarChange{Name: k, A: va, B: vb})
	}
	return out
}

func (r *RunReport) compareResponses(sa, sb *runner.StepRecord, masker *pathmask.Masker) {
	x, errA := decode(sa.Response)
	y, errB := decode(sb.Response)
	if errA != nil || errB != nil {
		if string(sa.Response) != string(sb.Response) {
			r.Changes = append(r.Changes, Change{Step: sa.ID, Path: "response", Kind: KindType, Want: string(sa.Response), Got: string(sb.Response)})
		}
		return
	}
	collectIDPairs(sa.ID, x, y, "", masker, &r.idPairs)
	r.compared = append(r.compared, comparedStep{id: sa.ID, want: x, got: y, mask: masker})
	walk(x, y, "", func(c Change) {
		shaped, why := false, ""
		if c.Kind == KindChanged {
			shaped, why = volatileIn(c.Path, c.Want, c.Got, r.winA, r.winB)
		}
		if underMask(masker, c.Path) || shaped {
			r.Masked++
			return
		}
		if why != "" {
			c.Detail = why
		}
		noteTimeUnit(&c)
		c.Step = sa.ID
		r.Changes = append(r.Changes, c)
	})
}

func (r *RunReport) compareRequests(sa, sb *runner.StepRecord, masker *pathmask.Masker, fx Fixtures) {
	if !SameCall(sa, sb) {
		r.RequestChanges = append(r.RequestChanges, Change{Step: sa.ID, Path: "call", Kind: KindChanged, Want: sa.Call, Got: sb.Call})
	}
	if sa.AuthProfile != "" && sb.AuthProfile != "" && sa.AuthProfile != sb.AuthProfile {
		r.RequestChanges = append(r.RequestChanges, Change{Step: sa.ID, Path: AuthProfilePath, Kind: KindChanged, Want: sa.AuthProfile, Got: sb.AuthProfile})
	}
	for _, c := range headerChanges(sa, sb) {
		if fx.Named != nil && fx.Named(sa.ID, c.Path) {
			r.FixtureRequests++
			continue
		}
		r.RequestChanges = append(r.RequestChanges, c)
	}
	if len(sa.Request) == 0 || len(sb.Request) == 0 {
		return
	}
	a, errA := decode(sa.Request)
	b, errB := decode(sb.Request)
	if errA != nil || errB != nil {
		return
	}
	r.fixturePairs = append(r.fixturePairs, generatedPairs([]*runner.StepRecord{sa}, []*runner.StepRecord{sb}, fx.Generated)...)
	rn := renamer(r.fixturePairs)
	walk(a, b, "", func(c Change) {
		fixture := (fx.Named != nil && fx.Named(sa.ID, c.Path)) || (fx.Generated != nil && fx.Generated(sa.ID, c.Path))
		if fixture {
			if x, ok := c.Want.(string); ok && len(x) >= minFixtureEcho {
				if y, ok := c.Got.(string); ok {
					r.fixturePairs = append(r.fixturePairs, [2]string{x, y})
				}
			}
		}
		if maskedAt(masker, c) || (c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got)) {
			r.Masked++
			return
		}
		if fixture {
			r.FixtureRequests++
			return
		}
		if w, okW := c.Want.(string); okW && rn != nil && c.Kind == KindChanged {
			if g, okG := c.Got.(string); okG && rn.Replace(w) == g {
				r.FixtureRequests++
				return
			}
		}
		c.Step = sa.ID
		r.RequestChanges = append(r.RequestChanges, c)
	})
}

func everyFieldMasked(m *pathmask.Masker, raw []byte) bool {
	body, err := decode(raw)
	if err != nil || body == nil {
		return false
	}
	masked, open := 0, 0
	var count func(v any)
	count = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, item := range t {
				count(item)
			}
		case []any:
			for _, item := range t {
				count(item)
			}
		case string:
			if t == pathmask.MaskVolatile {
				masked++
				return
			}
			open++
		default:
			open++
		}
	}
	count(m.Apply(body))
	return masked > 0 && open == 0
}

func firstFailure(rec *runner.Record) string {
	for _, s := range rec.Steps {
		if s.Status == runner.StatusFailed || s.Status == runner.StatusError {
			return s.ID
		}
	}
	return ""
}

func underMask(m *pathmask.Masker, path string) bool {
	segs := strings.Split(path, ".")
	for i := len(segs); i > 0; i-- {
		if m.Masks(strings.Join(segs[:i], ".")) {
			return true
		}
	}
	return false
}

var (
	uuidShape  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	digitsOnly = regexp.MustCompile(`^[0-9]+$`)
)

func LooksVolatile(path string, a, b any) bool {
	return looksVolatile(path, a, b)
}

func looksVolatile(path string, a, b any) bool {
	if timeMismatch(path, a, b, nil, nil) != "" {
		return false
	}
	key := lastKey(path)
	lower := strings.ToLower(key)
	switch {
	case lower == "id", lower == "ids", lower == "token", lower == "access_token", lower == "idempotency_key",
		strings.HasSuffix(lower, "_id"), strings.HasSuffix(lower, "_ids"), strings.HasPrefix(lower, "id_"),
		strings.HasSuffix(lower, "_at"), strings.HasSuffix(lower, "_time"), strings.Contains(lower, "timestamp"),
		camelSuffix(key, "Id"), camelSuffix(key, "Ids"), camelSuffix(key, "At"), camelSuffix(key, "Time"),
		camelIDPrefix(key):
		return sameShape(a, b)
	}
	return bothAre(a, b, isTimestamp) || bothAre(a, b, uuidShape.MatchString)
}

var alnumRun = regexp.MustCompile(`[A-Za-z0-9]+`)

func sameShape(a, b any) bool {
	if x, ok := a.(float64); ok {
		y, ok := b.(float64)
		return ok && x != 0 && y != 0
	}
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	if !ok1 || !ok2 || x == "" || y == "" || zeroID(x) != zeroID(y) {
		return false
	}
	if bothAre(a, b, isTimestamp) || bothAre(a, b, uuidShape.MatchString) {
		return true
	}
	hasDigit := func(s string) bool { return strings.ContainsAny(s, "0123456789") }
	return alnumRun.ReplaceAllString(x, "x") == alnumRun.ReplaceAllString(y, "x") && hasDigit(x) == hasDigit(y) &&
		kindPrefix(x) == kindPrefix(y)
}

var letterPrefix = regexp.MustCompile(`^([A-Za-z]+)[^A-Za-z0-9]`)

func kindPrefix(s string) string {
	if m := letterPrefix.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func lastKey(path string) string {
	segs := strings.Split(path, ".")
	for i := len(segs) - 1; i >= 0; i-- {
		if !digitsOnly.MatchString(segs[i]) {
			return segs[i]
		}
	}
	return ""
}

func camelIDPrefix(key string) bool {
	return len(key) > 2 && key[:2] == "id" && key[2] >= 'A' && key[2] <= 'Z'
}

func camelSuffix(key, suffix string) bool {
	if len(key) <= len(suffix) || !strings.HasSuffix(key, suffix) {
		return false
	}
	prev := key[len(key)-len(suffix)-1]
	return prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
}

func bothAre(a, b any, pred func(string) bool) bool {
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	return ok1 && ok2 && pred(x) && pred(y)
}

func isTimestamp(s string) bool {
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

func (r *RunReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff of %s: %s vs %s%s\n", r.Chain, runLabel("A", r.SelectorA, r.RunA, r.StatusA),
		runLabel("B", r.SelectorB, r.RunB, r.StatusB), r.recordedOrder())
	fmt.Fprintf(&b, "%s\n", r.Note)
	if line := RenamedLine(r.RenamedSteps, "run A", "run B"); line != "" {
		fmt.Fprintf(&b, "\n%s\n", line)
	}
	if len(r.FullyMasked) > 0 {
		fmt.Fprintf(&b, "\nWARNING: every response field of step(s) %s is under a volatile pattern, so this diff compared nothing "+
			"of those responses and \"no differences\" says nothing about them. Narrow the volatile patterns (a bare \"**\" masks everything)\n",
			strings.Join(r.FullyMasked, ", "))
	}
	if r.TargetA != "" || r.TargetB != "" {
		fmt.Fprintf(&b, "\ntargets differ: A %s, B %s\n", r.TargetA, r.TargetB)
	}
	if r.BuildA != "" || r.BuildB != "" {
		fmt.Fprintf(&b, "\nbuilds differ: A %s, B %s\n", orUnset(r.BuildA), orUnset(r.BuildB))
	}
	if r.KeepGoingA != r.KeepGoingB {
		with, without, red, only := "B", "A", r.FirstFailureA, r.NewlyReached
		if r.KeepGoingA {
			with, without, red, only = "A", "B", r.FirstFailureB, r.NoLongerReached
		}
		if red != "" {
			fmt.Fprintf(&b, "\nrun %s used -keep-going and run %s did not, so %s went on past %s's first red (%s)", with, without, with, without, red)
			if len(only) > 0 {
				fmt.Fprintf(&b, "; %d step(s) reached in %s only, listed below", len(only), with)
			}
			if len(r.SkippedKeepGoing) > 0 {
				fmt.Fprintf(&b, "; skipped in %s as well, and a skipped step is not reached: %s", with, strings.Join(r.SkippedKeepGoing, ", "))
			}
			if len(only) == 0 && len(r.SkippedKeepGoing) == 0 {
				fmt.Fprintf(&b, ", yet reached no step %s did not", without)
			}
			b.WriteString("\n")
		}
	}
	if len(r.VarChanges) > 0 {
		parts, fixtures := []string{}, []string{}
		for _, v := range r.VarChanges {
			part := fmt.Sprintf("%s a=%v b=%v", v.Name, orAbsent(v.A), orAbsent(v.B))
			if v.Fixture {
				fixtures = append(fixtures, part)
			} else {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "\nthe runs used different vars, so a difference may come from the input rather than the backend: %s\n", strings.Join(parts, "; "))
		}
		if len(fixtures) > 0 {
			fmt.Fprintf(&b, "\nfixture vars differ, echoes masked: %s\n", strings.Join(fixtures, "; "))
		}
	}
	if r.FirstFailureA != r.FirstFailureB {
		fmt.Fprintf(&b, "\nfirst failing step moved: A %s, B %s\n", orNone(r.FirstFailureA), orNone(r.FirstFailureB))
	} else if r.FirstFailureA != "" {
		fmt.Fprintf(&b, "\nfirst failing step unchanged: %s\n", r.FirstFailureA)
	}
	if len(r.StatusChanges) > 0 {
		b.WriteString("\nstep status changes (A -> B):\n")
		for _, s := range r.StatusChanges {
			fmt.Fprintf(&b, "  %s  %s -> %s%s\n", s.Step, s.A, s.B, s.errors())
		}
	}
	timedOutA, timedOutB := map[string]string{}, map[string]string{}
	for _, s := range r.WhyNotReached {
		if how := sentWithoutAnswer(s.ErrorA); how != "" {
			timedOutA[s.Step] = how
		}
		if how := sentWithoutAnswer(s.ErrorB); how != "" {
			timedOutB[s.Step] = how
		}
	}
	unreachedLines := func(ids []string, timedOut map[string]string, reachedIn, other string) {
		sent, dropped, unreached := []string{}, []string{}, []string{}
		for _, id := range ids {
			switch timedOut[id] {
			case transport.NoAnswerBeforeTimeout:
				sent = append(sent, id)
			case transport.ClosedAfterSending:
				dropped = append(dropped, id)
			default:
				unreached = append(unreached, id)
			}
		}
		if len(unreached) > 0 {
			fmt.Fprintf(&b, "\nreached in %s, not reached in %s: %s\n", reachedIn, other, strings.Join(unreached, ", "))
		}
		if len(sent) > 0 {
			fmt.Fprintf(&b, "\nanswered in %s; sent in %s, no answer before target.timeout: %s\n", reachedIn, other, strings.Join(sent, ", "))
		}
		if len(dropped) > 0 {
			fmt.Fprintf(&b, "\nanswered in %s; sent in %s, no answer (the connection closed): %s\n", reachedIn, other, strings.Join(dropped, ", "))
		}
	}
	unreachedLines(r.NoLongerReached, timedOutB, "A", "B")
	unreachedLines(r.NewlyReached, timedOutA, "B", "A")
	skipsA, skipsB := runner.NewSkipCondenser(), runner.NewSkipCondenser()
	for _, s := range r.WhyNotReached {
		s.ErrorA, s.ErrorB = skipsA.Condense(s.Step, s.ErrorA), skipsB.Condense(s.Step, s.ErrorB)
		fmt.Fprintf(&b, "  %s  %s -> %s%s\n", s.Step, s.A, s.B, s.errors())
	}
	if len(r.ErrorChanges) > 0 {
		b.WriteString("\nsteps that errored in both runs, for different reasons (A -> B):\n")
		for _, s := range r.ErrorChanges {
			fmt.Fprintf(&b, "  %s  %s -> %s%s\n", s.Step, s.A, s.B, s.errors())
		}
	}
	if len(r.RequestChanges) > 0 {
		fmt.Fprintf(&b, "\n%d request difference(s), what the two runs SENT, in steps both reached (a = run A, b = run B):\n", len(r.RequestChanges))
		for _, c := range r.RequestChanges {
			fmt.Fprintf(&b, "  [%s] %-10s %s %s\n", c.Step, c.Kind, c.Path, c.describeRuns())
		}
	}
	if r.FixtureRequests > 0 {
		fmt.Fprintf(&b, "\n%d request value(s) differ only in a fixture name built from a var inside other text (`sku-${vars.tag}`) or from `${uuid}` or the clock, not shown\n", r.FixtureRequests)
	}
	if len(r.Changes) > 0 {
		fmt.Fprintf(&b, "\n%d response difference(s) in steps both runs reached (a = run A, b = run B):\n", len(r.Changes))
		for _, c := range r.Changes {
			detail := ""
			if c.Detail != "" {
				detail = " (" + c.Detail + ")"
			}
			fmt.Fprintf(&b, "  [%s] %-10s %s %s%s\n", c.Step, c.Kind, c.Path, c.describeRuns(), detail)
		}
	}
	if len(r.UnsentDefaults) > 0 {
		fmt.Fprintf(&b, "\n%d response field(s) are declared in one run's record and absent from the other's, where they were not on "+
			"the wire (left at the proto3 default, the same bytes), so they are not shown, as `shrt verify` does not count them: %s\n",
			len(r.UnsentDefaults), strings.Join(r.UnsentDefaults, ", "))
	}
	if len(r.UndeclaredSame) > 0 {
		fmt.Fprintf(&b, "\n%d response field(s) are declared in one run's record and were on the wire, undeclared, in the other's with "+
			"the same value, so they are not shown: %s\n", len(r.UndeclaredSame), strings.Join(r.UndeclaredSame, ", "))
	}
	if len(r.UndeclaredUnknown) > 0 {
		fmt.Fprintf(&b, "\n%d response field(s) are declared in one run's record and were on the wire, undeclared, in the other's, whose "+
			"build did not record their value, so they were not compared: %s\n", len(r.UndeclaredUnknown), strings.Join(r.UndeclaredUnknown, ", "))
	}
	if r.FixtureEchoed > 0 {
		fmt.Fprintf(&b, "\n%d response value(s) differ only by echoing the fixture name the run sent, as `shrt verify` masks them, not shown\n", r.FixtureEchoed)
	}
	if r.Masked > 0 {
		fmt.Fprintf(&b, "\n%d differing value(s) not shown: declared volatile paths, ids and timestamps differ every run\n", r.Masked)
	}
	if r.Same() {
		b.WriteString("\nno differences between the two runs\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func runLabel(side, selector, id, status string) string {
	if selector != "" && selector != id {
		return fmt.Sprintf("run %s = %s (%s, %s)", side, selector, id, status)
	}
	return fmt.Sprintf("run %s %s (%s)", side, id, status)
}

func (r *RunReport) recordedOrder() string {
	switch {
	case r.StartedA.IsZero() || r.StartedB.IsZero():
		return ""
	case r.StartedA.After(r.StartedB):
		return "; A was recorded after B, so b= is the older value"
	case r.StartedB.After(r.StartedA):
		return "; A was recorded before B, so b= is the newer value"
	}
	return ""
}

func (s StepStatus) errors() string {
	out := ""
	if s.ErrorA != "" {
		out += "\n      A: " + s.ErrorA
	}
	if s.ErrorB != "" {
		out += "\n      B: " + s.ErrorB
	}
	return out
}

func (c Change) describeRuns() string {
	if c.Kind == KindLength {
		return fmt.Sprintf("a=%v item(s) b=%v item(s)", c.Want, c.Got)
	}
	if c.Kind != KindType {
		return fmt.Sprintf("a=%v b=%v", c.Want, c.Got)
	}
	return fmt.Sprintf("a=%s b=%s", withKind(c.Want), withKind(c.Got))
}

func orUnset(s string) string {
	if s == "" {
		return "(no build recorded)"
	}
	return s
}

func orAbsent(v any) any {
	if v == nil {
		return "(unset)"
	}
	return v
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func sentWithoutAnswer(msg string) string {
	switch {
	case strings.HasPrefix(msg, "not sent"):
		return ""
	case strings.Contains(msg, transport.NoAnswerBeforeTimeout):
		return transport.NoAnswerBeforeTimeout
	case strings.Contains(msg, transport.ClosedAfterSending):
		return transport.ClosedAfterSending
	}
	return ""
}
