package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
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

	WithInput bool `json:"with_different_input,omitempty"`
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

	UnapprovedVolatile []string `json:"unapproved_volatile,omitempty"`
	UnapprovedMasked   []string `json:"unapproved_masked,omitempty"`
	PrincipalUnchecked []string `json:"principal_unchecked,omitempty"`
	Reordered          []string `json:"reordered_lists,omitempty"`

	inputSeparated    bool
	compared          []comparedStep
	renames           [][2]string
	reorderCandidates []stepPath
	reordered         []stepPath
}

func (r *Report) Widened() bool { return len(r.UnapprovedVolatile) > 0 }

func (r *Report) Clean() bool { return len(r.Changes) == 0 }

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
	if spot.Target != rec.Target {
		rep.SafeSpotTarget, rep.RunTarget = spot.Target, rec.Target
	}
	rep.PrincipalUnchecked = uncheckedPrincipals(spot, rec)
	masker := pathmask.NewMasker(mergePatterns(spot.Volatile, rec.Volatile, extra))
	approvedPatterns := append([]string{}, spot.Volatile...)
	for _, st := range spot.Steps {
		approvedPatterns = append(approvedPatterns, st.Volatile...)
	}
	approved := pathmask.NewMasker(approvedPatterns)
	rep.UnapprovedVolatile = unapproved(approvedPatterns, rec, extra)
	first := firstRed(rec)
	if first != nil {
		rep.FirstFailure = fmt.Sprintf("step %d %s (%s)", first.Index, first.ID, first.Status)
		if why := firstLineOf(first.Error); why != "" {
			rep.FirstFailure += ": " + why
		}
	}
	stoppedEarly := len(rec.Steps) < len(spot.Steps) && !rec.Passed()

	redactPaths := pathmask.NewMasker(rec.Redacted)
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
		if StepReached(rec, want) && !StepReached(rec, got) && got != first {
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
		if errA != nil || errB != nil {
			stepChanges = compareStep(want, got)
		} else {
			known := renamer(idRenames(idPairs))
			declared := unorderedSet(want.Unordered, got.Unordered, assumed[want.ID])
			if len(declared) > 0 {
				b = reorderUnordered(a, b, "", declared, known)
			}
			if assumed == nil {
				reorderCandidates(a, b, "", declared, known, func(p string) {
					rep.reorderCandidates = append(rep.reorderCandidates, stepPath{want.ID, p})
				})
			}
			walk(a, b, "", func(c Change) {
				c.Step = want.ID
				stepChanges = append(stepChanges, c)
			})
		}
		for _, c := range stepChanges {
			switch {
			case c.Path != "response" && maskedAt(stepMask, c):
				rep.VolatileMasked++
				rep.VolatilePaths = append(rep.VolatilePaths, c.Step+" "+c.Path)
				rep.VolatileValues = append(rep.VolatileValues, c)
				if !maskedAt(approved, c) && (c.Kind != KindChanged || !looksVolatile(c.Path, c.Want, c.Got)) {
					rep.UnapprovedMasked = append(rep.UnapprovedMasked, c.Step+" "+c.Path)
				}
			case c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got):
				rep.Masked++
				rep.ShapeMasked = append(rep.ShapeMasked, c)
			default:
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
	var renamed []Change
	rep.Changes, renamed = splitEchoes(rep.Changes, rep.compared, rep.renames)
	rep.Masked += len(renamed)
	rep.ShapeMasked = append(rep.ShapeMasked, renamed...)
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
	for _, st := range spot.Steps {
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
			out = append(out, expectChanges(st, s)...)
		}
	}
	for _, id := range nowOrder {
		if !was[id] {
			out = append(out, Change{Step: id, Path: "step", Kind: KindUnexpected, Got: now[id].Call,
				Detail: "the chain has a step the confirmed run did not"})
		}
	}
	if len(out) == 0 && strings.Join(wasOrder, ",") != strings.Join(nowOrder, ",") {
		out = append(out, Change{Step: "-", Path: "steps", Kind: KindOrder,
			Want: strings.Join(wasOrder, ", "), Got: strings.Join(nowOrder, ", ")})
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

func expectChanges(was *runner.StepRecord, now *chain.Step) []Change {
	out := []Change{}
	for i := range max(len(was.Expect), len(now.Expect)) {
		switch {
		case i >= len(now.Expect):
			out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindMissing, Want: expectText(was.Expect[i].Path, was.Expect[i].Rule, was.Expect[i].Want)})
		case i >= len(was.Expect):
			r := now.Expect[i].Evaluate(nil)
			out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindUnexpected, Got: expectText(r.Path, r.Rule, r.Want)})
		default:
			w, r := was.Expect[i], now.Expect[i].Evaluate(nil)
			if w.Rule == "unevaluated" {
				continue
			}
			same := w.Path == r.Path && w.Rule == r.Rule
			if text, templated := r.Want.(string); same && !(templated && strings.Contains(text, "${")) && fmt.Sprint(w.Want) != pathmask.MaskRedacted {
				same = fmt.Sprint(w.Want) == fmt.Sprint(r.Want)
			}
			if !same {
				out = append(out, Change{Step: was.ID, Path: ExpectPath, Kind: KindChanged, Want: expectText(w.Path, w.Rule, w.Want), Got: expectText(r.Path, r.Rule, r.Want)})
			}
		}
	}
	return out
}

