package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func init() {
	register(&command{
		name:    "confirm",
		summary: "propose a passing run as its chain's safe spot, then approve or reject it once the user decides",
		run:     runConfirm,
	})
}

const confirmUsage = "usage: shrt confirm <chain> -note \"what you checked\" [-run <id>] [-supersede]   propose, and print the summary to show the user\n" +
	"       shrt confirm <chain> -approve -by <user email>                                only after the user said yes\n" +
	"       shrt confirm <chain> -reject\n" +
	"       shrt confirm -pending                                                         list proposals awaiting a decision"

func runConfirm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("confirm", flag.ContinueOnError)
	runID := fs.String("run", "latest", "run id to propose, or 'latest'")
	note := fs.String("note", "", "proposing: what you checked that makes this run correct (required); approving: an optional note of the approver's")
	by := fs.String("by", "", "approving: the email of the user who said yes (required); proposing: who proposes (default agent)")
	supersede := fs.Bool("supersede", false, "propose replacing the existing safe spot, which is archived on approval")
	approve := fs.Bool("approve", false, "approve the pending proposal and write the safe spot; run it only after the user has said yes")
	reject := fs.Bool("reject", false, "discard the pending proposal")
	pending := fs.Bool("pending", false, "list proposals awaiting a decision")
	setUsage(fs, confirmUsage, "\nexit codes:\n  0  proposal written, approved, rejected or listed\n"+
		"  1  refused: no passing run, a chain with kept_red (never confirmed), no -note, no -by, or nothing pending\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	if *pending {
		return listProposals(e)
	}
	if len(rest) != 1 {
		return errors.New(confirmUsage)
	}
	name := rest[0]
	if err := e.knownChain(name); err != nil {
		return err
	}
	switch {
	case *approve && *reject:
		return errors.New("-approve and -reject together say nothing; pick one")
	case *approve:
		if err := keptRedNeverConfirmed(e, name); err != nil {
			return err
		}
		return approveProposal(e, name, *by, *note)
	case *reject:
		if !e.store.DropProposal(name) {
			return fmt.Errorf("%w for chain %s", store.ErrNoProposal, name)
		}
		fmt.Printf("proposal for %q rejected and removed; no safe spot was written\n", name)
		return nil
	}

	if err := keptRedNeverConfirmed(e, name); err != nil {
		return err
	}
	e.store.Notes = nil
	rec, err := e.store.LoadRun(name, *runID)
	if err != nil {
		return err
	}
	comparedTo, unstable := unstableFields(e, rec)
	p, err := e.store.Propose(rec, store.ProposalInput{By: *by, Checked: *note, Supersede: *supersede, Now: time.Now(),
		ComparedTo: comparedTo, Unstable: unstable, Replaced: differsFromSafeSpot(e, rec)})
	if errors.Is(err, store.ErrNoEvidence) {
		return fmt.Errorf("%w: pass -note with what you inspected in the responses and why they are correct, not only that the run is green", err)
	}
	if err != nil {
		return err
	}
	fmt.Printf("proposed run %s as the safe spot for %q — NOT a safe spot yet\n  full report: %s\n",
		p.RunID, p.Chain, rel(e.cfg.Root, p.Report))
	if p.Replaces != "" {
		fmt.Printf("  replaces the safe spot from run %s once approved\n", p.Replaces)
	}
	fmt.Printf("\nshow the user this summary in the conversation, with what you checked, and ask them to approve or reject:\n\n")
	fmt.Print(store.ProposalSummary(p, rec))
	fmt.Printf("\nonly after the user says yes:  shrt confirm %s -approve -by <their email>\n"+
		"if they say no:                shrt confirm %s -reject\n", p.Chain, p.Chain)
	return nil
}

func keptRedNeverConfirmed(e *env, name string) error {
	c, err := chain.Resolve(e.chainsDir(), name)
	if err != nil || len(c.KeptRed) == 0 {
		return nil
	}
	pins := make([]string, 0, len(c.KeptRed))
	for _, k := range c.KeptRed {
		pins = append(pins, k.Step+" "+k.Path)
	}
	return fmt.Errorf("chain %s is kept red on purpose (kept_red pins %s), so it is never confirmed: its runs are evidence "+
		"of the pinned defect, not baselines, and a safe spot would freeze the bug as ground truth. Its check is `shrt run %s` "+
		"exiting 0 (failed exactly as pinned). Once the defect is fixed (the run says defect_gone), remove kept_red, assert the "+
		"corrected behaviour, run it until it passes, and propose that run", name, strings.Join(pins, ", "), name)
}

