package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
)

func blockedBy(r chain.ExpectResult) (string, bool) {
	if r.Passed || r.Rule != "unevaluated" {
		return "", false
	}
	m := diff.HeldBackProducer.FindStringSubmatch(r.Detail)
	if m == nil {
		return "", false
	}
	return m[1], true
}

type blockedRead struct {
	index    int
	path     string
	upstream string
	eval     *chain.ExpectResult
}

func blockedReads(source, replay chain.Verdict) []blockedRead {
	out := []blockedRead{}
	aligned := len(source.Expect) == len(replay.Expect)
	for i, e := range source.Expect {
		upstream, ok := blockedBy(e)
		if !ok {
			continue
		}
		b := blockedRead{index: i, path: e.Path, upstream: upstream}
		if aligned && replay.Expect[i].Path == e.Path && replay.Expect[i].Rule != "unevaluated" {
			r := replay.Expect[i]
			b.eval = &r
		}
		out = append(out, b)
	}
	return out
}

func (b blockedRead) line(sourceRun string) string {
	head := fmt.Sprintf("%s not evaluated in source run %s: it reads step %s, which failed there", b.path, sourceRun, b.upstream)
	if b.eval == nil {
		return head + "; the slice did not evaluate it either"
	}
	outcome := "passed"
	if !b.eval.Passed {
		outcome = "FAILED"
	}
	return fmt.Sprintf("%s; the slice evaluated it: %s %s %s (want %s, got %s)",
		head, b.eval.Path, b.eval.Rule, outcome, quoted(b.eval.Want), quoted(b.eval.Got))
}

func withoutBlocked(source, replay chain.Verdict, blocked []blockedRead) chain.Verdict {
	out := replay
	out.Expect = append([]chain.ExpectResult{}, replay.Expect...)
	at := map[int]bool{}
	for _, b := range blocked {
		out.Expect[b.index] = source.Expect[b.index]
		at[b.index] = true
	}
	onlyBlocked := true
	for i, e := range source.Expect {
		if !e.Passed && !at[i] {
			onlyBlocked = false
		}
	}
	for i, r := range replay.Expect {
		if !r.Passed && !at[i] {
			onlyBlocked = false
		}
	}
	if onlyBlocked {
		out.Status = source.Status
	}
	return out
}

func blockedReason(sourceRun string, blocked []blockedRead) string {
	lines := []string{}
	upstream := []string{}
	for _, b := range blocked {
		lines = append(lines, b.line(sourceRun))
		if !slices.Contains(upstream, b.upstream) {
			upstream = append(upstream, b.upstream)
		}
	}
	return strings.Join(lines, "\n") + fmt.Sprintf("\nthe source run has no verdict on them to reproduce: %s held them back", strings.Join(upstream, ", "))
}

func blockedNote(chainRef, step, run string, f chain.ExpectResult) (string, bool) {
	upstream, ok := blockedBy(f)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("not evaluated: it reads step %s, which failed in run %s; evaluate it on its own, with %s relaxed: "+
		"shrt chain slice %s -step %s -run %s -verify", upstream, run, upstream, chainRef, step, run), true
}
