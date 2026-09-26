package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
	"github.com/N4darae/shrt/transport"
)

const (
	KindMissing    = "missing"
	KindUnexpected = "unexpected"
	KindChanged    = "changed"
	KindType       = "type"
	KindOrder      = "order"
	KindLength     = "length"
	KindMembership = "membership"
	KindStatus     = "status"
	KindNotReached = "not_reached"
)

type Change struct {
	Step   string `json:"step,omitempty"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Want   any    `json:"want,omitempty"`
	Got    any    `json:"got,omitempty"`
	Detail string `json:"detail,omitempty"`

	WithInput  bool   `json:"with_different_input,omitempty"`
	ReplayPath string `json:"replay_path,omitempty"`
	Mask       string `json:"mask,omitempty"`
}

type Report struct {
	Chain          string   `json:"chain"`
	SafeSpotID     string   `json:"safe_spot_run_id"`
	RunID          string   `json:"run_id"`
	SafeSpotTarget string   `json:"safe_spot_target,omitempty"`
	RunTarget      string   `json:"run_target,omitempty"`
	FirstFailure   string   `json:"first_failure,omitempty"`
	RequestChanges []Change `json:"request_changes,omitempty"`
	InputCause     string   `json:"input_cause,omitempty"`
	FixtureInput   []Change `json:"fixture_input,omitempty"`
	FixtureEchoed  []Change `json:"fixture_echoed,omitempty"`
	Changes        []Change `json:"changes"`
	Masked         int      `json:"masked"`
	VolatileMasked int      `json:"volatile_masked"`
	VolatilePaths  []string `json:"volatile_paths,omitempty"`
	VolatileValues []Change `json:"volatile_values,omitempty"`
	ShapeMasked    []Change `json:"shape_masked,omitempty"`
	Redacted       int      `json:"redacted"`
	RedactedPaths  []string `json:"redacted_paths,omitempty"`
	ScrubbedPaths  []string `json:"scrubbed_paths,omitempty"`
	FullyMasked    []string `json:"fully_masked,omitempty"`

	UnapprovedVolatile []string     `json:"unapproved_volatile,omitempty"`
	UnapprovedMasked   []string     `json:"unapproved_masked,omitempty"`
	UnapprovedRedact   []string     `json:"unapproved_redact,omitempty"`
	UnapprovedRedacted []string     `json:"unapproved_redacted,omitempty"`
	PrincipalUnchecked []string     `json:"principal_unchecked,omitempty"`
	Reordered          []string     `json:"reordered_lists,omitempty"`
	RenamedSteps       []StepRename `json:"renamed_steps,omitempty"`
	UnsentDefaults     []string     `json:"unsent_defaults,omitempty"`
	UndeclaredSame     []string     `json:"undeclared_same,omitempty"`
	HideMasked         bool         `json:"-"`
	UndeclaredUnknown  []string     `json:"undeclared_uncompared,omitempty"`

	inputSeparated    bool
	compared          []comparedStep
	approvedMask      *pathmask.Masker
	renames           [][2]string
	reorderCandidates []stepPath
	reordered         []stepPath
	reorderExpect     map[string][]string
	folded            map[string]bool
	foldWhy           string
}

func (r *Report) FoldSteps(steps []string, why string) {
	if len(steps) == 0 {
		return
	}
	r.folded = map[string]bool{}
	for _, s := range steps {
		r.folded[s] = true
	}
	r.foldWhy = why
}

func (r *Report) foldedCount(step string) int {
	n := 0
	for _, c := range r.Changes {
		if c.Step == step && c.Kind != KindNotReached {
			n++
		}
	}
	return n
}

func (r *Report) Widened() bool { return len(r.UnapprovedVolatile) > 0 || len(r.UnapprovedRedact) > 0 }

func (r *Report) NoteApprovedRedact(approved []string, rec *runner.Record) {
	had := map[string]bool{}
	for _, p := range approved {
		had[p] = true
	}
	for _, p := range rec.Redacted {
		if !had[p] {
			r.addUnapprovedRedact(p)
		}
	}
}

func (r *Report) NoteRedactedRequests(spot *store.SafeSpot, rec *runner.Record) {
	if len(r.UnapprovedRedact) == 0 {
		return
	}
	masker := pathmask.NewMasker(r.UnapprovedRedact)
	for _, p := range sameIDSteps(spot.Steps, rec.Steps) {
		want, got := p[0], p[1]
		if len(want.Request) == 0 || len(got.Request) == 0 {
			continue
		}
		a, errA := decode(want.Request)
		b, errB := decode(got.Request)
		if errA != nil || errB != nil {
			continue
		}
		walk(a, b, "", func(c Change) {
			if (c.Kind == KindChanged || c.Kind == KindType) && c.Got == pathmask.MaskRedacted && c.Want != pathmask.MaskRedacted && maskedAt(masker, c) {
				r.UnapprovedRedacted = append(r.UnapprovedRedacted, want.ID+" request "+c.Path)
			}
		})
	}
}

func neverRedacted(v any) bool {
	switch t := v.(type) {
	case nil, bool:
		return true
	case string:
		return t == "" || t == "0"
	case float64:
		return t == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func (r *Report) addUnapprovedRedact(pattern string) {
	for _, p := range r.UnapprovedRedact {
		if p == pattern {
			return
		}
	}
	r.UnapprovedRedact = append(r.UnapprovedRedact, pattern)
}

func (r *Report) oneSidedRedaction(c Change, patterns []string) bool {
	if c.Kind != KindChanged && c.Kind != KindType {
		return false
	}
	if w, ok := c.Want.(string); ok && w == pathmask.MaskRedacted && !neverRedacted(c.Got) {
		r.RedactedPaths = append(r.RedactedPaths, c.Step+" "+c.Path)
		r.Redacted++
		return true
	}
	if g, ok := c.Got.(string); !ok || g != pathmask.MaskRedacted || neverRedacted(c.Want) {
		return false
	}
	covered := false
	for _, p := range patterns {
		if maskedAt(pathmask.NewMasker([]string{p}), c) {
			covered = true
			r.addUnapprovedRedact(p)
		}
	}
	if covered {
		r.UnapprovedRedacted = append(r.UnapprovedRedacted, c.Step+" "+c.Path)
	}
	return covered
}

func (r *Report) Clean() bool { return len(r.Changes) == 0 }

func (r *Report) NotReachedCount() int {
	n := 0
	for _, c := range r.Changes {
		if c.Kind == KindNotReached {
			n++
		}
	}
	return n
}

func (r *Report) Counted() int {
	changedAt := r.valueChangedSteps()
	n := 0
	for _, c := range r.Changes {
		if c.Kind != KindNotReached && !(c.Kind == KindStatus && changedAt[c.Step]) {
			n++
		}
	}
	if n > 0 {
		return n
	}
	return len(r.Changes)
}

func (r *Report) valueChangedSteps() map[string]bool {
	changedAt := map[string]bool{}
	for _, c := range r.Changes {
		if c.Kind != KindStatus && c.Kind != KindNotReached {
			changedAt[c.Step] = true
		}
	}
	return changedAt
}

func (r *Report) uncountedNote() string {
	n := r.NotReachedCount()
	if n == 0 || n == len(r.Changes) {
		return ""
	}
	return fmt.Sprintf("; %d step(s) not reached are listed below and not counted: a step the run never sent is not a change", n)
}

const (
	AuthProfilePath   = "auth_profile"
	AuthPrincipalPath = "auth_principal"
)

func (r *Report) PrincipalChanged() bool {
	for _, c := range r.RequestChanges {
		if c.Path == AuthProfilePath || c.Path == AuthPrincipalPath {
			return true
		}
	}
	return false
}

func Compare(spot *store.SafeSpot, rec *runner.Record) *Report {
	return CompareMasking(spot, rec, nil)
}

func CompareMasking(spot *store.SafeSpot, rec *runner.Record, extra []string) *Report {
	rep := compareMasking(spot, rec, extra, nil)
	rep.noteReordered(spot, rec, extra, nil, nil)
	return rep
}

func compareMasking(spot *store.SafeSpot, rec *runner.Record, extra []string, assumed map[string][]string) *Report {
	rep := &Report{Chain: spot.Chain, SafeSpotID: spot.RunID, RunID: rec.RunID}
	if !config.SameTarget(spot.Target, rec.Target) {
		rep.SafeSpotTarget, rep.RunTarget = spot.Target, rec.Target
	}
	rep.PrincipalUnchecked = uncheckedPrincipals(spot, rec)
	masker := pathmask.NewMasker(mergePatterns(spot.Volatile, rec.Volatile, extra))
	approvedPatterns := append([]string{}, spot.Volatile...)
	for _, st := range spot.Steps {
		approvedPatterns = append(approvedPatterns, st.Volatile...)
	}
	approved := pathmask.NewMasker(approvedPatterns)
	rep.approvedMask = approved
	rep.UnapprovedVolatile = unapproved(approvedPatterns, rec, extra)
	first := firstRed(rec)
	if first != nil {
		rep.FirstFailure = fmt.Sprintf("step %d %s (%s)", first.Index, first.ID, first.Status)
		if why := firstLineOf(first.Error); why != "" {
			rep.FirstFailure += ": " + why
		} else if failed := failedExpectation(first); failed != "" {
			rep.FirstFailure += ": " + failed
		}
	}
	stoppedEarly := len(rec.Steps) < len(spot.Steps) && !rec.Passed()

	redactPaths := pathmask.NewMasker(rec.Redacted)
	spotWin, recWin := spotWindow(spot), recordWindow(rec)
	idPairs := []idPair{}
	pairs, structural, tail := alignSteps(spot.Steps, rec.Steps, stoppedEarly)
	rep.Changes = append(rep.Changes, structural...)
	for _, p := range pairs {
		i, want, got := p.i, p.want, p.got
		if want.ID != got.ID || !SameCall(want, got) {
			rep.Changes = append(rep.Changes, Change{
				Step: want.ID, Path: fmt.Sprintf("steps.%d", i), Kind: KindOrder,
				Want: want.ID + " " + want.Call, Got: got.ID + " " + got.Call,
			})
			continue
		}
		if StepReached(rec, want) && !StepReached(rec, got) && got != first && !sentUnanswered(got) {
			rep.Changes = append(rep.Changes, Change{
				Step: want.ID, Path: "status", Kind: KindNotReached,
				Want: want.Status, Got: got.Status, Detail: firstLineOf(got.Error),
			})
			continue
		}
		if want.Status != got.Status {
			change := Change{Step: want.ID, Path: "status", Kind: KindStatus, Want: want.Status, Got: got.Status}
			if got.Status == runner.StatusError || got.Transport != nil {
				change.Detail = firstLineOf(got.Error)
			}
			if change.Detail == "" {
				change.Detail = heldBackDetail(got)
			}
			rep.Changes = append(rep.Changes, change)
		}
		if !StepReached(rec, got) {
			continue
		}
		gotRedacted := map[string]bool{}
		for _, p := range pathmask.RedactedPaths(got.Response) {
			gotRedacted[p] = true
		}
		for _, p := range pathmask.RedactedPaths(want.Response) {
			switch {
			case !gotRedacted[p]:
			case len(rec.Redacted) > 0 && !redactPaths.Masks(p):
				rep.ScrubbedPaths = append(rep.ScrubbedPaths, want.ID+" "+p)
			default:
				rep.Redacted++
				rep.RedactedPaths = append(rep.RedactedPaths, want.ID+" "+p)
			}
		}
		stepMask := pathmask.NewMasker(mergePatterns(masker.Patterns(), want.Volatile, got.Volatile))
		if everyFieldMasked(stepMask, want.Response) && everyFieldMasked(stepMask, got.Response) {
			rep.FullyMasked = append(rep.FullyMasked, want.ID)
		}
		a, errA := decode(want.Response)
		b, errB := decode(got.Response)
		var stepChanges []Change
		moves := map[string][]int{}
		if errA != nil || errB != nil {
			stepChanges = compareStep(want, got)
		} else {
			known := renamer(idRenames(idPairs))
			declared := unorderedSet(want.Unordered, got.Unordered, assumed[want.ID])
			if len(declared) > 0 {
				b = reorderUnordered(a, b, "", declared, known, moves)
			}
			if assumed == nil {
				reorderCandidates(a, b, "", declared, known, func(p string) {
					rep.reorderCandidates = append(rep.reorderCandidates, stepPath{want.ID, p})
				})
			}
			walk(a, b, "", func(c Change) {
				c.Step = want.ID
				c.ReplayPath = replayPath(c.Path, moves)
				stepChanges = append(stepChanges, c)
			})
		}
		for _, c := range stepChanges {
			shaped, why := false, ""
			if c.Kind == KindChanged {
				shaped, why = volatileIn(c.Path, c.Want, c.Got, spotWin, recWin)
			}
			switch {
			case rep.oneSidedRedaction(c, rec.Redacted):
			case c.Path != "response" && maskedValue(stepMask, c) && !vanishedUnderMask(stepMask, c):
				rep.VolatileMasked++
				rep.VolatilePaths = append(rep.VolatilePaths, c.Step+" "+c.Path)
				c.Mask = maskOf(stepMask, c)
				rep.VolatileValues = append(rep.VolatileValues, c)
				if !maskedValue(approved, c) && (c.Kind != KindChanged || !looksVolatile(c.Path, c.Want, c.Got)) {
					rep.UnapprovedMasked = append(rep.UnapprovedMasked, c.Step+" "+c.Path)
				}
			case shaped && !valueVanished(c):
				rep.Masked++
				rep.ShapeMasked = append(rep.ShapeMasked, c)
			default:
				if c.Detail == "" && vanishedUnderMask(stepMask, c) {
					c.Detail = vanishedDetail(stepMask, c, "the safe spot", "this run")
				}
				if why != "" && c.Detail == "" {
					c.Detail = why
				}
				noteTimeUnit(&c)
				rep.Changes = append(rep.Changes, c)
			}
		}
		if errA == nil && errB == nil {
			collectIDPairs(want.ID, a, b, "", stepMask, &idPairs)
			rep.compared = append(rep.compared, comparedStep{id: want.ID, want: a, got: b, mask: stepMask})
		}
	}
	rep.applyRenaming(idPairs)
	rep.renames = idRenames(idPairs)
	rep.collapseMembership(rec)
	var renamed []Change
	rep.Changes, renamed = splitEchoes(rep.Changes, rep.compared, rep.renames)
	rep.Masked += len(renamed)
	rep.ShapeMasked = append(rep.ShapeMasked, renamed...)
	rep.dropEchoedUnapproved(rep.renames)
	if len(tail) > 0 {
		why := "the run stopped before this step"
		if first != nil {
			why = fmt.Sprintf("the run stopped at step %d %s", first.Index, first.ID)
		}
		for _, want := range tail {
			rep.Changes = append(rep.Changes, Change{
				Step: want.ID, Path: "status", Kind: KindNotReached, Want: want.Status, Detail: why,
			})
		}
	}
	return rep
}

type stepPair struct {
	i         int
	want, got *runner.StepRecord
}

func uniqueIDs(steps []*runner.StepRecord) bool {
	seen := map[string]bool{}
	for _, st := range steps {
		if st.ID == "" || seen[st.ID] {
			return false
		}
		seen[st.ID] = true
	}
	return true
}

func sameIDSteps(was, now []*runner.StepRecord) [][2]*runner.StepRecord {
	out := [][2]*runner.StepRecord{}
	if !uniqueIDs(was) || !uniqueIDs(now) {
		for i := range min(len(was), len(now)) {
			if was[i].ID == now[i].ID {
				out = append(out, [2]*runner.StepRecord{was[i], now[i]})
			}
		}
		return out
	}
	at := map[string]*runner.StepRecord{}
	for _, st := range now {
		at[st.ID] = st
	}
	for _, st := range was {
		if got, ok := at[st.ID]; ok {
			out = append(out, [2]*runner.StepRecord{st, got})
		}
	}
	return out
}

func alignSteps(spot, rec []*runner.StepRecord, stoppedEarly bool) ([]stepPair, []Change, []*runner.StepRecord) {
	pairs, structural, tail := []stepPair{}, []Change{}, []*runner.StepRecord{}
	if !uniqueIDs(spot) || !uniqueIDs(rec) {
		if len(spot) != len(rec) && !stoppedEarly {
			structural = append(structural, Change{Path: "steps", Kind: KindLength, Want: len(spot), Got: len(rec)})
		}
		n := min(len(spot), len(rec))
		for i := range n {
			pairs = append(pairs, stepPair{i, spot[i], rec[i]})
		}
		if stoppedEarly {
			tail = spot[n:]
		}
		return pairs, structural, tail
	}
	recAt := map[string]int{}
	for j, st := range rec {
		recAt[st.ID] = j
	}
	inSpot := map[string]bool{}
	lastPresent := -1
	for i, st := range spot {
		inSpot[st.ID] = true
		if _, ok := recAt[st.ID]; ok {
			lastPresent = i
		}
	}
	for i, st := range spot {
		j, ok := recAt[st.ID]
		switch {
		case ok:
			pairs = append(pairs, stepPair{i, st, rec[j]})
		case stoppedEarly && i > lastPresent:
			tail = append(tail, st)
		default:
			structural = append(structural, Change{Step: st.ID, Path: "step", Kind: KindMissing, Want: st.Call, Detail: "this run has no such step"})
		}
	}
	for _, st := range rec {
		if !inSpot[st.ID] {
			structural = append(structural, Change{Step: st.ID, Path: "step", Kind: KindUnexpected, Got: st.Call, Detail: "the safe spot has no such step"})
		}
	}
	wasOrder := make([]string, 0, len(pairs))
	for _, p := range pairs {
		wasOrder = append(wasOrder, p.want.ID)
	}
	nowOrder := make([]string, 0, len(pairs))
	for _, st := range rec {
		if inSpot[st.ID] {
			nowOrder = append(nowOrder, st.ID)
		}
	}
	if strings.Join(wasOrder, ",") != strings.Join(nowOrder, ",") {
		structural = append(structural, Change{Step: "-", Path: "steps", Kind: KindOrder, Want: strings.Join(wasOrder, ", "), Got: strings.Join(nowOrder, ", ")})
	}
	return pairs, structural, tail
}

func (r *Report) applyRenaming(pairs []idPair) {
	broken := renamingViolations(pairs)
	if len(broken) == 0 {
		return
	}
	at := map[string]bool{}
	for _, c := range broken {
		at[c.Step+" "+c.Path] = true
	}
	kept := r.ShapeMasked[:0]
	for _, c := range r.ShapeMasked {
		if at[c.Step+" "+c.Path] {
			r.Masked--
			continue
		}
		kept = append(kept, c)
	}
	r.ShapeMasked = kept
	r.Changes = append(r.Changes, broken...)
}

func CompareWithRequests(spot *store.SafeSpot, rec *runner.Record, extra []string, derived func(step, path string) bool) *Report {
	rep := CompareMasking(spot, rec, extra)
	rep.RequestChanges = CompareRequests(spot, rec, derived)
	return rep
}

func ChainChanges(spot *store.SafeSpot, c *chain.Chain) []Change {
	return ChainChangesIn(spot, c, nil)
}

func ChainChangesIn(spot *store.SafeSpot, c *chain.Chain, rec *runner.Record) []Change {
	out := []Change{}
	if c == nil {
		return out
	}
	now := map[string]*chain.Step{}
	nowOrder := []string{}
	for _, s := range c.Steps {
		if s != nil {
			now[s.ID] = s
			nowOrder = append(nowOrder, s.ID)
		}
	}
	was := map[string]bool{}
	wasOrder := []string{}
	for i, st := range spot.Steps {
		was[st.ID] = true
		wasOrder = append(wasOrder, st.ID)
		s, ok := now[st.ID]
		switch {
		case !ok:
			out = append(out, Change{Step: st.ID, Path: "step", Kind: KindMissing, Want: st.Call,
				Detail: "the chain no longer has this step"})
		case !callNames(s.Call, st.Call, st.Procedure):
			out = append(out, Change{Step: st.ID, Path: "call", Kind: KindChanged, Want: st.Call, Got: s.Call})
		default:
			var ran *runner.StepRecord
			if rec != nil {
				ran, _ = rec.Step(st.ID)
			}
			out = append(out, expectChanges(st, s, ran)...)
			out = append(out, refChanges(spot.Steps[:i], st, s)...)
		}
	}
	for _, id := range nowOrder {
		if !was[id] {
			out = append(out, Change{Step: id, Path: "step", Kind: KindUnexpected, Got: now[id].Call,
				Detail: "the chain has a step the confirmed run did not"})
		}
	}
	wasKept, nowKept := []string{}, []string{}
	for _, id := range wasOrder {
		if now[id] != nil {
			wasKept = append(wasKept, id)
		}
	}
	for _, id := range nowOrder {
		if was[id] {
			nowKept = append(nowKept, id)
		}
	}
	if strings.Join(wasKept, ",") != strings.Join(nowKept, ",") {
		out = append(out, Change{Step: "-", Path: "steps", Kind: KindOrder,
			Want: strings.Join(wasKept, ", "), Got: strings.Join(nowKept, ", ")})
	}
	return out
}

func SameCall(a, b *runner.StepRecord) bool {
	if a.Call == b.Call {
		return true
	}
	if a.Procedure != "" && b.Procedure != "" {
		return strings.EqualFold(a.Procedure, b.Procedure)
	}
	return callNames(a.Call, b.Call, b.Procedure) || callNames(b.Call, a.Call, a.Procedure)
}

func callNames(call, recorded, procedure string) bool {
	if call == recorded {
		return true
	}
	if procedure == "" {
		return false
	}
	c := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(call), "/"))
	p := strings.ToLower(strings.TrimPrefix(procedure, "/"))
	return c != "" && (c == p || strings.HasSuffix(p, "/"+c) || strings.HasSuffix(p, "."+c))
}

const ExpectPath = "expect"

func expectChanges(was *runner.StepRecord, now *chain.Step, ran *runner.StepRecord) []Change {
	out := []Change{}
	declared := make([]chain.ExpectResult, len(now.Expect))
	for i, e := range now.Expect {
		declared[i] = e.Evaluate(nil)
	}
	shown := func(i int) string {
		r := declared[i]
		if ran != nil && i < len(ran.Expect) && ran.Expect[i].Path == r.Path && ran.Expect[i].Rule == r.Rule {
			return expectText(r.Path, r.Rule, ran.Expect[i].Want)
		}
		return expectText(r.Path, r.Rule, r.Want)
	}
	pairs := pairExpectations(was.Expect, declared)
	paths := []string{}
	seen := map[string]bool{}
	for _, r := range declared {
		if !seen[namecase.Fold(r.Path)] {
			seen[namecase.Fold(r.Path)] = true
			paths = append(paths, r.Path)
		}
	}
	for _, w := range was.Expect {
		if !seen[namecase.Fold(w.Path)] {
			seen[namecase.Fold(w.Path)] = true
			paths = append(paths, w.Path)
		}
	}
	matched := map[int]bool{}
	for _, j := range pairs {
		if j >= 0 {
			matched[j] = true
		}
	}
	for _, path := range paths {
		for i, w := range was.Expect {
			if !namecase.Equal(w.Path, path) {
				continue
			}
			j := pairs[i]
			if j < 0 {
				out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindMissing, Want: expectText(w.Path, w.Rule, w.Want)})
				continue
			}
			r := declared[j]
			if w.Rule == "unevaluated" {
				continue
			}
			same := w.Rule == r.Rule
			if same && fmt.Sprint(w.Want) != pathmask.MaskRedacted {
				same = sameDeclaredOperand(w.Want, r.Want)
			}
			if !same {
				out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindChanged, Want: expectText(w.Path, w.Rule, w.Want), Got: shown(j)})
			}
		}
		for j, r := range declared {
			if namecase.Equal(r.Path, path) && !matched[j] {
				out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindUnexpected, Got: shown(j)})
			}
		}
	}
	return out
}

func sameDeclaredOperand(recorded, declared any) bool {
	switch d := declared.(type) {
	case map[string]any:
		if text, ok := recorded.(string); ok {
			of, by, split := strings.Cut(text, " ± ")
			return split && len(d) == 2 && sameDeclaredScalar(of, d["of"]) && sameDeclaredScalar(by, d["by"])
		}
		r, ok := recorded.(map[string]any)
		if !ok || len(r) != len(d) {
			return false
		}
		for k, v := range d {
			if rv, ok := r[k]; !ok || !sameDeclaredOperand(rv, v) {
				return false
			}
		}
		return true
	case []any:
		r, ok := recorded.([]any)
		if text, isText := recorded.(string); isText && strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			r = nil
			for _, part := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(text, "["), "]"), ", ") {
				r = append(r, part)
			}
			ok = true
		}
		if !ok || len(r) != len(d) {
			return false
		}
		for i := range d {
			if !sameDeclaredOperand(r[i], d[i]) {
				return false
			}
		}
		return true
	}
	return sameDeclaredScalar(recorded, declared)
}

func sameDeclaredScalar(recorded, declared any) bool {
	if text, ok := declared.(string); ok && strings.Contains(text, "${") {
		return true
	}
	if fmt.Sprint(recorded) == fmt.Sprint(declared) {
		return true
	}
	a, okA := chain.OperandText(recorded)
	b, okB := chain.OperandText(declared)
	return okA && okB && a == b
}

func pairExpectations(was, now []chain.ExpectResult) []int {
	pairs := make([]int, len(was))
	taken := make([]bool, len(now))
	for i := range pairs {
		pairs[i] = -1
	}
	for _, sameRule := range []bool{true, false} {
		for i, w := range was {
			if pairs[i] >= 0 {
				continue
			}
			for j, r := range now {
				if taken[j] || !namecase.Equal(r.Path, w.Path) || (sameRule && r.Rule != w.Rule && w.Rule != "unevaluated") {
					continue
				}
				pairs[i], taken[j] = j, true
				break
			}
		}
	}
	return pairs
}

func expectText(path, rule string, want any) string {
	if want == nil {
		return path + " " + rule
	}
	return fmt.Sprintf("%s %s %s", path, rule, show(want))
}

func CompareRequests(spot *store.SafeSpot, rec *runner.Record, derived func(step, path string) bool) []Change {
	out := []Change{}
	for _, p := range sameIDSteps(spot.Steps, rec.Steps) {
		want, got := p[0], p[1]
		if want.AuthProfile != "" && got.AuthProfile != "" && want.AuthProfile != got.AuthProfile {
			out = append(out, Change{Step: want.ID, Path: AuthProfilePath, Kind: KindChanged, Want: want.AuthProfile, Got: got.AuthProfile})
		} else if want.AuthPrincipal != "" && got.AuthPrincipal != "" && want.AuthPrincipal != got.AuthPrincipal {
			out = append(out, Change{Step: want.ID, Path: AuthPrincipalPath, Kind: KindChanged, Want: want.AuthPrincipal, Got: got.AuthPrincipal,
				Detail: fmt.Sprintf("auth profile %q logged in as another principal: its login body's non-secret fields differ", got.AuthProfile)})
		}
		out = append(out, headerChanges(want, got)...)
		if len(want.Request) == 0 || len(got.Request) == 0 {
			continue
		}
		a, errA := decode(want.Request)
		b, errB := decode(got.Request)
		if errA != nil || errB != nil {
			continue
		}
		walk(a, b, "", func(c Change) {
			if derived != nil && derived(want.ID, c.Path) {
				return
			}
			if derived == nil && c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got) {
				return
			}
			if (c.Kind == KindChanged || c.Kind == KindType) && (c.Want == pathmask.MaskRedacted || c.Got == pathmask.MaskRedacted) {
				return
			}
			c.Step = want.ID
			out = append(out, c)
		})
	}
	return out
}

const HeadersPathPrefix = "headers."

func headerChanges(want, got *runner.StepRecord) []Change {
	if want.Headers == nil || got.Headers == nil {
		return nil
	}
	names := []string{}
	for k := range want.Headers {
		names = append(names, k)
	}
	for k := range got.Headers {
		if _, ok := want.Headers[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	out := []Change{}
	for _, k := range names {
		w, had := want.Headers[k]
		g, has := got.Headers[k]
		switch {
		case had && has && (w == g || chain.FoldedRefs(w) == chain.FoldedRefs(g)):
		case had && has && (w == pathmask.MaskRedacted && runner.HeaderDigested(g) || g == pathmask.MaskRedacted && runner.HeaderDigested(w)):
		case had && has:
			out = append(out, Change{Step: want.ID, Path: HeadersPathPrefix + k, Kind: KindChanged, Want: w, Got: g, Detail: headerRefDetail(w, g)})
		case had:
			out = append(out, Change{Step: want.ID, Path: HeadersPathPrefix + k, Kind: KindMissing, Want: w, Detail: headerRefDetail(w)})
		default:
			out = append(out, Change{Step: want.ID, Path: HeadersPathPrefix + k, Kind: KindUnexpected, Got: g, Detail: headerRefDetail(g)})
		}
	}
	return out
}

func headerRefDetail(texts ...string) string {
	for _, t := range texts {
		if runner.ReadsAnotherStep(t) {
			return HeaderRefDetail
		}
	}
	return ""
}

func uncheckedPrincipals(spot *store.SafeSpot, rec *runner.Record) []string {
	out := []string{}
	for _, p := range sameIDSteps(spot.Steps, rec.Steps) {
		want, got := p[0], p[1]
		if want.AuthPrincipal != "" || got.AuthPrincipal == "" {
			continue
		}
		if want.AuthProfile == "" || want.AuthProfile == got.AuthProfile {
			out = append(out, want.ID)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *Report) principalCaveat() string {
	if len(r.PrincipalUnchecked) == 0 {
		return ""
	}
	return fmt.Sprintf("principal checking is off for safe spot %s: it records no auth_principal (it was confirmed before shrt recorded one), "+
		"so it cannot say which account step(s) %s logged in as, and a change below may come from logging in as another account rather than "+
		"from the backend. To turn it on, propose a passing run in its place and have a person approve it: shrt confirm %s -supersede -note \"...\"\n",
		r.SafeSpotID, strings.Join(r.PrincipalUnchecked, ", "), r.Chain)
}

func (r *Report) inputCounts() (int, int, int) {
	requests, edits, values := 0, 0, 0
	for _, c := range r.RequestChanges {
		switch {
		case chainLevel(c) || c.Path == ExpectPath:
			edits++
		case c.Path == ExpectValuePath:
			values++
		default:
			requests++
		}
	}
	return requests, edits, values
}

func (r *Report) OnlyChainChanged() bool {
	requests, edits, values := r.inputCounts()
	return requests == 0 && values == 0 && edits > 0
}

func (r *Report) InputSummary() string {
	requests, edits, values := r.inputCounts()
	parts := []string{}
	if requests > 0 {
		parts = append(parts, fmt.Sprintf("%d request value(s) differ", requests))
	}
	if values > 0 {
		parts = append(parts, fmt.Sprintf("%d expectation value(s) differ", values))
	}
	if edits > 0 {
		parts = append(parts, fmt.Sprintf("%d chain change(s)", edits))
	}
	return strings.Join(parts, " and ")
}

func (c Change) Transition() string {
	switch c.Kind {
	case KindMissing:
		return fmt.Sprintf("%s -> absent", show(c.Want))
	case KindUnexpected:
		return fmt.Sprintf("absent -> %s", show(c.Got))
	case KindLength:
		return fmt.Sprintf("%s item(s) -> %s item(s)", show(c.Want), show(c.Got))
	case KindType:
		return fmt.Sprintf("%s -> %s", withKind(c.Want), withKind(c.Got))
	}
	return fmt.Sprintf("%s -> %s", show(c.Want), show(c.Got))
}

func StepReached(rec *runner.Record, s *runner.StepRecord) bool {
	if rec.DryRun {
		return true
	}
	switch s.Status {
	case runner.StatusSkipped:
		return false
	case runner.StatusError:
		return s.HTTPStatus != 0 || len(s.Response) > 0
	}
	return true
}

func firstRed(rec *runner.Record) *runner.StepRecord {
	for _, s := range rec.Steps {
		if s.Status == runner.StatusFailed || s.Status == runner.StatusError {
			return s
		}
	}
	return nil
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func unapproved(approved []string, rec *runner.Record, extra []string) []string {
	seen := map[string]bool{}
	for _, p := range approved {
		seen[p] = true
	}
	out := []string{}
	add := func(ps []string) {
		for _, p := range ps {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	add(rec.Volatile)
	add(extra)
	for _, st := range rec.Steps {
		add(st.Volatile)
	}
	return out
}

func maskedValue(m *pathmask.Masker, c Change) bool {
	if maskedAt(m, c) {
		return true
	}
	return (c.Kind == KindMissing || c.Kind == KindUnexpected) && !valueVanished(c) && m.Masks(c.Path)
}

func maskedAt(m *pathmask.Masker, c Change) bool {
	segs := strings.Split(c.Path, ".")
	limit := len(segs)
	if c.Kind == KindMissing || c.Kind == KindUnexpected {
		limit--
	}
	for i := limit; i > 0; i-- {
		if m.Masks(strings.Join(segs[:i], ".")) {
			return true
		}
	}
	return false
}

func compareStep(want, got *runner.StepRecord) []Change {
	a, errA := decode(want.Response)
	b, errB := decode(got.Response)
	if errA != nil || errB != nil {
		return []Change{{Step: want.ID, Path: "response", Kind: KindType, Want: errA, Got: errB}}
	}
	changes := []Change{}
	walk(a, b, "", func(c Change) {
		c.Step = want.ID
		changes = append(changes, c)
	})
	return changes
}

func decode(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func walk(want, got any, path string, emit func(Change)) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			emit(Change{Path: pathOr(path), Kind: KindType, Want: want, Got: got})
			return
		}
		for _, k := range sortedKeys(w, g) {
			wv, wok := w[k]
			gv, gok := g[k]
			child := pathmask.Join(path, k)
			switch {
			case wok && !gok:
				emit(Change{Path: child, Kind: KindMissing, Want: wv})
			case !wok && gok:
				emit(Change{Path: child, Kind: KindUnexpected, Got: gv})
			default:
				walk(wv, gv, child, emit)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			emit(Change{Path: pathOr(path), Kind: KindType, Want: want, Got: got})
			return
		}
		if len(w) != len(g) {
			emit(Change{Path: pathOr(path), Kind: KindLength, Want: len(w), Got: len(g)})
		}
		for i := range min(len(w), len(g)) {
			walk(w[i], g[i], pathmask.Join(path, pathmask.IndexKey(i)), emit)
		}
	default:
		switch {
		case jsonKind(want) != jsonKind(got):
			emit(Change{Path: pathOr(path), Kind: KindType, Want: want, Got: got})
		case !sameScalar(want, got):
			emit(Change{Path: pathOr(path), Kind: KindChanged, Want: want, Got: got})
		}
	}
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64, float32, int, int32, int64:
		return "number"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return fmt.Sprintf("%T", v)
}

func sameScalar(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func sortedKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	keys := []string{}
	for k := range a {
		seen[k] = true
		keys = append(keys, k)
	}
	for k := range b {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func mergePatterns(sets ...[]string) []string {
	out := []string{}
	for _, s := range sets {
		out = append(out, s...)
	}
	return out
}

func pathOr(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func (c Change) StepOrder() bool {
	return c.Kind == KindOrder && c.Path == "steps" && (c.Step == "-" || c.Step == "")
}

func (c Change) Moves() string {
	was := strings.Split(fmt.Sprint(c.Want), ", ")
	now := strings.Split(fmt.Sprint(c.Got), ", ")
	at := map[string]int{}
	for i, id := range was {
		at[id] = i + 1
	}
	out := []string{}
	for i, id := range now {
		if j, ok := at[id]; ok && j != i+1 {
			out = append(out, fmt.Sprintf("%s step %d -> %d", id, j, i+1))
		}
	}
	return strings.Join(out, ", ")
}

func (c Change) describe() string {
	out := c.describeValues()
	if c.ReplayPath != "" {
		out += " (this item is at " + c.ReplayPath + " in this run: an unordered list is paired by content, and the path names the safe spot's index)"
	}
	if c.Detail != "" {
		out += " (" + c.Detail + ")"
	}
	return out
}

func (c Change) describeValues() string {
	if c.Kind == KindNotReached {
		if c.Got == nil {
			return fmt.Sprintf("want=%s got=not recorded", show(c.Want))
		}
		return fmt.Sprintf("want=%s got=%s, %s", show(c.Want), show(c.Got), sentOrNot(c.Detail))
	}
	if c.Kind == KindLength || c.Kind == KindMembership {
		return fmt.Sprintf("want=%s item(s) got=%s item(s)", show(c.Want), show(c.Got))
	}
	if c.Path == "step" && c.Kind == KindMissing {
		return fmt.Sprintf("want=%s got=absent", show(c.Want))
	}
	if c.Path == "step" && c.Kind == KindUnexpected {
		return fmt.Sprintf("want=absent got=%s", show(c.Got))
	}
	if c.Kind == KindChanged && fmt.Sprint(c.Want) == fmt.Sprint(c.Got) {
		return fmt.Sprintf("want=%s got=%s, the same text, so the change is that it did not change", withKind(c.Want), withKind(c.Got))
	}
	if c.Kind != KindType {
		return fmt.Sprintf("want=%s got=%s", show(c.Want), show(c.Got))
	}
	return fmt.Sprintf("want=%s got=%s", withKind(c.Want), withKind(c.Got))
}

var heldBackProducer = regexp.MustCompile(`reads step "([^"]+)", which did not pass`)