func expectText(path, rule string, want any) string {
	if want == nil {
		return path + " " + rule
	}
	return fmt.Sprintf("%s %s %v", path, rule, want)
}

func CompareRequests(spot *store.SafeSpot, rec *runner.Record, derived func(step, path string) bool) []Change {
	out := []Change{}
	for i := range min(len(spot.Steps), len(rec.Steps)) {
		want, got := spot.Steps[i], rec.Steps[i]
		if want.ID != got.ID {
			continue
		}
		if want.AuthProfile != "" && got.AuthProfile != "" && want.AuthProfile != got.AuthProfile {
			out = append(out, Change{Step: want.ID, Path: AuthProfilePath, Kind: KindChanged, Want: want.AuthProfile, Got: got.AuthProfile})
		} else if want.AuthPrincipal != "" && got.AuthPrincipal != "" && want.AuthPrincipal != got.AuthPrincipal {
			out = append(out, Change{Step: want.ID, Path: AuthPrincipalPath, Kind: KindChanged, Want: want.AuthPrincipal, Got: got.AuthPrincipal,
				Detail: fmt.Sprintf("auth profile %q logged in as another principal: its login body's non-secret fields differ", got.AuthProfile)})
		}
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
			c.Step = want.ID
			out = append(out, c)
		})
	}
	return out
}

