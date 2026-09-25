package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/hollow"
	"github.com/N4darae/shrt/store"
)

func chainHollow(args []string) error {
	fs := flag.NewFlagSet("chain hollow", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	allowPath := fs.String("allow", "", "allowlist file (default "+hollow.DefaultAllowFile+")")
	runsPath := fs.String("runs", "", "runs directory to read (default paths.runs from the config)")
	gate := fs.Bool("gate", false, "ratchet the reported count against -baseline: fail if it rises, and fail if it falls without the baseline being lowered")
	baseline := fs.String("baseline", "", "file holding the reported count the ratchet holds to")
	setUsage(fs, "usage: shrt chain hollow [flags]   read steps in the local run records that passed while the response carried nothing",
		"\nexit codes:\n  0  no unexplained hollow read; under -gate, at the baseline\n"+
			"  1  hollow reads reported; under -gate, worse or better than the baseline\n"+
			"  2  no run records to read\n")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}

	explicit := *allowPath != ""
	path := *allowPath
	if !explicit {
		path = filepath.Join(e.cfg.Root, filepath.FromSlash(hollow.DefaultAllowFile))
	}
	allow, err := hollow.LoadAllowlist(path, explicit)
	if err != nil {
		return err
	}

	chains, brokenChains, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil {
		return err
	}
	for _, b := range brokenChains {
		fmt.Fprintf(os.Stderr, "hollow: a chain did not load, so any step it declares is invisible to this "+
			"gate and a hollow read in it cannot be reported: %v\n", b)
	}

	runsDir := e.cfg.Abs(e.cfg.Paths.Runs)
	if *runsPath != "" {
		runsDir = *runsPath
	}
	known := map[string]bool{}
	for _, c := range chains {
		known[strings.ToLower(c.Name)] = true
	}
	if len(brokenChains) > 0 || *runsPath != "" {
		known = nil
	}
	rep, scanErr := hollow.ScanKnownScratch(runsDir, allow, hollow.DataAsserted(chains), known, e.scratchSource)
	if errors.Is(scanErr, hollow.ErrNoRecords) {
		return exitWith(2,
			"hollow: read 0 run records under %s.\nThe count of hollow reads is zero because there was nothing to read, not because there are none.\n"+
				"Run records are gitignored, so a fresh clone has none: record one with 'shrt run <chain>', or point this at a tree that has them with -runs <dir>.",
			runsDir)
	}
	if scanErr != nil {
		return scanErr
	}

	if *asJSON {
		if err := emitJSON(rep); err != nil {
			return err
		}
		return nil
	}
	renamed := renamedChains(e)
	if *gate {
		return hollowGate(rep, *baseline, path, renamed)
	}
	for _, f := range rep.Findings {
		if f.Status != hollow.StatusReported {
			continue
		}
		fmt.Printf("HOLLOW %-40s %-54s %-30s %s (%d record(s))\n", f.Chain, f.Step, f.RPC, f.RunID, f.Occurrences)
	}
	fmt.Printf("\n%s: %d passed read step(s), %d asserting only the envelope verdict, %d asserting nothing, %d of those hollow\n",
		recordsCounted(rep), rep.ReadSteps, rep.EnvelopeOnly, rep.AssertsNothing, rep.HollowRecords)
	fmt.Printf("%d distinct (chain, step): %d reported, %d allowlisted, %d whose chain now asserts a data path\n",
		rep.DistinctSteps, rep.Unallowed, rep.Allowed, rep.ChainFixed)
	reportOrphans(rep, renamed)
	if rep.Unallowed > 0 {
		return exitWith(1,
			"%d read step(s) passed while the response carried nothing.\n"+
				"Either assert something the read should have found (.shrt/docs/PLAYBOOK.md §4),\n"+
				"or, if an empty body is the correct answer, add '<chain> <step-id> <reason>' to %s.",
			rep.Unallowed, path)
	}
	return nil
}

