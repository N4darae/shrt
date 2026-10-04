package main

import (
	"fmt"
	"iter"
	"slices"
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
	for prev := range newestRuns(e, rec.Chain, rec.RunID) {
		if ranBefore(prev, rec) {
			return namedAs(prev, rec)
		}
	}
	return nil
}

func newestRuns(e *env, chainName string, skip ...string) iter.Seq[*runner.Record] {
	return func(yield func(*runner.Record) bool) {
		ids, _ := e.store.ListRuns(chainName)
		for i := len(ids) - 1; i >= 0; i-- {
			if slices.Contains(skip, ids[i]) {
				continue
			}
			if rec, err := e.store.LoadRun(chainName, ids[i]); err == nil && !rec.DryRun && !yield(rec) {
				return
			}
		}
	}
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
