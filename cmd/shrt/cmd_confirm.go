package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
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
	"       shrt confirm <new> -rename-from <old> -by <user email>                        carry a safe spot across a pure chain rename\n" +
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
	renameFrom := fs.String("rename-from", "", "carry the safe spot of `<old>` chain, renamed to this one, with its approval: only when the chain is identical apart from its name; needs -by")
	setUsage(fs, confirmUsage, "\nexit codes:\n  0  proposal written, approved, rejected or listed\n"+
		"  1  refused: no passing run, a chain with kept_red (never confirmed), no -note, no -by, nothing pending, or a -rename-from that is not a pure rename\n")
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
	name, err := e.chainName(rest[0])
	if err != nil && !*reject {
		return err
	}
	if err != nil {
		name = rest[0]
	}
	if err := e.knownChain(name); err != nil {
		return err
	}
	if *renameFrom != "" {
		if *approve || *reject || *supersede || *note != "" {
			return errors.New("-rename-from takes only -by: it carries an approved safe spot across a rename and proposes nothing")
		}
		return renameSafeSpot(e, name, *renameFrom, *by)
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
	comparedTo, unstable, carried := unstableFields(e, rec)
	branch, commit := gitWhere(e.cfg.Root)
	p, err := e.store.Propose(rec, store.ProposalInput{By: *by, Checked: *note, Supersede: *supersede, Now: time.Now(),
		ComparedTo: comparedTo, Unstable: unstable, Carried: carried, Replaced: differsFromSafeSpot(e, rec),
		Branch: branch, Commit: commit})
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
	if rec.ChainSource != "" {
		fmt.Printf("  chain file:  %s (what run %s ran)\n", rel(e.cfg.Root, rec.ChainSource), rec.RunID)
	}
	if why := ranOtherChainFile(e, rec); why != "" {
		fmt.Printf("  WARNING: %s\n", why)
	}
	fmt.Printf("\nshow the user this summary in the conversation, with what you checked, and ask them to approve or reject:\n\n")
	fmt.Print(store.ProposalSummary(p, rec))
	fmt.Printf("\nonly after the user says yes:  shrt confirm %s -approve -by <their email>\n"+
		"if they say no:                shrt confirm %s -reject\n", p.Chain, p.Chain)
	return nil
}

func ranOtherChainFile(e *env, rec *runner.Record) string {
	c, err := chain.Resolve(e.chainsDir(), rec.Chain)
	if err != nil || c.SourcePath == "" {
		return ""
	}
	now := rel(e.cfg.Root, c.SourcePath)
	if rec.ChainSource != "" && !sameFilePath(rec.ChainSource, c.SourcePath) {
		return fmt.Sprintf("run %s ran %s, but chain %s is %s now: approving makes a run of another file %s's ground truth",
			rec.RunID, rel(e.cfg.Root, rec.ChainSource), rec.Chain, now, rec.Chain)
	}
	if rec.ChainDigest != "" && rec.ChainDigest != c.Digest() {
		return fmt.Sprintf("%s changed since run %s ran it (chain digest %s, now %s): the run is not of the chain as it is now",
			now, rec.RunID, rec.ChainDigest, c.Digest())
	}
	return ""
}

func sameFilePath(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	if err1 == nil && err2 == nil && x == y {
		return true
	}
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
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
	if err := proposalChainMatches(e, p); err != nil {
		return err
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

func unstableFields(e *env, rec *runner.Record) (string, []string, []string) {
	ids, err := e.store.ListRuns(rec.Chain)
	if err != nil {
		return "", nil, nil
	}
	spot, err := e.store.LoadSafeSpot(rec.Chain)
	if err != nil {
		spot = nil
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
		unstable, carried := unstableAgainstSpot(prev, rec, spot, currentVolatile(e, rec.Chain), c, unsentDefault(e))
		return prev.RunID, unstable, carried
	}
	return "", nil, nil
}

func unstableAgainst(prev, rec *runner.Record, volatile []string, c *chain.Chain, unsent func(procedure, path string, v any) bool) []string {
	unstable, _ := unstableAgainstSpot(prev, rec, nil, volatile, c, unsent)
	return unstable
}

func unstableAgainstSpot(prev, rec *runner.Record, spot *store.SafeSpot, volatile []string, c *chain.Chain,
	unsent func(procedure, path string, v any) bool) ([]string, []string) {
	base := &store.SafeSpot{Chain: prev.Chain, RunID: prev.RunID, Volatile: prev.Volatile, Steps: prev.Steps}
	base, _ = diff.RenameSpotSteps(base, rec.Steps)
	if spot != nil {
		spot, _ = diff.RenameSpotSteps(spot, rec.Steps)
	}
	rep := diff.CompareMasking(base, rec, volatile)
	rep.DropUnsentDefaults(base, rec, unsent)
	if c != nil {
		rep.RequestChanges = diff.CompareRequests(base, rec, derivedRequestPath(c))
		rep.SeparateInput(base, rec, volatile, requestFixtures(c))
	}
	unstable, carried := []string{}, []string{}
	for _, ch := range rep.Changes {
		if structuralChange(ch) {
			continue
		}
		line := ch.Step + " " + ch.Path + ": " + ch.Transition()
		if spot != nil && heldBySpot(spot, rec, ch, unsent) {
			carried = append(carried, line)
			continue
		}
		unstable = append(unstable, line)
	}
	return unstable, carried
}

func structuralChange(ch diff.Change) bool {
	switch {
	case ch.Path == "step" || ch.Path == "response":
		return true
	case ch.Kind == diff.KindNotReached || ch.Kind == diff.KindOrder || ch.Kind == diff.KindStatus:
		return true
	}
	return false
}

func heldBySpot(spot *store.SafeSpot, rec *runner.Record, ch diff.Change, unsent func(procedure, path string, v any) bool) bool {
	var was *runner.StepRecord
	for _, st := range spot.Steps {
		if st.ID == ch.Step {
			was = st
		}
	}
	if was == nil || len(was.Response) == 0 {
		return false
	}
	var body any
	if err := json.Unmarshal(was.Response, &body); err != nil {
		return false
	}
	held, found := chain.Get(body, ch.Path)
	prevAbsent := ch.Kind == diff.KindUnexpected
	switch {
	case found && prevAbsent:
		return false
	case found:
		return reflect.DeepEqual(held, ch.Want)
	case prevAbsent:
		return true
	}
	if unsent == nil {
		return false
	}
	st, ok := rec.Step(ch.Step)
	return ok && unsent(st.Procedure, ch.Path, ch.Want)
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
	rep.DropUnsentDefaults(spot, rec, unsentDefault(e))
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
