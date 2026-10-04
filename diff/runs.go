package diff

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

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
	FailingA          []string     `json:"failing_a,omitempty"`
	FailingB          []string     `json:"failing_b,omitempty"`
	FailingAlike      bool         `json:"failing_alike,omitempty"`
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
	MaskedChanges     []Change     `json:"masked_changes,omitempty"`
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
	varsA, varsB map[string]any
	fixturePairs [][2]string
}

func (r *RunReport) Same() bool {
	return r.StatusA == r.StatusB && r.FirstFailureA == r.FirstFailureB &&
		len(r.StatusChanges) == 0 && len(r.NoLongerReached) == 0 &&
		len(r.NewlyReached) == 0 && len(r.ErrorChanges) == 0 && len(r.Changes) == 0 && len(r.RequestChanges) == 0
}

func stepError(s *runner.StepRecord) string {
	if s == nil || (s.Status != runner.StatusError && s.Status != runner.StatusSkipped && s.Transport == nil) {
		return ""
	}
	return runner.FirstLine(s.Error)
}

func CompareRunsSkipping(a, b *runner.Record, extra []string, fx Fixtures) *RunReport {
	rep := &RunReport{
		Chain: a.Chain,
		RunA:  a.RunID, RunB: b.RunID, StatusA: a.Status, StatusB: b.Status, StartedA: a.StartedAt, StartedB: b.StartedAt,
		FirstFailureA: firstFailure(a), FirstFailureB: firstFailure(b),
		winA: recordWindow(a), winB: recordWindow(b), varsA: a.Vars, varsB: b.Vars,
	}
	if rep.FirstFailureA != "" && rep.FirstFailureA == rep.FirstFailureB {
		sa, _ := a.Step(rep.FirstFailureA)
		sb, _ := b.Step(rep.FirstFailureB)
		rep.FailingA, rep.FailingB = failingLines(sa), failingLines(sb)
		rep.FailingAlike = len(rep.FailingA) == len(rep.FailingB)
		for i := range rep.FailingA {
			if rep.FailingAlike && MaskVarValues(a.Vars, rep.FailingA[i]) != MaskVarValues(b.Vars, rep.FailingB[i]) {
				rep.FailingAlike = false
			}
		}
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
		if StepReached(b, s) {
			byID[s.ID] = s
		}
	}
	allA := map[string]*runner.StepRecord{}
	for _, s := range a.Steps {
		allA[s.ID] = s
	}
	inA := map[string]bool{}
	base := slices.Concat(a.Volatile, b.Volatile, extra)
	for _, sa := range a.Steps {
		if !StepReached(a, sa) {
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
		masker := pathmask.NewMasker(slices.Concat(base, sa.Volatile, sb.Volatile))
		rep.compareRequests(sa, sb, masker, fx)
		if x, y := rep.compareResponses(sa, sb, masker); everyFieldMasked(masker, x) && everyFieldMasked(masker, y) {
			rep.FullyMasked = append(rep.FullyMasked, sa.ID)
		}
	}
	renames := idRenames(rep.idPairs)
	if rn := renamer(renames); rn != nil && !rep.FailingAlike && rep.FirstFailureA != "" && rep.FirstFailureA == rep.FirstFailureB && len(rep.FailingA) == len(rep.FailingB) {
		rep.FailingAlike = true
		for i := range rep.FailingA {
			if MaskVarValues(a.Vars, rn.Replace(rep.FailingA[i])) != MaskVarValues(b.Vars, rep.FailingB[i]) {
				rep.FailingAlike = false
			}
		}
	}
	var renamed, echoed []Change
	rep.Changes, renamed, _ = splitStaleEchoes(rep.Changes, rep.compared, renames)
	rep.Masked += len(renamed)
	rep.MaskedChanges = append(rep.MaskedChanges, withMask(renamed, renamedMask)...)
	var stale []Change
	rep.Changes, echoed, stale = splitStaleEchoes(rep.Changes, rep.compared, append(rep.fixturePairs, renames...))
	rep.FixtureEchoed += len(echoed)
	rep.MaskedChanges = append(rep.MaskedChanges, withMask(echoed, fixtureMask)...)
	rep.Changes = append(rep.Changes, stale...)
	for _, sa := range a.Steps {
		sb := allB[sa.ID]
		if sb == nil || sa.Status != runner.StatusError || sb.Status != runner.StatusError || StepReached(a, sa) || StepReached(b, sb) {
			continue
		}
		if ea, eb := runner.FirstLine(sa.Error), runner.FirstLine(sb.Error); ea != eb {
			rep.ErrorChanges = append(rep.ErrorChanges, StepStatus{Step: sa.ID, A: sa.Status, B: sb.Status, ErrorA: ea, ErrorB: eb})
		}
	}
	if a.KeepGoing != b.KeepGoing {
		kept, other, otherSteps := a, b, allB
		if b.KeepGoing {
			kept, other, otherSteps = b, a, allA
		}
		for _, s := range kept.Steps {
			if o := otherSteps[s.ID]; s.Status == runner.StatusSkipped && (o == nil || !StepReached(other, o)) {
				rep.SkippedKeepGoing = append(rep.SkippedKeepGoing, s.ID)
			}
		}
	}
	for _, sb := range b.Steps {
		if StepReached(b, sb) && !inA[sb.ID] {
			rep.NewlyReached = append(rep.NewlyReached, sb.ID)
			if why := stepError(allA[sb.ID]); why != "" {
				rep.WhyNotReached = append(rep.WhyNotReached, StepStatus{Step: sb.ID, A: allA[sb.ID].Status, B: sb.Status, ErrorA: why})
			}
		}
	}
	return rep
}

func varChanges(a, b map[string]any) []VarChange {
	out := []VarChange{}
	for _, k := range sortedKeys(a, b) {
		va, inA := a[k]
		vb, inB := b[k]
		if inA && inB && fmt.Sprint(va) == fmt.Sprint(vb) {
			continue
		}
		out = append(out, VarChange{Name: k, A: va, B: vb})
	}
	return out
}

func (r *RunReport) compareResponses(sa, sb *runner.StepRecord, masker *pathmask.Masker) (any, any) {
	x, errA := decode(sa.Response)
	y, errB := decode(sb.Response)
	if errA != nil || errB != nil {
		if string(sa.Response) != string(sb.Response) {
			r.Changes = append(r.Changes, Change{Step: sa.ID, Path: "response", Kind: KindType, Want: string(sa.Response), Got: string(sb.Response)})
		}
		return x, y
	}
	collectIDPairs(sa.ID, x, y, masker, &r.idPairs)
	r.compared = append(r.compared, comparedStep{id: sa.ID, want: x, got: y, mask: masker})
	walk(x, y, "", func(c Change) {
		shaped, why := false, ""
		if c.Kind == KindChanged {
			shaped, why = volatileIn(c.Path, c.Want, c.Got, r.winA, r.winB)
		}
		gone := valueVanished(c)
		volatile := underMask(masker, c.Path) && !(gone && masker.Masks(c.Path))
		if volatile && failedLength(sb, c) {
			volatile, c.Detail = false, VolatileFailed
		}
		if volatile || (shaped && !gone) {
			r.Masked++
			c.Step = sa.ID
			c.Mask = shapeMask
			if volatile {
				c.Mask = hidingPattern(masker, c.Path, true)
			}
			r.MaskedChanges = append(r.MaskedChanges, c)
			return
		}
		if why != "" {
			c.Detail = why
		}
		if c.Detail == "" && gone && masker.Masks(c.Path) {
			c.Detail = vanishedDetail(masker, c, "run A", "run B")
		}
		noteTimeUnit(&c)
		c.Step = sa.ID
		r.Changes = append(r.Changes, c)
	})
	return x, y
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
	r.fixturePairs = append(r.fixturePairs, generatedPairs([]*runner.StepRecord{sa}, []*runner.StepRecord{sb}, fx.Generated)...)
	rn := renamer(r.fixturePairs)
	walkRequests(sa, sb, func(c Change) {
		fixture := (fx.Named != nil && fx.Named(sa.ID, c.Path)) || (fx.Generated != nil && fx.Generated(sa.ID, c.Path))
		if p, ok := echoPair(c); fixture && ok {
			r.fixturePairs = append(r.fixturePairs, p)
		}
		if volatile := maskedAt(masker, c); volatile || (c.Kind == KindChanged && LooksVolatile(c.Path, c.Want, c.Got)) {
			r.Masked++
			hidden := c
			hidden.Step, hidden.Path, hidden.Mask = sa.ID, "request."+c.Path, shapeMask
			if volatile {
				hidden.Mask = maskOf(masker, c)
			}
			r.MaskedChanges = append(r.MaskedChanges, hidden)
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

func everyFieldMasked(m *pathmask.Masker, body any) bool {
	if body == nil {
		return false
	}
	masked, open := 0, 0
	visitScalars(m.Apply(body), "", func(_ string, v any) {
		if v == pathmask.MaskVolatile {
			masked++
		} else {
			open++
		}
	})
	return masked > 0 && open == 0
}

func firstFailure(rec *runner.Record) string {
	if s := firstRed(rec); s != nil {
		return s.ID
	}
	return ""
}

func underMask(m *pathmask.Masker, path string) bool {
	_, ok := m.HidingPattern(path, true)
	return ok
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHex(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isAlnum(c byte) bool { return isDigit(c) || isLetter(c) }

func allOf(s string, class func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !class(s[i]) {
			return false
		}
	}
	return s != ""
}

func digitsOnly(s string) bool { return allOf(s, isDigit) }

func hexRun(s string) bool { return len(s) >= 8 && allOf(s, isHex) }

func uuidShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if dash := i == 8 || i == 13 || i == 18 || i == 23; dash != (s[i] == '-') || !dash && !isHex(s[i]) {
			return false
		}
	}
	return true
}

func alnumRuns(s string) []string {
	var runs []string
	for i := 0; i < len(s); {
		if !isAlnum(s[i]) {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && isAlnum(s[j]) {
			j++
		}
		runs = append(runs, s[i:j])
		i = j
	}
	return runs
}

func runShape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if !isAlnum(s[i]) {
			b.WriteByte(s[i])
		} else if i == 0 || !isAlnum(s[i-1]) {
			b.WriteByte('x')
		}
	}
	return b.String()
}

func LooksVolatile(path string, a, b any) bool {
	if timeMismatch(path, a, b, nil, nil) != "" {
		return false
	}
	key := lastKey(path)
	if lower := strings.ToLower(key); namecase.IDNamed(key) || lower == "token" || lower == "access_token" || timeNamed(path) {
		return sameShape(a, b)
	}
	return bothAre(a, b, isTimestamp) || bothAre(a, b, uuidShape)
}

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
	if bothAre(a, b, isTimestamp) || bothAre(a, b, uuidShape) {
		return true
	}
	return runShape(x) == runShape(y) && sameRunClasses(x, y) && kindPrefix(x) == kindPrefix(y)
}

func sameRunClasses(x, y string) bool {
	xs, ys := alnumRuns(x), alnumRuns(y)
	if len(xs) != len(ys) {
		return false
	}
	hasDigit := func(s string) bool { return strings.ContainsAny(s, "0123456789") }
	for i := range xs {
		if len(xs[i]) == len(ys[i]) && hexRun(xs[i]) && hexRun(ys[i]) {
			continue
		}
		if hasDigit(xs[i]) != hasDigit(ys[i]) {
			return false
		}
	}
	return true
}

func kindPrefix(s string) string {
	i := 0
	for i < len(s) && isLetter(s[i]) {
		i++
	}
	if i == 0 || i == len(s) || isDigit(s[i]) {
		return ""
	}
	return s[:i]
}

func lastKey(path string) string {
	for {
		i := strings.LastIndexByte(path, '.')
		if seg := path[i+1:]; !digitsOnly(seg) {
			return seg
		}
		if i < 0 {
			return ""
		}
		path = path[:i]
	}
}

func bothAre(a, b any, pred func(string) bool) bool {
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	return ok1 && ok2 && pred(x) && pred(y)
}

func isTimestamp(s string) bool {
	if len(s) < len("2006-01-02T1:04:05Z") || s[4] != '-' {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

func (r *RunReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff of %s: %s vs %s%s\n", r.Chain, runLabel("A", r.SelectorA, r.RunA, r.StatusA),
		runLabel("B", r.SelectorB, r.RunB, r.StatusB), r.recordedOrder())
	if line := RenamedLine(r.RenamedSteps, "run A", "run B"); line != "" {
		fmt.Fprintf(&b, "%s\n", line)
	}
	if line := fullyMaskedLine(r.FullyMasked, "this diff", "no differences"); line != "" {
		fmt.Fprintf(&b, "%s\n", line)
	}
	if r.TargetA != "" || r.TargetB != "" {
		fmt.Fprintf(&b, "targets differ: A %s, B %s\n", r.TargetA, r.TargetB)
	}
	if r.BuildA != "" || r.BuildB != "" {
		fmt.Fprintf(&b, "builds differ: A %s, B %s\n", cmp.Or(r.BuildA, "(no build recorded)"), cmp.Or(r.BuildB, "(no build recorded)"))
	}
	if r.KeepGoingA != r.KeepGoingB {
		with, without, red, only := "B", "A", r.FirstFailureA, r.NewlyReached
		if r.KeepGoingA {
			with, without, red, only = "A", "B", r.FirstFailureB, r.NoLongerReached
		}
		if red != "" {
			fmt.Fprintf(&b, "run %s used -keep-going and run %s did not, so %s went on past %s's first red (%s)", with, without, with, without, red)
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
	if parts := r.varChanges(false); len(parts) > 0 {
		fmt.Fprintf(&b, "the runs used different vars, so a difference may come from the input rather than the backend: %s\n", strings.Join(parts, "; "))
	}
	if r.FirstFailureA != r.FirstFailureB {
		fmt.Fprintf(&b, "first failing step moved: A %s, B %s\n", cmp.Or(r.FirstFailureA, "none"), cmp.Or(r.FirstFailureB, "none"))
	} else if r.FirstFailureA != "" {
		how := ""
		switch {
		case len(r.FailingA) == 0 && len(r.FailingB) == 0:
		case r.FailingAlike && strings.Join(r.FailingA, "\n") == strings.Join(r.FailingB, "\n"):
			how = ", failing the same way in both"
		case r.FailingAlike:
			how = ", failing the same way in both: the values differ only by the ids and fixture names each run generated"
		default:
			how = ", failing differently"
		}
		fmt.Fprintf(&b, "first failing step unchanged: %s%s\n", r.FirstFailureA, how)
		for _, line := range r.FailingA {
			fmt.Fprintf(&b, "  A: %s\n", line)
		}
		for _, line := range r.FailingB {
			fmt.Fprintf(&b, "  B: %s\n", line)
		}
	}
	if len(r.StatusChanges) > 0 {
		b.WriteString("step status changes (A -> B):\n")
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
			fmt.Fprintf(&b, "reached in %s, not reached in %s: %s\n", reachedIn, other, strings.Join(unreached, ", "))
		}
		if len(sent) > 0 {
			fmt.Fprintf(&b, "answered in %s; sent in %s, no answer before target.timeout: %s\n", reachedIn, other, strings.Join(sent, ", "))
		}
		if len(dropped) > 0 {
			fmt.Fprintf(&b, "answered in %s; sent in %s, no answer (the connection closed): %s\n", reachedIn, other, strings.Join(dropped, ", "))
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
		b.WriteString("steps that errored in both runs, for different reasons (A -> B):\n")
		for _, s := range r.ErrorChanges {
			fmt.Fprintf(&b, "  %s  %s -> %s%s\n", s.Step, s.A, s.B, s.errors())
		}
	}
	if len(r.RequestChanges) > 0 {
		fmt.Fprintf(&b, "%d request difference(s), what the two runs SENT, in steps both reached:\n", len(r.RequestChanges))
		for _, c := range r.RequestChanges {
			fmt.Fprintf(&b, "  [%s] %-10s %s %s\n", c.Step, c.Kind, c.Path, c.DescribeRuns())
		}
	}
	if len(r.Changes) > 0 {
		fmt.Fprintf(&b, "%d response difference(s) in steps both runs reached:\n", len(r.Changes))
		for _, c := range r.Changes {
			detail := ""
			if c.Detail != "" {
				detail = " (" + c.Detail + ")"
			}
			fmt.Fprintf(&b, "  [%s] %-10s %s %s%s\n", c.Step, c.Kind, c.Path, r.describe(c), detail)
		}
	}
	if len(r.UndeclaredUnknown) > 0 {
		fmt.Fprintf(&b, "%d response field(s) are declared in one run's record and were on the wire, undeclared, in the other's, whose "+
			"build did not record their value, so they were not compared: %s\n", len(r.UndeclaredUnknown), strings.Join(r.UndeclaredUnknown, ", "))
	}
	if r.Same() {
		b.WriteString("no differences between the two runs\n")
	}
	if n := r.Masked + r.FixtureEchoed + r.FixtureRequests + len(r.UnsentDefaults) + len(r.UndeclaredSame); n > 0 {
		fmt.Fprintf(&b, "not counted: %d (-masked lists them)\n", n)
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
		return "; b= is the older value"
	case r.StartedB.After(r.StartedA):
		return "; b= is the newer value"
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

func (c Change) DescribeRuns() string {
	if c.Kind == KindLength {
		return fmt.Sprintf("a=%v item(s) b=%v item(s)", c.Want, c.Got)
	}
	if c.Kind != KindType {
		return fmt.Sprintf("a=%v b=%v", c.Want, c.Got)
	}
	return fmt.Sprintf("a=%s b=%s", withKind(c.Want), withKind(c.Got))
}

func orAbsent(v any) any {
	if v == nil {
		return "(unset)"
	}
	return v
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

func failingLines(s *runner.StepRecord) []string {
	if s == nil {
		return nil
	}
	out := []string{}
	for _, e := range s.Expect {
		if e.Passed || e.Rule == "unevaluated" {
			continue
		}
		out = append(out, chain.DescribeFailure(chain.ExpectResult{Path: e.Path, Rule: e.Rule, Want: e.Want, Got: e.Got}))
	}
	if len(out) == 0 {
		if s.Transport != nil {
			out = append(out, fmt.Sprintf("transport %s: %s", s.Transport.Code, s.Transport.Message))
		} else if s.Error != "" {
			out = append(out, runner.FirstLine(s.Error))
		}
	}
	return out
}

func MaskVarValues(vars map[string]any, text string) string {
	names := make([]string, 0, len(vars))
	for name, v := range vars {
		if value := fmt.Sprint(v); len(value) >= minFixtureEcho && value != pathmask.MaskRedacted {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return len(fmt.Sprint(vars[names[i]])) > len(fmt.Sprint(vars[names[j]])) })
	for _, name := range names {
		text = strings.ReplaceAll(text, fmt.Sprint(vars[name]), "${vars."+name+"}")
	}
	return text
}

func (r *RunReport) describe(c Change) string {
	if c.Kind == KindLength || c.Kind == KindType {
		return c.DescribeRuns()
	}
	a, b := map[string]any{}, map[string]any{}
	for name, v := range r.varsA {
		if w, ok := r.varsB[name]; ok && fmt.Sprint(v) != fmt.Sprint(w) {
			a[name], b[name] = v, w
		}
	}
	return fmt.Sprintf("a=%s b=%s", runValue(c.Want, a), runValue(c.Got, b))
}

func runValue(v any, vars map[string]any) string {
	switch t := v.(type) {
	case string:
		return chain.EdgeQuoted(MaskVarValues(vars, t))
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}
