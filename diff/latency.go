package diff

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

type LatencyPolicy struct {
	FloorMS   int64
	Ratio     float64
	Remeasure int
	Fail      bool
	Off       bool
}

func LatencyPolicyFrom(l *config.Latency) LatencyPolicy {
	p := LatencyPolicy{FloorMS: config.DefaultLatencyFloorMS, Ratio: config.DefaultLatencyRatio, Remeasure: config.DefaultLatencyRemeasure}
	if l == nil {
		return p
	}
	if l.FloorMS != nil {
		p.FloorMS = *l.FloorMS
	}
	if l.Ratio != nil {
		p.Ratio = *l.Ratio
	}
	if l.Remeasure != nil {
		p.Remeasure = *l.Remeasure
	}
	p.Fail, p.Off = l.Fail, l.Off
	return p
}

func (p LatencyPolicy) Exceeds(before, after int64) bool {
	if p.Off {
		return false
	}
	base := before
	if base < 1 {
		base = 1
	}
	return after-before >= p.FloorMS && float64(after) >= p.Ratio*float64(base)
}

func (p LatencyPolicy) Describe() string {
	return fmt.Sprintf("at least +%dms and %gx the safe spot's run", p.FloorMS, p.Ratio)
}

func (p LatencyPolicy) Suspect(spot []*runner.StepRecord) func(string, int64) bool {
	if p.Off {
		return nil
	}
	base := map[string]int64{}
	for _, st := range spot {
		if latencyMeasured(st) {
			base[st.ID] = st.LatencyMS
		}
	}
	return func(id string, ms int64) bool {
		before, ok := base[id]
		return ok && p.Exceeds(before, ms)
	}
}

type LatencyFlag struct {
	Step      string  `json:"step"`
	Call      string  `json:"call"`
	BeforeMS  int64   `json:"before_ms"`
	AfterMS   int64   `json:"after_ms"`
	Samples   []int64 `json:"samples_ms"`
	Confirmed bool    `json:"confirmed"`
	How       string  `json:"how"`
	Against   string  `json:"against,omitempty"`
}

func latencyMeasured(st *runner.StepRecord) bool {
	return st != nil && st.Status != runner.StatusSkipped && st.HTTPStatus != 0
}

func LatencyRegressions(spot []*runner.StepRecord, rec *runner.Record, prev *runner.Record, p LatencyPolicy) []LatencyFlag {
	if p.Off || rec == nil {
		return nil
	}
	before := map[string]*runner.StepRecord{}
	for _, st := range spot {
		if latencyMeasured(st) {
			before[st.ID] = st
		}
	}
	var out []LatencyFlag
	for _, st := range rec.Steps {
		was, ok := before[st.ID]
		if !latencyMeasured(st) || !ok || was.Call != st.Call {
			continue
		}
		samples := append([]int64{st.LatencyMS}, st.LatencyResent...)
		after := slices.Min(samples)
		if !p.Exceeds(was.LatencyMS, after) {
			continue
		}
		f := LatencyFlag{Step: st.ID, Call: st.Call, BeforeMS: was.LatencyMS, AfterMS: after, Samples: samples}
		switch {
		case len(st.LatencyResent) > 0:
			f.Confirmed = true
			f.How = fmt.Sprintf("re-sent %d more time(s), every answer slow: %s", len(st.LatencyResent), msList(samples))
		default:
			if old, ok := prevStep(prev, st); ok && p.Exceeds(was.LatencyMS, old.LatencyMS) {
				f.Confirmed = true
				f.How = fmt.Sprintf("measured once (a write is not re-sent); the previous run %s was slow there too (%dms)", prev.RunID, old.LatencyMS)
			} else {
				f.How = "measured once (a write is not re-sent) and the previous run was not slow there; re-run to confirm"
			}
		}
		out = append(out, f)
	}
	return out
}

func prevStep(prev *runner.Record, st *runner.StepRecord) (*runner.StepRecord, bool) {
	if prev == nil {
		return nil, false
	}
	old, ok := prev.Step(st.ID)
	if !ok || !latencyMeasured(old) || old.Call != st.Call {
		return nil, false
	}
	return old, true
}

func msList(xs []int64) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%dms", x)
	}
	return strings.Join(parts, ", ")
}

func (f LatencyFlag) Line() string {
	ratio := float64(f.AfterMS)
	if f.BeforeMS > 0 {
		ratio = float64(f.AfterMS) / float64(f.BeforeMS)
	}
	head := "LATENCY"
	if !f.Confirmed {
		head = "LATENCY (unconfirmed)"
	}
	against := f.Against
	if against == "" {
		against = "the safe spot's run"
	}
	return fmt.Sprintf("%s: %s at step %s took %dms, %s %dms (+%dms, %.1fx); %s",
		head, chain.RPCName(f.Call), f.Step, f.AfterMS, against, f.BeforeMS, f.AfterMS-f.BeforeMS, ratio, f.How)
}

func LatencyTable(spot []*runner.StepRecord, rec *runner.Record, p LatencyPolicy) string {
	base := map[string]*runner.StepRecord{}
	for _, st := range spot {
		base[st.ID] = st
	}
	var b strings.Builder
	fmt.Fprintf(&b, "latency per step, this run against the safe spot's run (flagged when %s):\n", p.Describe())
	for _, st := range rec.Steps {
		was, ok := base[st.ID]
		if !ok || !latencyMeasured(was) || !latencyMeasured(st) {
			fmt.Fprintf(&b, "  %-28s %-24s not compared (not answered in both runs)\n", st.ID, chain.RPCName(st.Call))
			continue
		}
		samples := append([]int64{st.LatencyMS}, st.LatencyResent...)
		after := slices.Min(samples)
		mark := ""
		if p.Exceeds(was.LatencyMS, after) {
			mark = "  slow"
		}
		extra := ""
		if len(st.LatencyResent) > 0 {
			extra = " (re-sent: " + msList(st.LatencyResent) + ")"
		}
		fmt.Fprintf(&b, "  %-28s %-24s %6dms -> %6dms%s%s\n", st.ID, chain.RPCName(st.Call), was.LatencyMS, after, extra, mark)
	}
	return strings.TrimRight(b.String(), "\n")
}