func approverEmail(by string) (string, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		return "", fmt.Errorf("%w: -approve needs -by <email of the user who said yes>", store.ErrNotConfirmed)
	}
	addr, err := mail.ParseAddress(by)
	if err != nil || addr.Address != by || addr.Name != "" {
		return "", fmt.Errorf("%w: -by %q is not an email address; approval is recorded under the email of the user who said yes", store.ErrNotConfirmed, by)
	}
	domain := by[strings.LastIndex(by, "@")+1:]
	labels := strings.Split(domain, ".")
	if len(labels) < 2 || slices.Contains(labels, "") {
		return "", fmt.Errorf("%w: -by %q has no dotted domain after the @ (like example.com), so it is not an address "+
			"anyone can be reached at; approval is recorded under the email of the user who said yes", store.ErrNotConfirmed, by)
	}
	return by, nil
}

func approveProposal(e *env, name, by, note string) error {
	by, err := approverEmail(by)
	if err != nil {
		return err
	}
	p, err := e.store.LoadProposal(name)
	if err != nil {
		return fmt.Errorf("%w\npropose first: shrt confirm %s -note \"...\"", err, name)
	}
	spot, path, err := e.store.Approve(name, store.Confirmation{By: by, Note: note, Acknowledged: true, Now: time.Now()})
	if err != nil {
		return err
	}
	fmt.Printf("safe spot for %q set from run %s (proposed by %s)\n  approved by: %s at %s\n  digest:      %s\n  file:        %s\n",
		spot.Chain, spot.RunID, p.ProposedBy, spot.ConfirmedBy, spot.ConfirmedAt.Format(time.RFC3339), spot.Digest, rel(e.cfg.Root, path))
	if spot.Supersedes != "" {
		fmt.Printf("  supersedes run %s (archived)\n", spot.Supersedes)
	}
	return nil
}

func listProposals(e *env) error {
	ps, err := e.store.ListProposals()
	if err != nil {
		return err
	}
	if len(ps) == 0 {
		fmt.Println("no proposals awaiting a decision")
		return nil
	}
	for _, p := range ps {
		fmt.Printf("%s  run %s  proposed by %s at %s\n  report: %s\n",
			p.Chain, p.RunID, p.ProposedBy, p.ProposedAt.Format(time.RFC3339), rel(e.cfg.Root, p.Report))
	}
	return nil
}

func unstableFields(e *env, rec *runner.Record) (string, []string) {
	ids, err := e.store.ListRuns(rec.Chain)
	if err != nil {
		return "", nil
	}
	for i := len(ids) - 1; i >= 0; i-- {
		if ids[i] == rec.RunID {
			continue
		}
		prev, err := e.store.LoadRun(rec.Chain, ids[i])
		if err != nil || !prev.Passed() || prev.DryRun || !config.SameTarget(prev.Target, rec.Target) {
			continue
		}
		c, _ := chain.Resolve(e.chainsDir(), rec.Chain)
		return prev.RunID, unstableAgainst(prev, rec, currentVolatile(e, rec.Chain), c)
	}
	return "", nil
}

func unstableAgainst(prev, rec *runner.Record, volatile []string, c *chain.Chain) []string {
	base := &store.SafeSpot{Chain: prev.Chain, RunID: prev.RunID, Volatile: prev.Volatile, Steps: prev.Steps}
	base, _ = diff.RenameSpotSteps(base, rec.Steps)
	rep := diff.CompareMasking(base, rec, volatile)
	if c != nil {
		rep.RequestChanges = diff.CompareRequests(base, rec, derivedRequestPath(c))
		rep.SeparateInput(base, rec, volatile, requestFixtures(c))
	}
	out := []string{}
	for _, ch := range rep.Changes {
		out = append(out, ch.Step+" "+ch.Path+": "+ch.Transition())
	}
	return out
}