func hollowGate(rep *hollow.Report, baselinePath, allowPath string, renamed map[string]string) error {
	want, verdict, err := store.Ratchet(baselinePath, rep.Unallowed)
	if err != nil {
		return err
	}
	switch verdict {
	case store.RatchetWorse:
		return exitWith(1, "hollow: %d read step(s) passed while finding nothing, worse than the baseline %d.\n"+
			"Run 'shrt chain hollow' to see which. Either assert something the read should have found\n"+
			"(.shrt/docs/PLAYBOOK.md §4), or, if an empty body is correct, add a reasoned line to %s.",
			rep.Unallowed, want, allowPath)
	case store.RatchetBetter:
		return exitWith(1, "hollow: %d read step(s) passed while finding nothing, better than the baseline %d — "+
			"lower %s to %d to keep the ratchet tight", rep.Unallowed, want, baselinePath, rep.Unallowed)
	}
	fmt.Printf("hollow: %d hollow read step(s) reported from %s, at the baseline (%d allowlisted, %d whose chain now asserts a data path)\n",
		rep.Unallowed, recordsCounted(rep), rep.Allowed, rep.ChainFixed)
	reportOrphans(rep, renamed)
	return nil
}

func recordsCounted(rep *hollow.Report) string {
	return fmt.Sprintf("%d run record(s) (%d shrt run, %d verify replay(s); %d of chains kept red, %d that did not pass; "+
		"a passed read step counts whatever its run's verdict)",
		rep.Records, rep.Records-rep.ReplayRecords, rep.ReplayRecords, rep.KeptRedRecords, rep.FailedRecords)
}

func reportOrphans(rep *hollow.Report, renamed map[string]string) {
	if len(rep.Edited) > 0 {
		fmt.Printf("\n%d run record(s) were changed after shrt wrote them (their content no longer matches their seal) and were NOT counted:\n",
			len(rep.Edited))
		for _, p := range rep.Edited {
			fmt.Printf("  edited  %s\n", p)
		}
	}
	if rep.Unsealed > 0 {
		fmt.Printf("\n%d counted run record(s) predate sealed run records, so an edit to them cannot be ruled out; re-run their chains for records shrt can check\n", rep.Unsealed)
	}
	if len(rep.Scratch) > 0 {
		fmt.Printf("\n%d run record(s) under %d directory(ies) are runs of chains given by path, outside %s, and were NOT counted above:\n",
			rep.ScratchRecords, len(rep.Scratch), "paths.chains")
		for _, d := range rep.Scratch {
			fmt.Printf("  scratch %s\n", d)
		}
	}
	if len(rep.Orphans) == 0 {
		return
	}
	fmt.Printf("\n%d run record(s) under %d directory(ies) belong to no chain and were NOT counted above:\n",
		rep.OrphanRecords, len(rep.Orphans))
	for _, d := range rep.Orphans {
		if to, ok := renamed[d]; ok {
			fmt.Printf("  orphan  %s  (renamed to %s: its safe spot records the rename; these records stay under the old name)\n", d, to)
			continue
		}
		fmt.Printf("  orphan  %s\n", d)
	}
	fmt.Print("Deleting a chain leaves its runs behind. They are still evidence of what happened, so " +
		"nothing removes them for you — but a finding in one names a chain that no longer exists and " +
		"cannot be acted on, so they are excluded from the counts and from the gate. Delete the " +
		"directory when the evidence has served its purpose.\n")
}

func (e *env) scratchSource(source string) bool {
	if source == "" {
		return false
	}
	path := source
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.cfg.Root, filepath.FromSlash(source))
	}
	if rel, err := filepath.Rel(e.chainsDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func renamedChains(e *env) map[string]string {
	out := map[string]string{}
	dir := e.cfg.Abs(e.cfg.Paths.SafeSpots)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		spot, err := e.store.LoadSafeSpot(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		for _, r := range spot.Renamed {
			out[r.From] = spot.Chain
		}
	}
	return out
}
