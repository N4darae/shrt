package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func proposeAll(e *env, note, by string) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w: pass -note with what you inspected in the responses and why they are correct, not only that the runs are green", store.ErrNoEvidence)
	}
	chains, _, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil {
		return err
	}
	e.store.Notes = nil
	branch, commit := gitWhere(e.cfg.Root)
	briefs := []string{}
	failed := 0
	for _, c := range chains {
		rec, reason := proposable(e, c)
		hasSpot := e.store.HasSafeSpot(c.Name)
		var replaced []store.Differ
		if reason == "" && hasSpot {
			if replaced = differsFromSafeSpot(e, rec); len(replaced) == 0 {
				reason = "safe spot unchanged"
			}
		}
		if reason != "" {
			fmt.Printf("skip     %s: %s\n", c.Name, reason)
			continue
		}
		comparedTo, unstable, carried := unstableFields(e, rec)
		p, err := e.store.Propose(rec, store.ProposalInput{By: by, Checked: note, Supersede: hasSpot, Now: time.Now(),
			ComparedTo: comparedTo, Unstable: unstable, Carried: carried, Replaced: replaced,
			Branch: branch, Commit: commit})
		if err != nil {
			fmt.Printf("FAIL     %s: %v\n", c.Name, err)
			failed++
			continue
		}
		replaces := ""
		if p.Replaces != "" {
			replaces = ", replacing the safe spot from run " + p.Replaces
		}
		fmt.Printf("proposed %s: run %s%s, NOT a safe spot yet\n", c.Name, p.RunID, replaces)
		briefs = append(briefs, store.ProposalBrief(p, rec, c.Description))
	}
	if len(briefs) > 0 {
		fmt.Printf("\nshow the user these %d summaries in the conversation, with what you checked, and ask them to approve or reject each:\n", len(briefs))
		for _, b := range briefs {
			fmt.Print("\n" + b)
		}
		fmt.Printf("\nonly after the user says yes to every one:  shrt confirm -all -approve -by <their email>\n" +
			"for each one they reject, first:             shrt confirm <chain> -reject\n")
	} else {
		fmt.Println("nothing proposed")
	}
	if failed > 0 {
		return fmt.Errorf("%d chain(s) could not be proposed", failed)
	}
	return nil
}

func proposable(e *env, c *chain.Chain) (*runner.Record, string) {
	if len(c.KeptRed) > 0 {
		return nil, "kept red on purpose, never confirmed"
	}
	rec, err := e.store.LoadRun(c.Name, "latest")
	switch {
	case err != nil:
		return nil, "no run recorded"
	case rec.DryRun:
		return nil, "latest run " + rec.RunID + " is a dry run"
	case !rec.Passed():
		return nil, "latest run " + rec.RunID + " did not pass"
	case e.otherTarget(rec.Target):
		return nil, "latest run " + rec.RunID + " went to " + rec.Target + ", not " + e.targetURL()
	}
	if why := ranOtherChainFile(e, rec); why != "" {
		return nil, why
	}
	return rec, ""
}

func approveAll(e *env, by, note string) error {
	by, err := approverEmail(by)
	if err != nil {
		return err
	}
	ps, err := e.store.ListProposals()
	if err != nil {
		return err
	}
	if len(ps) == 0 {
		return fmt.Errorf("%w: nothing is pending; propose first: shrt confirm -all -note \"...\"", store.ErrNoProposal)
	}
	failed := 0
	for _, p := range ps {
		err := keptRedNeverConfirmed(e, p.Chain)
		if err == nil {
			err = proposalChainMatches(e, p)
		}
		var spot *store.SafeSpot
		if err == nil {
			spot, _, err = e.store.Approve(p.Chain, store.Confirmation{By: by, Note: note, Acknowledged: true, Now: time.Now()})
		}
		if err != nil {
			fmt.Printf("FAIL     %s: %v\n", p.Chain, err)
			failed++
			continue
		}
		supersedes := ""
		if spot.Supersedes != "" {
			supersedes = ", superseding run " + spot.Supersedes + " (archived)"
		}
		fmt.Printf("approved %s: safe spot from run %s, approved by %s%s\n", spot.Chain, spot.RunID, spot.ConfirmedBy, supersedes)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d proposal(s) not approved", failed, len(ps))
	}
	return nil
}