func uncheckedPrincipals(spot *store.SafeSpot, rec *runner.Record) []string {
	out := []string{}
	for i := range min(len(spot.Steps), len(rec.Steps)) {
		want, got := spot.Steps[i], rec.Steps[i]
		if want.ID != got.ID || want.AuthPrincipal != "" || got.AuthPrincipal == "" {
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

func (r *Report) inputCounts() (int, int) {
	requests, edits := 0, 0
	for _, c := range r.RequestChanges {
		if chainLevel(c) || c.Path == ExpectPath {
			edits++
		} else {
			requests++
		}
	}
	return requests, edits
}

func (r *Report) OnlyChainChanged() bool {
	requests, edits := r.inputCounts()
	return requests == 0 && edits > 0
}

func (r *Report) InputSummary() string {
	requests, edits := r.inputCounts()
	parts := []string{}
	if requests > 0 {
		parts = append(parts, fmt.Sprintf("%d request value(s) differ", requests))
	}
	if edits > 0 {
		parts = append(parts, fmt.Sprintf("%d chain change(s)", edits))
	}
	return strings.Join(parts, " and ")
}

func (c Change) Transition() string {
	switch c.Kind {
	case KindMissing:
		return fmt.Sprintf("%v -> absent", c.Want)
	case KindUnexpected:
		return fmt.Sprintf("absent -> %v", c.Got)
	case KindLength:
		return fmt.Sprintf("%v item(s) -> %v item(s)", c.Want, c.Got)
	case KindType:
		return fmt.Sprintf("%s -> %s", withKind(c.Want), withKind(c.Got))
	}
	return fmt.Sprintf("%v -> %v", c.Want, c.Got)
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

func (c Change) describe() string {
	out := c.describeValues()
	if c.Detail != "" {
		out += " (" + c.Detail + ")"
	}
	return out
}

func (c Change) describeValues() string {
	if c.Kind == KindNotReached {
		if c.Got == nil {
			return fmt.Sprintf("want=%v got=not recorded", c.Want)
		}
		return fmt.Sprintf("want=%v got=%v, %s", c.Want, c.Got, sentOrNot(c.Detail))
	}
	if c.Kind == KindLength {
		return fmt.Sprintf("want=%v item(s) got=%v item(s)", c.Want, c.Got)
	}
	if c.Path == "step" && c.Kind == KindMissing {
		return fmt.Sprintf("want=%v got=absent", c.Want)
	}
	if c.Path == "step" && c.Kind == KindUnexpected {
		return fmt.Sprintf("want=absent got=%v", c.Got)
	}
	if c.Kind != KindType {
		return fmt.Sprintf("want=%v got=%v", c.Want, c.Got)
	}
	return fmt.Sprintf("want=%s got=%s", withKind(c.Want), withKind(c.Got))
}

func sentOrNot(detail string) string {
	if strings.Contains(detail, transport.NoAnswerBeforeTimeout) && !strings.HasPrefix(detail, "not sent") {
		return transport.NoAnswerBeforeTimeout
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
	return fmt.Sprintf("%s %v", jsonKind(v), v)
}

func (r *Report) MaskedList() string {
	var b strings.Builder
	section := func(title string, cs []Change) {
		if len(cs) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s, not compared:\n", title)
		for _, c := range cs {
			fmt.Fprintf(&b, "  %s %s (%s)\n", c.Step, c.Path, c.Transition())
		}
	}
	section("values under volatile paths", r.VolatileValues)
	section("id- or timestamp-shaped values", r.ShapeMasked)
	section("values echoing a fixture name", r.FixtureEchoed)
	return strings.TrimRight(b.String(), "\n")
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
	if len(parts) > 0 {
		masked = " (" + strings.Join(parts, " and ") + " that differ every run were not counted)"
	}
	var b strings.Builder
	if len(r.FullyMasked) > 0 {
		fmt.Fprintf(&b, "WARNING: every response field of step(s) %s is under a volatile pattern, so verify compared nothing "+
			"of those responses and \"no drift\" says nothing about them. Narrow the volatile patterns (a bare \"**\" masks everything)\n",
			strings.Join(r.FullyMasked, ", "))
	}
	if r.SafeSpotTarget != "" || r.RunTarget != "" {
		fmt.Fprintf(&b, "targets differ: safe spot %s, this run %s; a difference may come from the target, not from a change in the code\n",
			orNotRecorded(r.SafeSpotTarget), orNotRecorded(r.RunTarget))
	}
	if r.Widened() {
		fmt.Fprintf(&b, "the replay was masked with %d volatile pattern(s) the safe spot %s did not approve: %s\n",
			len(r.UnapprovedVolatile), r.SafeSpotID, strings.Join(r.UnapprovedVolatile, ", "))
		if len(r.UnapprovedMasked) == 0 {
			b.WriteString("  they hid no value this time, but they would hide a change there\n")
		} else {
			fmt.Fprintf(&b, "  they hid %d value(s) the approved mask compares:\n", len(r.UnapprovedMasked))
			for _, p := range r.UnapprovedMasked {
				fmt.Fprintf(&b, "    %s\n", p)
			}
		}
	}
	if r.Redacted > 0 {
		fmt.Fprintf(&b, "%d redacted response value(s), under redact paths, are blanked in both records and were never compared, "+
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
		fmt.Fprintf(&b, "%s differs from the confirmed run at %s %s (%s)\n", what, c.Step, c.Path, c.Transition())
	}
	if len(r.FixtureInput) > 0 {
		names := []string{}
		for _, c := range r.FixtureInput {
			names = append(names, c.Step+" "+c.Path)
		}
		fmt.Fprintf(&b, "%d request value(s) differ from the confirmed run only in a fixture name (a var inside other text, `sku-${vars.tag}`) "+
			"or under a volatile path, so they are not counted as different input: %s\n", len(r.FixtureInput), strings.Join(names, ", "))
	}
	if len(r.RequestChanges) > 0 {
		cause := "its input changed since it was confirmed"
		if r.OnlyChainChanged() {
			cause = "the chain changed since it was confirmed; a step or expectation edit is a chain change, not an input change, " +
				"and an expectation edit explains a status change at its own step only"
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
		fmt.Fprintf(&b, "no drift vs safe spot %s%s", r.SafeSpotID, masked)
		return b.String()
	}
	unexplained := len(r.Unexplained())
	mixed := len(r.RequestChanges) > 0 && unexplained > 0
	switch {
	case mixed:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s: %d come before any step whose input differs, so the different input does not explain them "+
			"and they are evidence of a backend regression; %d come at or after it\n", len(r.Changes), r.SafeSpotID, masked, unexplained, len(r.Changes)-unexplained)
	case r.OnlyChainChanged():
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s, after a chain change, so they are not evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	case len(r.RequestChanges) > 0:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s, with different input, so they are not evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	default:
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s\n", len(r.Changes), r.SafeSpotID, masked)
	}
	if r.FirstFailure != "" {
		fmt.Fprintf(&b, "  first failing step: %s\n", r.FirstFailure)
	}
	b.WriteString(r.reorderedText())
	for i := 0; i < len(r.Changes); i++ {
		c := r.Changes[i]
		step := c.Step
		if step == "" {
			step = "-"
		}
		if run := sameNotReached(r.Changes[i:]); run > 1 {
			last := r.Changes[i+run-1]
			fmt.Fprintf(&b, "  [%s..%s] %-10s %d step(s) want=%v, %s (%s)\n", step, last.Step, c.Kind, run, c.Want, sentOrNot(c.Detail), c.Detail)
			i += run - 1
			continue
		}
		after := ""
		if mixed && c.WithInput {
			after = " (after different input)"
		}
		fmt.Fprintf(&b, "  [%s] %-10s %s %s%s\n", step, c.Kind, c.Path, c.describe(), after)
	}
	return strings.TrimRight(b.String(), "\n")
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
