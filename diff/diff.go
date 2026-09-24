package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
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
}

type Report struct {
	Chain          string   `json:"chain"`
	SafeSpotID     string   `json:"safe_spot_run_id"`
	RunID          string   `json:"run_id"`
	FirstFailure   string   `json:"first_failure,omitempty"`
	RequestChanges []Change `json:"request_changes,omitempty"`
	Changes        []Change `json:"changes"`
	Masked         int      `json:"masked"`
}

func (r *Report) Clean() bool { return len(r.Changes) == 0 }

func Compare(spot *store.SafeSpot, rec *runner.Record) *Report {
	return CompareMasking(spot, rec, nil)
}

func CompareMasking(spot *store.SafeSpot, rec *runner.Record, extra []string) *Report {
	rep := &Report{Chain: spot.Chain, SafeSpotID: spot.RunID, RunID: rec.RunID}
	masker := pathmask.NewMasker(mergePatterns(spot.Volatile, rec.Volatile, extra))
	first := firstRed(rec)
	if first != nil {
		rep.FirstFailure = fmt.Sprintf("step %d %s (%s)", first.Index, first.ID, first.Status)
		if why := firstLineOf(first.Error); why != "" {
			rep.FirstFailure += ": " + why
		}
	}
	stoppedEarly := len(rec.Steps) < len(spot.Steps) && !rec.Passed()

	if len(spot.Steps) != len(rec.Steps) && !stoppedEarly {
		rep.Changes = append(rep.Changes, Change{
			Path: "steps", Kind: KindLength,
			Want: len(spot.Steps), Got: len(rec.Steps),
		})
	}
	n := min(len(spot.Steps), len(rec.Steps))
	for i := range n {
		want, got := spot.Steps[i], rec.Steps[i]
		if want.ID != got.ID || want.Call != got.Call {
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
		stepMask := pathmask.NewMasker(mergePatterns(masker.Patterns(), want.Volatile, got.Volatile))
		for _, c := range compareStep(want, got, stepMask) {
			if c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got) {
				rep.Masked++
				continue
			}
			rep.Changes = append(rep.Changes, c)
		}
	}
	if stoppedEarly {
		why := "the run stopped before this step"
		if first != nil {
			why = fmt.Sprintf("the run stopped at step %d %s", first.Index, first.ID)
		}
		for _, want := range spot.Steps[n:] {
			rep.Changes = append(rep.Changes, Change{
				Step: want.ID, Path: "status", Kind: KindNotReached, Want: want.Status, Detail: why,
			})
		}
	}
	return rep
}

func CompareWithRequests(spot *store.SafeSpot, rec *runner.Record, extra []string, derived func(step, path string) bool) *Report {
	rep := CompareMasking(spot, rec, extra)
	rep.RequestChanges = CompareRequests(spot, rec, derived)
	return rep
}

func CompareRequests(spot *store.SafeSpot, rec *runner.Record, derived func(step, path string) bool) []Change {
	out := []Change{}
	for i := range min(len(spot.Steps), len(rec.Steps)) {
		want, got := spot.Steps[i], rec.Steps[i]
		if want.ID != got.ID || len(want.Request) == 0 || len(got.Request) == 0 {
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

func compareStep(want, got *runner.StepRecord, masker *pathmask.Masker) []Change {
	a, errA := decode(want.Response)
	b, errB := decode(got.Response)
	if errA != nil || errB != nil {
		return []Change{{Step: want.ID, Path: "response", Kind: KindType, Want: errA, Got: errB}}
	}
	changes := []Change{}
	walk(masker.Apply(a), masker.Apply(b), "", func(c Change) {
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
		return fmt.Sprintf("want=%v got=%v, not sent", c.Want, c.Got)
	}
	if c.Kind == KindLength {
		return fmt.Sprintf("want=%v item(s) got=%v item(s)", c.Want, c.Got)
	}
	if c.Kind != KindType {
		return fmt.Sprintf("want=%v got=%v", c.Want, c.Got)
	}
	return fmt.Sprintf("want=%s got=%s", withKind(c.Want), withKind(c.Got))
}

func withKind(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("string %q", s)
	}
	return fmt.Sprintf("%s %v", jsonKind(v), v)
}

func (r *Report) Text() string {
	masked := ""
	if r.Masked > 0 {
		masked = fmt.Sprintf(" (%d id- or timestamp-shaped value(s) that differ every run were not counted)", r.Masked)
	}
	var b strings.Builder
	for _, c := range r.RequestChanges {
		fmt.Fprintf(&b, "request differs from the confirmed run at %s %s (%s)\n", c.Step, c.Path, c.Transition())
	}
	if len(r.RequestChanges) > 0 {
		fmt.Fprintf(&b, "the chain now sends %d request value(s) the safe spot's run %s did not send: its input changed since it was confirmed\n",
			len(r.RequestChanges), r.SafeSpotID)
	}
	if r.Clean() {
		fmt.Fprintf(&b, "no drift vs safe spot %s%s", r.SafeSpotID, masked)
		return b.String()
	}
	if len(r.RequestChanges) > 0 {
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s, with different input, so they are not evidence of a backend regression\n", len(r.Changes), r.SafeSpotID, masked)
	} else {
		fmt.Fprintf(&b, "%d change(s) vs safe spot %s%s\n", len(r.Changes), r.SafeSpotID, masked)
	}
	if r.FirstFailure != "" {
		fmt.Fprintf(&b, "  first failing step: %s\n", r.FirstFailure)
	}
	for _, c := range r.Changes {
		step := c.Step
		if step == "" {
			step = "-"
		}
		fmt.Fprintf(&b, "  [%s] %-10s %s %s\n", step, c.Kind, c.Path, c.describe())
	}
	return strings.TrimRight(b.String(), "\n")
}
