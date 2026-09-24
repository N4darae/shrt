package diff

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

const RunComparisonNote = "this compares two recorded runs with each other; it is not a verdict against a confirmed safe spot (that is 'shrt verify')"

type StepStatus struct {
	Step string `json:"step"`
	A    string `json:"a"`
	B    string `json:"b"`
}

type RunReport struct {
	Note            string       `json:"note"`
	Chain           string       `json:"chain"`
	RunA            string       `json:"run_a"`
	RunB            string       `json:"run_b"`
	StatusA         string       `json:"status_a"`
	StatusB         string       `json:"status_b"`
	TargetA         string       `json:"target_a,omitempty"`
	TargetB         string       `json:"target_b,omitempty"`
	FirstFailureA   string       `json:"first_failure_a,omitempty"`
	FirstFailureB   string       `json:"first_failure_b,omitempty"`
	StatusChanges   []StepStatus `json:"status_changes,omitempty"`
	NoLongerReached []string     `json:"no_longer_reached,omitempty"`
	NewlyReached    []string     `json:"newly_reached,omitempty"`
	Changes         []Change     `json:"changes,omitempty"`
	Masked          int          `json:"masked"`
}

func (r *RunReport) Same() bool {
	return r.StatusA == r.StatusB && r.FirstFailureA == r.FirstFailureB &&
		len(r.StatusChanges) == 0 && len(r.NoLongerReached) == 0 &&
		len(r.NewlyReached) == 0 && len(r.Changes) == 0
}

func CompareRuns(a, b *runner.Record) *RunReport {
	rep := &RunReport{
		Note: RunComparisonNote, Chain: a.Chain,
		RunA: a.RunID, RunB: b.RunID, StatusA: a.Status, StatusB: b.Status,
		FirstFailureA: firstFailure(a), FirstFailureB: firstFailure(b),
	}
	if a.Target != b.Target {
		rep.TargetA, rep.TargetB = a.Target, b.Target
	}
	byID := map[string]*runner.StepRecord{}
	for _, s := range b.Steps {
		byID[s.ID] = s
	}
	inA := map[string]bool{}
	base := mergePatterns(a.Volatile, b.Volatile)
	for _, sa := range a.Steps {
		inA[sa.ID] = true
		sb, ok := byID[sa.ID]
		if !ok {
			rep.NoLongerReached = append(rep.NoLongerReached, sa.ID)
			continue
		}
		if sa.Status != sb.Status {
			rep.StatusChanges = append(rep.StatusChanges, StepStatus{Step: sa.ID, A: sa.Status, B: sb.Status})
		}
		masker := pathmask.NewMasker(mergePatterns(base, sa.Volatile, sb.Volatile))
		rep.compareResponses(sa, sb, masker)
	}
	for _, sb := range b.Steps {
		if !inA[sb.ID] {
			rep.NewlyReached = append(rep.NewlyReached, sb.ID)
		}
	}
	return rep
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
	walk(x, y, "", func(c Change) {
		if underMask(masker, c.Path) || (c.Kind == KindChanged && looksVolatile(c.Path, c.Want, c.Got)) {
			r.Masked++
			return
		}
		c.Step = sa.ID
		r.Changes = append(r.Changes, c)
	})
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

func looksVolatile(path string, a, b any) bool {
	key := lastKey(path)
	lower := strings.ToLower(key)
	switch {
	case lower == "id", lower == "ids", lower == "token", lower == "access_token", lower == "idempotency_key",
		strings.HasSuffix(lower, "_id"), strings.HasSuffix(lower, "_ids"),
		strings.HasSuffix(lower, "_at"), strings.HasSuffix(lower, "_time"), strings.Contains(lower, "timestamp"),
		camelSuffix(key, "Id"), camelSuffix(key, "Ids"), camelSuffix(key, "At"), camelSuffix(key, "Time"):
		return true
	}
	return bothAre(a, b, isTimestamp) || bothAre(a, b, uuidShape.MatchString)
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
	fmt.Fprintf(&b, "diff of %s: run A %s (%s) vs run B %s (%s)\n", r.Chain, r.RunA, r.StatusA, r.RunB, r.StatusB)
	fmt.Fprintf(&b, "%s\n", r.Note)
	if r.TargetA != "" || r.TargetB != "" {
		fmt.Fprintf(&b, "\ntargets differ: A %s, B %s\n", r.TargetA, r.TargetB)
	}
	if r.FirstFailureA != r.FirstFailureB {
		fmt.Fprintf(&b, "\nfirst failing step moved: A %s, B %s\n", orNone(r.FirstFailureA), orNone(r.FirstFailureB))
	} else if r.FirstFailureA != "" {
		fmt.Fprintf(&b, "\nfirst failing step unchanged: %s\n", r.FirstFailureA)
	}
	if len(r.StatusChanges) > 0 {
		b.WriteString("\nstep status changes (A -> B):\n")
		for _, s := range r.StatusChanges {
			fmt.Fprintf(&b, "  %s  %s -> %s\n", s.Step, s.A, s.B)
		}
	}
	if len(r.NoLongerReached) > 0 {
		fmt.Fprintf(&b, "\nreached in A, not reached in B: %s\n", strings.Join(r.NoLongerReached, ", "))
	}
	if len(r.NewlyReached) > 0 {
		fmt.Fprintf(&b, "\nreached in B, not reached in A: %s\n", strings.Join(r.NewlyReached, ", "))
	}
	if len(r.Changes) > 0 {
		fmt.Fprintf(&b, "\n%d response difference(s) in steps both runs reached (a = run A, b = run B):\n", len(r.Changes))
		for _, c := range r.Changes {
			fmt.Fprintf(&b, "  [%s] %-10s %s %s\n", c.Step, c.Kind, c.Path, c.describeRuns())
		}
	}
	if r.Masked > 0 {
		fmt.Fprintf(&b, "\n%d differing value(s) not shown: declared volatile paths, ids and timestamps differ every run\n", r.Masked)
	}
	if r.Same() {
		b.WriteString("\nno differences between the two runs\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c Change) describeRuns() string {
	if c.Kind != KindType {
		return fmt.Sprintf("a=%v b=%v", c.Want, c.Got)
	}
	return fmt.Sprintf("a=%s b=%s", withKind(c.Want), withKind(c.Got))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