func heldBackDetail(st *runner.StepRecord) string {
	answered, producer := []string{}, ""
	for _, ex := range st.Expect {
		if ex.Rule != "unevaluated" {
			continue
		}
		if m := heldBackProducer.FindStringSubmatch(ex.Detail); m != nil && producer == "" {
			producer = m[1]
		}
		if ex.Got != nil {
			answered = append(answered, ex.Path+"="+show(ex.Got))
		}
	}
	if producer == "" {
		return ""
	}
	out := "not judged: it reads step " + producer + ", which did not pass"
	if len(answered) > 0 {
		out = "answered " + strings.Join(answered, ", ") + ", " + out
	}
	return out
}

func show(v any) string {
	switch v.(type) {
	case map[string]any, []any:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err == nil {
			return strings.TrimRight(buf.String(), "\n")
		}
	}
	return fmt.Sprint(v)
}

func sentUnanswered(st *runner.StepRecord) bool {
	return st.Status == runner.StatusError && !strings.HasPrefix(st.Error, "not sent") && strings.Contains(st.Error, transport.SentNoAnswer)
}

func sentOrNot(detail string) string {
	if strings.Contains(detail, transport.NoAnswerBeforeTimeout) && !strings.HasPrefix(detail, "not sent") {
		return transport.NoAnswerBeforeTimeout
	}
	if strings.Contains(detail, transport.SentNoAnswer) && !strings.HasPrefix(detail, "not sent") {
		return transport.SentNoAnswer
	}
	return "not sent"
}

