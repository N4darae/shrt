package main

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
)

func contractPlan(args []string) error {
	fs := flag.NewFlagSet("contract plan", flag.ContinueOnError)
	name := fs.String("name", "", "chain name, defaults to one derived from the rpc, or the -write file's name")
	write := &optionalString{}
	fs.Var(write, "write", "write into the chains directory; given a `[file]` name, into .shrt/scratch/, which no gate runs, or to a path with a slash")
	force := fs.Bool("force", false, "overwrite an existing chain file, unless it has a safe spot or a kept-red slice")
	forceApproved := fs.Bool("force-approved", false, "with -force, ALSO overwrite one that has; only after the user has said so")
	showNotes := fs.Bool("notes", false, "print each gap, then one line per probe group saying why it is there")
	verbose := fs.Bool("v", false, "print every step id; with -notes, every note in full")
	all := fs.Bool("all", false, "plan one chain per rpc with a contract, one line each and its gaps")
	setUsage(fs, "usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write [<file>] [-name <chain>] [-force]] [-notes] [-v]\n"+
		"       shrt contract plan -all [-write [-force]]",
		"\nwithout -write it prints the order, the step count per probe group, and a gap: line for each thing it could\n"+
			"not plan or assert. -all does that for each rpc with a contract, as plan <rpc> would, skipping client- and\n"+
			"bidi-streaming ones; -write keeps an existing file unless -force.\n"+
			"\nexit codes:\n"+
			"  0  the plan was printed, or with -write written; also when a required field still has no\n"+
			"     usable value (a note names it and chain lint errors on it until you fill it)\n"+
			"  1  nothing was planned or written; with -all, an rpc could not be planned\n"+
			"     - no rpc named, an rpc the catalog does not have, or an alias its contract does not declare\n"+
			"     - a client- or bidi-streaming target, or a contract graph that pulls in a streaming rpc as setup\n"+
			"     - a dependency cycle in the contracts (needs/from/same_as/before)\n"+
			"     - with -write, the file already exists and no -force, or has a safe spot or kept-red slice and no -force-approved\n"+
			"     - bad flags, or a setup that cannot load (no .shrt/config.yaml, a missing descriptor, a\n"+
			"       contract overlay that does not parse)\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(rest, func(v string) bool { return strings.HasSuffix(v, ".yaml") }); write.set && write.value == "" && i >= 0 {
		write.value = rest[i]
		rest = slices.Delete(rest, i, i+1)
	}
	*force = *force || *forceApproved
	if *all && (len(rest) > 0 || *name != "" || write.value != "") {
		return fmt.Errorf("-all plans every rpc with a contract into the chains directory: name no rpc, no -name and no -write file")
	}
	if len(rest) == 0 && !*all {
		return fmt.Errorf("usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write], or -all")
	}
	e, lib, err := loadLibrary()
	if err != nil {
		return err
	}
	if *all {
		return planAll(e, lib, write.set, *force, *forceApproved)
	}
	chainName, path := *name, ""
	if write.value != "" {
		var named string
		if path, named, err = planFile(e, write.value); err != nil {
			return err
		}
		chainName = cmp.Or(chainName, named)
	}
	if chainName == "" {
		chainName, err = planChainName(rest, lib, e)
		if err != nil {
			return err
		}
	}
	plan, err := contract.BuildPlanWith(rest, lib, e.cat, chainName, planOptions(e))
	if err != nil {
		return err
	}
	raw, err := plan.YAML()
	if err != nil {
		return err
	}
	again := "shrt contract plan " + strings.Join(rest, " ")
	if *name != "" {
		again += " -name " + *name
	}
	if path == "" {
		path = filepath.Join(e.chainsDir(), chainName+".yaml")
	}
	held, beside := approvedBy(e, path), fmt.Sprintf("%s -write %s-plan.yaml", again, chainName)
	rewrite := strings.TrimSpace(again + " -write " + write.value + " -force")
	if held != "" {
		rewrite = beside
	}
	order := strings.Join(shortNames(plan.Order), " -> ")
	if !write.set {
		fmt.Printf("order: %s\n", order)
		groups := []string{}
		for _, g := range plan.StepGroups() {
			groups = append(groups, fmt.Sprintf("%d %s", g.Steps, g.Label))
		}
		fmt.Printf("%d steps: %s\n", len(plan.Chain.Steps), strings.Join(groups, ", "))
		if *verbose {
			ids := make([]string, 0, len(plan.Chain.Steps))
			for _, st := range plan.Chain.Steps {
				ids = append(ids, st.ID)
			}
			fmt.Printf("step ids: %s\n", strings.Join(ids, ", "))
		}
		printPlanNotes(plan, again, rewrite, *showNotes, *verbose)
		fmt.Printf("next: %s -write\n", again)
		return nil
	}
	if _, err := os.Stat(path); err == nil && (!*force || held != "" && !*forceApproved) {
		if held == "" {
			return fmt.Errorf("%s already exists, pass -force to overwrite", path)
		}
		return fmt.Errorf("%s has %s: overwriting it changes the steps verify replays against what was approved, so the gate shows "+
			"chain change drift and may run again a step a slice pins red. Write the plan beside it: %s (into .shrt/scratch/); "+
			"to replace the approved chain, only once the user has said so: -force -force-approved", rel(e.cfg.Root, path), held, beside)
	}
	if err := writePlanFile(path, raw); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d steps, order %s\n", rel(e.cfg.Root, path), len(plan.Chain.Steps), order)
	printPlanNotes(plan, again, rewrite, *showNotes, *verbose)
	if write.value != "" {
		chainName = rel(e.cfg.Root, path)
	}
	if plan.UnfilledCount() > 0 {
		fmt.Printf("next: fill the test data, then shrt chain lint %s\n", chainName)
		return nil
	}
	fmt.Printf("next: shrt chain lint %s\n", chainName)
	return nil
}

