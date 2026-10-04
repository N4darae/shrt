package main

import (
	"fmt"
	"path/filepath"
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
	rows := []store.ProposalRow{}
	failed, skipped, reasons := 0, map[string][]string{}, []string{}
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
			if skipped[reason] == nil {
				reasons = append(reasons, reason)
			}
			skipped[reason] = append(skipped[reason], c.Name)
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
		rows = append(rows, store.ProposalRowOf(p, rec))
	}
	for _, r := range reasons {
		fmt.Printf("skip     %s: %s\n", r, capList(skipped[r], 3))
	}
	if len(rows) == 0 {
		fmt.Println("nothing proposed")
	} else {
		printProposalRows(e, rows, note)
	}
	if failed > 0 {
		return fmt.Errorf("%d chain(s) could not be proposed", failed)
	}
	return nil
}

func printProposalRows(e *env, rows []store.ProposalRow, note string) {
	fmt.Printf("\nproposed %d chain(s), NOT safe spots yet; what the proposer checked: %s\n\n", len(rows), strings.Join(strings.Fields(note), " "))
	fmt.Print(proposalTable(rows))
	volatile := map[string][]string{}
	order := []string{}
	for _, r := range rows {
		if volatile[r.Volatile] == nil {
			order = append(order, r.Volatile)
		}
		volatile[r.Volatile] = append(volatile[r.Volatile], r.Chain)
	}
	for _, v := range order {
		if v == "" {
			continue
		}
		which := "every chain above"
		if len(volatile[v]) < len(rows) {
			which = strings.Join(volatile[v], ", ")
		}
		fmt.Printf("\n**Volatile, never compared by `shrt verify`:** %s (%s)\n", v, which)
	}
	fmt.Printf("\nApproving makes every response field of each run that no volatile pattern covers the baseline `shrt verify` compares against. "+
		"Each chain's full summary: %s/<chain>.md\n", rel(e.cfg.Root, filepath.Join(e.store.SafeSpotsDir, "pending")))
	fmt.Printf("\nshow the user this table, with what you checked, and ask them to approve or reject each:\n" +
		"only after the user says yes to every one:  shrt confirm -all -approve -by <their email>\n" +
		"for each one they reject, first:             shrt confirm <chain> -reject\n")
}

func proposalTable(rows []store.ProposalRow) string {
	var b strings.Builder
	b.WriteString("| chain | run | steps passed | refusals asserted |\n|---|---|---|---|\n")
	chains, order := map[string][]string{}, []string{}
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", r.Chain, r.Run, r.Steps, r.Refusals)
		if r.Check == "" || r.Check == "none" {
			continue
		}
		if chains[r.Check] == nil {
			order = append(order, r.Check)
		}
		chains[r.Check] = append(chains[r.Check], "`"+r.Chain+"`")
	}
	if len(order) > 0 {
		b.WriteString("\n**Check before approving:**\n")
	}
	for _, check := range order {
		fmt.Fprintf(&b, "- %s: %s\n", strings.Join(chains[check], ", "), check)
	}
	return b.String()
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