func orNotRecorded(s string) string {
	if s == "" {
		return "(not recorded)"
	}
	return s
}

func withKind(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("string %q", s)
	}
	return fmt.Sprintf("%s %s", jsonKind(v), show(v))
}

func (r *Report) MaskedList() string {
	var b strings.Builder
	section := func(title string, cs []Change) {
		if len(cs) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s, not compared:\n", title)
		for _, c := range cs {
			fmt.Fprintf(&b, "  %s %s (%s)%s\n", c.Step, c.Path, c.Transition(), maskSuffix(c))
		}
	}
	section("values under volatile paths", r.VolatileValues)
	section("id- or timestamp-shaped values", r.ShapeMasked)
	section("values echoing a fixture name", r.FixtureEchoed)
	return strings.TrimRight(b.String(), "\n")
}

func (r *Report) FixtureInputLine(listed bool) string {
	if len(r.FixtureInput) == 0 {
		return ""
	}
	line := fmt.Sprintf("%d request value(s) differ from the confirmed run only in a fixture name or under a volatile path, "+
		"so they are not counted as different input", len(r.FixtureInput))
	if !listed {
		return line + " (-masked lists them)"
	}
	names := []string{}
	for _, c := range r.FixtureInput {
		names = append(names, c.Step+" "+c.Path)
	}
	return line + ": " + strings.Join(names, ", ")
}