func planAll(e *env, lib *contract.Library, write, force, forceApproved bool) error {
	planned, kept, failed := 0, 0, []string{}
	for _, rpc := range lib.RPCs() {
		m, err := e.cat.Lookup(rpc)
		if err != nil {
			fmt.Printf("%s: %v\n", methodName(rpc), err)
			failed = append(failed, methodName(rpc))
			continue
		}
		if m.StreamRefusal() != "" {
			fmt.Printf("%s: %s, not planned\n", m.Name, m.StreamKind())
			continue
		}
		name, err := planChainName([]string{rpc}, lib, e)
		if err == nil {
			var plan *contract.Plan
			if plan, err = contract.BuildPlanWith([]string{rpc}, lib, e.cat, name, planOptions(e)); err == nil {
				var existed bool
				existed, err = planAllOne(e, plan, name, write, force, forceApproved)
				if existed {
					kept++
				}
			}
		}
		if err != nil {
			fmt.Printf("%s: %v\n", m.Name, err)
			failed = append(failed, m.Name)
			continue
		}
		planned++
	}
	if kept > 0 {
		fmt.Printf("%d existing chain file(s) kept: -force overwrites those without a safe spot or a kept-red slice\n", kept)
	}
	if planned > 0 {
		fmt.Println("every note: shrt contract plan <rpc> -notes")
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d rpc(s) not planned: %s", len(failed), strings.Join(failed, ", "))
	}
	if planned == 0 {
		return fmt.Errorf("no rpc with a contract to plan")
	}
	return nil
}

func planAllOne(e *env, plan *contract.Plan, name string, write, force, forceApproved bool) (bool, error) {
	groups := []string{}
	for _, g := range plan.StepGroups() {
		groups = append(groups, fmt.Sprintf("%d %s", g.Steps, g.Label))
	}
	line := fmt.Sprintf("%s: %s, %d steps (%s)", name, strings.Join(shortNames(plan.Order), " -> "), len(plan.Chain.Steps), strings.Join(groups, ", "))
	existed := false
	if write {
		path := filepath.Join(e.chainsDir(), name+".yaml")
		if _, err := os.Stat(path); err == nil && !force {
			line, existed = line+", kept the existing file", true
		} else if held := approvedBy(e, path); held != "" && !forceApproved {
			line, existed = line+", kept: it has "+held, true
		} else {
			raw, err := plan.YAML()
			if err == nil {
				err = writePlanFile(path, raw)
			}
			if err != nil {
				return false, err
			}
			line += ", written"
		}
	}
	fmt.Println(line)
	printFillAndGaps(plan, "  ")
	return existed, nil
}

func printFillAndGaps(plan *contract.Plan, indent string) {
	for _, n := range plan.FillNotes() {
		fmt.Printf("%sfill: %s\n", indent, n)
	}
	for _, n := range plan.GapNotes() {
		fmt.Printf("%sgap: %s\n", indent, n)
	}
}

