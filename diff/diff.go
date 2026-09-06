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
	KindStatus     = "status"
)

type Change struct {
	Step string `json:"step,omitempty"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Want any    `json:"want,omitempty"`
	Got  any    `json:"got,omitempty"`
}

type Report struct {
	Chain      string   `json:"chain"`
	SafeSpotID string   `json:"safe_spot_run_id"`
	RunID      string   `json:"run_id"`
	Changes    []Change `json:"changes"`
}

func (r *Report) Clean() bool { return len(r.Changes) == 0 }

func Compare(spot *store.SafeSpot, rec *runner.Record) *Report {
	rep := &Report{Chain: spot.Chain, SafeSpotID: spot.RunID, RunID: rec.RunID}
	masker := pathmask.NewMasker(append(append([]string{}, spot.Volatile...), rec.Volatile...))

	if len(spot.Steps) != len(rec.Steps) {
		rep.Changes = append(rep.Changes, Change{
			Path: "steps", Kind: KindOrder,
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
		if want.Status != got.Status {
			rep.Changes = append(rep.Changes, Change{
				Step: want.ID, Path: "status", Kind: KindStatus,
				Want: want.Status, Got: got.Status,
			})
		}
		stepMask := pathmask.NewMasker(mergePatterns(masker.Patterns(), want.Volatile, got.Volatile))
		rep.Changes = append(rep.Changes, compareStep(want, got, stepMask)...)
	}
	return rep
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
			emit(Change{Path: pathOr(path), Kind: KindOrder, Want: len(w), Got: len(g)})
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
	if r.Clean() {
		return fmt.Sprintf("no drift vs safe spot %s", r.SafeSpotID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d change(s) vs safe spot %s\n", len(r.Changes), r.SafeSpotID)
	for _, c := range r.Changes {
		step := c.Step
		if step == "" {
			step = "-"
		}
		fmt.Fprintf(&b, "  [%s] %-10s %s %s\n", step, c.Kind, c.Path, c.describe())
	}
	return strings.TrimRight(b.String(), "\n")
}
