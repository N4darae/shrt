package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func gitWhere(root string) (string, string) {
	out := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		b, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	branch := out("rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = ""
	}
	return branch, out("rev-parse", "--short", "HEAD")
}

func proposalChainMatches(e *env, p *store.Proposal) error {
	rec, err := e.store.LoadRun(p.Chain, p.RunID)
	if err != nil || rec.ChainDigest == "" {
		return nil
	}
	c, err := chain.Resolve(e.chainsDir(), p.Chain)
	if err != nil {
		return fmt.Errorf("refusing to approve %s: run %s was proposed from a chain file that does not load here (%v); "+
			"check out the branch the proposal came from, or run the chain as it is here and propose that run", p.Chain, p.RunID, err)
	}
	now := c.Digest()
	if now == rec.ChainDigest {
		return nil
	}
	where := ""
	if on := p.ProposedOn(); on != "" {
		where = ", proposed on " + strings.ReplaceAll(on, "`", "")
		if branch, commit := gitWhere(e.cfg.Root); branch != "" || commit != "" {
			where += "; checked out now: " + strings.TrimSpace(branch+" "+commit)
		}
	}
	return fmt.Errorf("refusing to approve %s: the chain file is not the one run %s ran (chain digest %s, now %s%s):\n  %s\n"+
		"approving would baseline a chain this checkout does not have, and the next verify would report drift with different input. "+
		"Check out the branch the proposal came from and approve there, or run the chain as it is now and propose that run "+
		"(shrt confirm %s -note \"...\"; -supersede when a safe spot exists), then approve",
		p.Chain, p.RunID, rec.ChainDigest, now, where, strings.Join(proposalChainDifferences(e, rec, c), "\n  "), p.Chain)
}

func proposalChainDifferences(e *env, rec *runner.Record, c *chain.Chain) []string {
	out := []string{}
	named := map[string]bool{}
	for _, ch := range diff.ChainChanges(&store.SafeSpot{Chain: rec.Chain, RunID: rec.RunID, Steps: rec.Steps}, c) {
		if ch.StepOrder() {
			out = append(out, "step order: "+ch.Moves())
			continue
		}
		named[ch.Step] = true
		out = append(out, fmt.Sprintf("step %s %s: %s", ch.Step, ch.Path, ch.Transition()))
	}
	isDefault := protoDefault(e, true)
	for _, st := range rec.Steps {
		s, ok := c.Step(st.ID)
		if !ok || named[st.ID] {
			continue
		}
		if why := stepDiffers(st, s, c, isDefault); why != "" {
			out = append(out, "step "+st.ID+": "+why)
		}
	}
	if len(out) == 0 {
		out = append(out, "it differs somewhere a run record does not show, such as vars defaults, a template that resolves to "+
			"the same value, allow_fail, export, redact, kept_red, unordered or a description")
	}
	return out
}