func (r *Report) QuietText() string {
	if !r.Clean() || r.PrincipalChanged() || len(r.FullyMasked) > 0 || len(r.UnapprovedVolatile) > 0 ||
		len(r.UnapprovedRedact) > 0 || len(r.RequestChanges) > 0 || len(r.RenamedSteps) > 0 {
		return r.Text()
	}
	var b strings.Builder
	if r.Chain != "" {
		fmt.Fprintf(&b, "%s: ", r.Chain)
	}
	fmt.Fprintf(&b, "no drift vs safe spot %s", r.SafeSpotID)
	return b.String()
}

func (r *Report) Text() string {
	parts := []string{}
	if r.Masked > 0 {
		parts = append(parts, fmt.Sprintf("%d id- or timestamp-shaped value(s)", r.Masked))
	}
	if r.VolatileMasked > 0 {
		parts = append(parts, fmt.Sprintf("%d value(s) under volatile paths", r.VolatileMasked))
	}
	if len(r.FixtureEchoed) > 0 {
		parts = append(parts, fmt.Sprintf("%d value(s) echoing a fixture name", len(r.FixtureEchoed)))
	}
	masked := ""
	if len(parts) > 0 && (!r.HideMasked || len(r.UnapprovedVolatile) > 0 || len(r.FullyMasked) > 0) {
		masked = " (" + strings.Join(parts, " and ") + " that differ every run were not counted)"
	}
	var b strings.Builder
	if line := RenamedLine(r.RenamedSteps, "the safe spot", "this run"); line != "" {
		b.WriteString(line + "\n")
	}
	if len(r.FullyMasked) > 0 {
		fmt.Fprintf(&b, "WARNING: every response field of step(s) %s is under a volatile pattern, so verify compared nothing "+
			"of those responses and \"no drift\" says nothing about them. Narrow the volatile patterns (a bare \"**\" masks everything)\n",
			strings.Join(r.FullyMasked, ", "))
	}
	if r.SafeSpotTarget != "" || r.RunTarget != "" {
		fmt.Fprintf(&b, "targets differ: safe spot %s, this run %s; a difference may come from the target, not from a change in the code\n",
			orNotRecorded(r.SafeSpotTarget), orNotRecorded(r.RunTarget))
	}
	if len(r.UnapprovedVolatile) > 0 {
		fmt.Fprintf(&b, "the replay was masked with %d volatile pattern(s) the safe spot %s did not approve: %s\n",
			len(r.UnapprovedVolatile), r.SafeSpotID, strings.Join(r.UnapprovedVolatile, ", "))
		if len(r.UnapprovedMasked) == 0 {
			b.WriteString("  they hid nothing this run, but they would hide a change there\n")
		} else {
			fmt.Fprintf(&b, "  they hid %d value(s) the approved mask compares:\n", len(r.UnapprovedMasked))
			for _, p := range r.UnapprovedMasked {
				fmt.Fprintf(&b, "    %s\n", p)
			}
		}
	}
	if len(r.UnapprovedRedact) > 0 {
		fmt.Fprintf(&b, "the replay was redacted with %d redact pattern(s) the safe spot %s did not have: %s\n",
			len(r.UnapprovedRedact), r.SafeSpotID, strings.Join(r.UnapprovedRedact, ", "))
		if len(r.UnapprovedRedacted) == 0 {
			b.WriteString("  they hid nothing this run (they blanked no value the safe spot holds), but a change there is not compared\n")
		} else {
			fmt.Fprintf(&b, "  they blanked %d value(s) the safe spot holds in the clear, which were not compared (not a backend change):\n", len(r.UnapprovedRedacted))
			for _, p := range r.UnapprovedRedacted {
				fmt.Fprintf(&b, "    %s\n", p)
			}
		}
	}
	if r.Redacted > 0 {
		fmt.Fprintf(&b, "%d redacted response value(s), under redact paths, are blanked in the run records and were never compared, "+
			"so a change there is invisible to verify: %s\n", r.Redacted, strings.Join(r.RedactedPaths, ", "))
	}
	if len(r.ScrubbedPaths) > 0 {
		fmt.Fprintf(&b, "%d response value(s) under no redact path were scrubbed by value, blanked in both records because they held "+
			"a secret the run knew (a credential or token it sent), and were never compared, so a change there is invisible to verify: %s\n",
			len(r.ScrubbedPaths), strings.Join(r.ScrubbedPaths, ", "))
	}
	for _, c := range r.RequestChanges {
		what := "request"
		if chainLevel(c) || c.Path == ExpectPath {
			what = "chain"
		}
		if c.Path == ExpectValuePath {
			fmt.Fprintf(&b, "expectation differs from the confirmed run at %s: %v -> %v (%s)\n", c.Step, c.Want, c.Got, c.Detail)
			continue
		}
		if c.StepOrder() {
			fmt.Fprintf(&b, "chain differs from the confirmed run in its step order: %s (was %v; now %v)\n", c.Moves(), c.Want, c.Got)
			continue
		}
		fmt.Fprintf(&b, "%s differs from the confirmed run at %s %s (%s)\n", what, c.Step, c.Path, c.Transition())
	}
	if len(r.UnsentDefaults) > 0 {
		fmt.Fprintf(&b, "%d response field(s) are declared now but were not on the wire (left at the proto3 default, the same bytes "+
			"the safe spot's backend sent), so they are not counted as a change: %s\n", len(r.UnsentDefaults), strings.Join(r.UnsentDefaults, ", "))
	}
	if len(r.UndeclaredSame) > 0 {
		fmt.Fprintf(&b, "%d response field(s) are declared now and were on the wire, undeclared, in the safe spot's run with the same value, "+
			"so they are not counted as a change: %s\n", len(r.UndeclaredSame), strings.Join(r.UndeclaredSame, ", "))
	}
	if len(r.UndeclaredUnknown) > 0 {
		fmt.Fprintf(&b, "%d response field(s) are declared now and were on the wire, undeclared, in the safe spot's run, whose build did not "+
			"record their value, so they were not compared (propose a passing run in place of the safe spot to compare them): %s\n",
			len(r.UndeclaredUnknown), strings.Join(r.UndeclaredUnknown, ", "))
	}
	if len(r.RequestChanges) > 0 {
		cause := "its input changed since it was confirmed"
		if r.OnlyChainChanged() {
			cause = "the chain changed since it was confirmed; a step, expectation or body or header reference edit is a chain change, not an input change, " +
				"and an expectation edit explains a status change at its own step only, and only when the edited expectation failed"
		}
		if r.InputCause != "" {
			cause = r.InputCause
		}
		fmt.Fprintf(&b, "%s since the safe spot's run %s: %s\n", r.InputSummary(), r.SafeSpotID, cause)
	}
	b.WriteString(r.principalCaveat())
	if r.Clean() && r.PrincipalChanged() {
		fmt.Fprintf(&b, "the responses match safe spot %s%s, but a step ran under another auth profile or principal than the confirmed run, "+
			"so the safe spot does not vouch for this principal: drift with different input", r.SafeSpotID, masked)
		return b.String()
	}
	if r.Clean() {
		if r.Chain != "" {
			fmt.Fprintf(&b, "%s: ", r.Chain)
		}
		fmt.Fprintf(&b, "no drift vs safe spot %s%s", r.SafeSpotID, masked)
		return b.String()
	}
	unexplained := len(r.Unexplained())
	mixed := len(r.RequestChanges) > 0 && unexplained > 0
	expectOnly := r.OnlyExpectationsEdited()
	switch {
	case mixed && expectOnly && unexplained == len(r.Changes):
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s: the expectation change explains none of them, since it explains only a failure of the "+
			"changed expectation itself, so they are evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	case mixed && expectOnly:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s: %d are not explained by the expectation change, which explains only its own step's "+
			"status and the steps not reached after it when the changed expectation failed, so they are evidence of a backend regression; "+
			"%d are\n", len(r.Changes), r.SafeSpotID, masked, unexplained, len(r.Changes)-unexplained)
	case mixed && r.OnlyChainChanged():
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s: %d are at steps the chain change cannot affect (not a changed, added or removed step, "+
			"after no added or removed write, and reading none of those steps), so it does not explain them and they are evidence of a "+
			"backend regression; %d it explains\n", len(r.Changes), r.SafeSpotID, masked, unexplained, len(r.Changes)-unexplained)
	case mixed:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s: %d are at steps whose input did not differ, that read no value the different input "+
			"changed and follow no write whose answer changed with it, so it does not explain them and they are evidence of a backend regression; %d it explains\n", len(r.Changes), r.SafeSpotID, masked, unexplained, len(r.Changes)-unexplained)
	case r.OnlyChainChanged():
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s, after a chain change, so they are not evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	case len(r.RequestChanges) > 0:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s, with different input, so they are not evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	default:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s%s\n", r.Counted(), r.SafeSpotID, masked, r.uncountedNote())
	}
	if r.FirstFailure != "" {
		fmt.Fprintf(&b, "  first failing step: %s\n", r.FirstFailure)
	}
	b.WriteString(r.reorderedText())
	skips := runner.NewSkipCondenser()
	foldSaid := map[string]bool{}
	renames := r.RenamedFields()
	renamedAt, renamedTo := map[int]FieldRename{}, map[int]bool{}
	for _, fr := range renames {
		renamedAt[fr.missing], renamedTo[fr.unexpected] = fr, true
	}
	changedAt := r.valueChangedSteps()
	for i := 0; i < len(r.Changes); i++ {
		c := r.Changes[i]
		if c.Kind == KindStatus && changedAt[c.Step] {
			continue
		}
		if c.Kind == KindNotReached {
			c.Detail = skips.Condense(c.Step, c.Detail)
		}
		step := c.Step
		if step == "" {
			step = "-"
		}
		if r.folded[c.Step] && c.Kind != KindNotReached {
			if !foldSaid[c.Step] {
				foldSaid[c.Step] = true
				fmt.Fprintf(&b, "  [%s] %-10s %d change(s) not listed: %s\n", step, "not_judged", r.foldedCount(c.Step), r.foldWhy)
			}
			continue
		}
		if run := sameNotReached(r.Changes[i:]); run > 1 {
			last := r.Changes[i+run-1]
			fmt.Fprintf(&b, "  [%s..%s] %-10s %d step(s) want=%v, %s (%s)\n", step, last.Step, c.Kind, run, c.Want, sentOrNot(c.Detail), c.Detail)
			i += run - 1
			continue
		}
		if r.underReordered(c) || renamedTo[i] {
			continue
		}
		if fr, ok := renamedAt[i]; ok {
			fmt.Fprintf(&b, "  [%s] %-10s %s\n", step, "renamed", fr.line())
			continue
		}
		after := ""
		if mixed && c.WithInput {
			after = " (after different input)"
			if r.OnlyChainChanged() {
				after = " (explained by the chain change)"
			}
			if expectOnly {
				after = " (explained by the failed changed expectation)"
			}
		}
		if c.StepOrder() {
			fmt.Fprintf(&b, "  [step order] moved: %s (was %v; now %v)%s\n", c.Moves(), c.Want, c.Got, after)
			continue
		}
		fmt.Fprintf(&b, "  [%s] %-10s %s %s%s\n", step, c.Kind, c.Path, c.describe(), after)
	}
	b.WriteString(renameText(renames))
	return strings.TrimRight(b.String(), "\n")
}

func failedExpectation(st *runner.StepRecord) string {
	failed := []string{}
	for _, e := range st.Expect {
		if !e.Passed {
			failed = append(failed, chain.DescribeFailure(e))
		}
	}
	if len(failed) == 0 {
		return ""
	}
	out := "expectation failed: " + failed[0]
	if len(failed) > 1 {
		out += fmt.Sprintf(" (and %d more)", len(failed)-1)
	}
	return out
}

func sameNotReached(changes []Change) int {
	first := changes[0]
	if first.Kind != KindNotReached || first.Detail == "" {
		return 0
	}
	n := 1
	for n < len(changes) {
		c := changes[n]
		if c.Kind != KindNotReached || c.Detail != first.Detail || c.Path != first.Path || fmt.Sprint(c.Want) != fmt.Sprint(first.Want) || fmt.Sprint(c.Got) != fmt.Sprint(first.Got) {
			break
		}
		n++
	}
	return n
}