func differsFromSafeSpot(e *env, rec *runner.Record) []store.Differ {
	spot, err := e.store.LoadSafeSpot(rec.Chain)
	if err != nil {
		return nil
	}
	var derived func(step, path string) bool
	c, cerr := chain.Resolve(e.chainsDir(), rec.Chain)
	if cerr == nil {
		derived = derivedRequestPath(c)
	} else {
		c = nil
	}
	spot, renamed := diff.RenameSpotSteps(spot, rec.Steps)
	rep := diff.CompareWithRequests(spot, rec, currentVolatile(e, rec.Chain), derived)
	if c != nil {
		rep.SeparateInput(spot, rec, currentVolatile(e, rec.Chain), requestFixtures(c))
	}
	out := unapprovedVolatileDiffers(rep.UnapprovedVolatile, rec, c)
	if !config.SameTarget(spot.Target, rec.Target) {
		out = append(out, store.Differ{Step: "-", Side: "target", Path: "base_url", Delta: orUnknown(spot.Target) + " -> " + orUnknown(rec.Target)})
	}
	edited := diff.ChainChanges(spot, c)
	reordered := false
	for _, ch := range edited {
		if ch.StepOrder() {
			reordered = true
			out = append(out, store.Differ{Step: "step order", Side: "chain", Path: "moved:", Delta: ch.Moves()})
			continue
		}
		out = append(out, store.Differ{Step: ch.Step, Side: "chain", Path: ch.Path, Delta: ch.Transition()})
	}
	for _, a := range diff.UnorderedAdditions(spot, rec) {
		step, where := a.Step, ""
		if step == "" {
			step, where = "-", " at chain level"
		}
		out = append(out, store.Differ{Step: step, Side: "chain", Path: "unordered",
			Delta: "absent -> unordered: [" + strings.Join(a.Paths, ", ") + "]" + where + " (compared as a multiset from now on)"})
	}
	for _, c := range diff.DropRefEdited(rep.RequestChanges, edited) {
		out = append(out, store.Differ{Step: c.Step, Side: "request", Path: c.Path, Delta: c.Transition()})
	}
	for _, c := range rep.Changes {
		if c.StepOrder() {
			if !reordered {
				out = append(out, store.Differ{Step: "step order", Side: "chain", Path: "moved:", Delta: c.Moves()})
			}
			continue
		}
		side := "response"
		switch c.Kind {
		case diff.KindStatus, diff.KindNotReached, diff.KindOrder:
			side = "step"
		}
		if c.Step == "" {
			side, c.Step = "run", "-"
		}
		out = append(out, store.Differ{Step: c.Step, Side: side, Path: c.Path, Delta: c.Transition()})
	}
	for _, rn := range renamed {
		out = append(out, store.Differ{Step: rn.Now, Side: "chain", Path: "step",
			Delta: fmt.Sprintf("renamed from %s (same call and position, compared as that step)", rn.Was)})
	}
	return out
}

func unapprovedVolatileDiffers(patterns []string, rec *runner.Record, c *chain.Chain) []store.Differ {
	owners := map[string][]string{}
	own := func(step string, ps []string) {
		for _, p := range ps {
			if !slices.Contains(owners[p], step) {
				owners[p] = append(owners[p], step)
			}
		}
	}
	for _, st := range rec.Steps {
		own(st.ID, st.Volatile)
	}
	if c != nil {
		for _, st := range c.Steps {
			own(st.ID, st.Volatile)
		}
	}
	chainWide := map[string]bool{}
	for _, p := range rec.Volatile {
		chainWide[p] = true
	}
	if c != nil {
		for _, p := range c.Volatile {
			chainWide[p] = true
		}
	}
	out := []store.Differ{}
	for _, p := range patterns {
		steps := owners[p]
		if len(steps) == 0 || chainWide[p] {
			steps = []string{"-"}
		}
		for _, step := range steps {
			out = append(out, store.Differ{Step: step, Side: "volatile", Path: p, Delta: "masked now, not in the replaced safe spot"})
		}
	}
	return out
}
