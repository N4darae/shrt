package main

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func latencyPolicy(e *env) diff.LatencyPolicy {
	return diff.LatencyPolicyFrom(e.cfg.Latency)
}

func withLatency(opts runner.Options, p diff.LatencyPolicy, spot *store.SafeSpot) runner.Options {
	if spot == nil || p.Off {
		return opts
	}
	opts.LatencySuspect = p.Suspect(spot.Steps)
	opts.Remeasure = p.Remeasure
	return opts
}

func latencyFlags(e *env, spot *store.SafeSpot, rec *runner.Record, p diff.LatencyPolicy) []diff.LatencyFlag {
	if spot == nil || rec == nil || rec.DryRun {
		return nil
	}
	return diff.LatencyRegressions(spot.Steps, rec, previousRunOf(e, rec), p)
}

func previousRunOf(e *env, rec *runner.Record) *runner.Record {
	ids, _ := e.store.ListRuns(rec.Chain)
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		prev, err := loadRunNamedAs(e, rec, ids[i])
		if err != nil || prev.DryRun || !ranBefore(prev, rec) {
			continue
		}
		return prev
	}
	return nil
}

func confirmedLatency(flags []diff.LatencyFlag) []diff.LatencyFlag {
	var out []diff.LatencyFlag
	for _, f := range flags {
		if f.Confirmed {
			out = append(out, f)
		}
	}
	return out
}

func latencyFailure(name string, flags []diff.LatencyFlag, p diff.LatencyPolicy) error {
	slow := confirmedLatency(flags)
	if !p.Fail || len(slow) == 0 {
		return nil
	}
	steps := make([]string, len(slow))
	for i, f := range slow {
		steps[i] = fmt.Sprintf("%s %dms -> %dms", f.Step, f.BeforeMS, f.AfterMS)
	}
	return fmt.Errorf("latency regression in %s: %d step(s) %s, confirmed by re-measurement or the previous run (%s); "+
		"latency.fail is set in .shrt/config.yaml, so this fails verify", name, len(slow), p.Describe(), strings.Join(steps, ", "))
}