func printPlanNotes(plan *contract.Plan, again, rewrite string, notes, full bool) {
	switch {
	case notes && full:
		for _, n := range plan.Notes {
			label := "note"
			if plan.IsGap(n) {
				label = "gap"
			}
			fmt.Printf("%s: %s\n", label, n)
		}
	case notes:
		printFillAndGaps(plan, "")
		for _, g := range plan.StepGroups() {
			if why := contract.ProbeWhy(g.Label); why != "" {
				fmt.Printf("%s (%d %s): %s\n", g.Label, g.Steps, pluralWord(g.Steps, "step", "steps"), why)
			}
		}
	default:
		printFillAndGaps(plan, "")
	}
	gaps := plan.GapNotes()
	if n := plan.UnfilledCount(); n > 0 {
		fmt.Printf("%d required field(s) carry no test data, and chain lint errors on each until filled; "+
			"after a value: in the contract, re-plan with %s\n", n, rewrite)
	}
	switch n := len(plan.Notes) - len(plan.FillNotes()) - len(gaps); {
	case n > 0 && !notes:
		fmt.Printf("%d more note(s) on why each probe is there: %s -notes\n", n, again)
	case n > 0 && !full:
		fmt.Printf("every note in full, and every step id: %s -notes -v\n", again)
	}
}

func planFile(e *env, value string) (string, string, error) {
	if isSlicePath(value) && !bareSliceFile(value) {
		return slicePathAndName(value)
	}
	name := strings.TrimSuffix(value, ".yaml")
	return filepath.Join(scratchDir(e), name+".yaml"), name, nil
}

func writePlanFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func approvedBy(e *env, path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".yaml")
	if _, err := os.Stat(path); err != nil || filepath.Dir(path) != filepath.Clean(e.chainsDir()) {
		return ""
	}
	var held []string
	if _, err := os.Stat(e.store.SafeSpotPath(name)); err == nil {
		held = append(held, "the approved safe spot "+rel(e.cfg.Root, e.store.SafeSpotPath(name)))
	}
	chains, _, _ := chain.LoadDirPartial(e.chainsDir())
	for _, c := range chains {
		if len(c.KeptRed) > 0 && strings.HasPrefix(c.Description, "Slice of "+name+" reproducing step ") {
			held = append(held, "the kept-red slice "+c.Name)
		}
	}
	return andList(held)
}

func planChainName(targets []string, lib *contract.Library, e *env) (string, error) {
	parts := []string{}
	seen := map[string]bool{}
	domain := ""
	for _, raw := range targets {
		node, m, err := contract.ResolveTarget(raw, lib, e.cat)
		if err != nil {
			return "", err
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		if domain == "" {
			domain = contract.DomainOf(m)
		}
		part := strings.ToLower(m.Name)
		if _, alias := contract.SplitNode(node); alias != "" {
			part += "-" + strings.ToLower(alias)
		}
		parts = append(parts, part)
	}
	return domain + "-" + strings.Join(parts, "-"), nil
}

func shortNames(rpcs []string) []string {
	out := make([]string, 0, len(rpcs))
	for _, r := range rpcs {
		out = append(out, methodName(r))
	}
	return out
}

func planOptions(e *env) contract.PlanOptions {
	opts := contract.PlanOptions{Auth: e.cfg.Auth != nil, Redact: e.cfg.Redact}
	for _, name := range e.cfg.AuthProfileNames() {
		if name != config.DefaultAuthProfile {
			opts.Profiles = append(opts.Profiles, name)
		}
	}
	opts.Logins = sortedKeys(loginRPCs(e))
	profiles := []*config.Auth{e.cfg.Auth}
	for _, name := range opts.Profiles {
		if e.cfg.Auth != nil {
			profiles = append(profiles, e.cfg.Auth.Profiles[name])
		}
	}
	for _, a := range profiles {
		if a == nil || a.Call == "" || len(a.Body) == 0 {
			continue
		}
		m, err := e.cat.Lookup(a.Call)
		if err != nil {
			continue
		}
		if opts.LoginBodies == nil {
			opts.LoginBodies = map[string]map[string]any{}
		}
		if _, taken := opts.LoginBodies[m.FullName]; !taken {
			opts.LoginBodies[m.FullName] = a.Body
		}
	}
	for _, name := range opts.Profiles {
		a := e.cfg.Auth.Profiles[name]
		if a == nil || e.cfg.Auth == nil || len(a.Body) == 0 {
			continue
		}
		if opts.ProfileBodies == nil {
			opts.ProfileBodies = map[string]map[string]any{}
		}
		opts.ProfileBodies[name] = a.Body
	}
	return opts
}
