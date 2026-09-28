package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
)

func contractPlan(args []string) error {
	fs := flag.NewFlagSet("contract plan", flag.ContinueOnError)
	name := fs.String("name", "", "chain name, defaults to one derived from the rpc")
	write := fs.Bool("write", false, "write the composed chain into the chains directory")
	force := fs.Bool("force", false, "overwrite an existing chain file")
	showNotes := fs.Bool("notes", false, "print each gap, then one line per probe group saying why it is there")
	verbose := fs.Bool("v", false, "print every step id; with -notes, every note in full")
	all := fs.Bool("all", false, "plan one chain per rpc with a contract, one line each and its gaps")
	setUsage(fs, "usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write [-name <chain>] [-force]] [-notes] [-v]\n"+
		"       shrt contract plan -all [-write [-force]]",
		"\nwithout -write it prints the order, the step count per probe group, and a gap: line for each thing it could\n"+
			"not plan or assert; -write writes the chain to the chains directory. -all does that for each rpc with a\n"+
			"contract, as plan <rpc> would, skipping client- and bidi-streaming ones; -write keeps an existing file unless -force.\n"+
			"\nexit codes:\n"+
			"  0  the plan was printed, or with -write written; also when a required field still has no\n"+
			"     usable value (a note names it and chain lint errors on it until you fill it)\n"+
			"  1  nothing was planned or written; with -all, an rpc could not be planned\n"+
			"     - no rpc named, an rpc the catalog does not have, or an alias its contract does not declare\n"+
			"     - a client- or bidi-streaming target, or a contract graph that pulls in a streaming rpc as setup\n"+
			"     - a dependency cycle in the contracts (needs/from/same_as/before)\n"+
			"     - with -write, the chain file already exists and -force was not given\n"+
			"     - bad flags, or a setup that cannot load (no .shrt/config.yaml, a missing descriptor, a\n"+
			"       contract overlay that does not parse)\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *all && (len(rest) > 0 || *name != "") {
		return fmt.Errorf("-all plans every rpc with a contract: name no rpc and no -name")
	}
	if len(rest) == 0 && !*all {
		return fmt.Errorf("usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write], or -all")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, err := e.library()
	if err != nil {
		return err
	}
	if *all {
		return planAll(e, lib, *write, *force)
	}
	chainName := *name
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
	order := strings.Join(shortNames(plan.Order), " -> ")
	if !*write {
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
		printPlanNotes(plan, again, *showNotes, *verbose)
		fmt.Printf("next: %s -write\n", again)
		return nil
	}
	path := filepath.Join(e.chainsDir(), chainName+".yaml")
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists, pass -force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d steps, order %s\n", rel(e.cfg.Root, path), len(plan.Chain.Steps), order)
	printPlanNotes(plan, again, *showNotes, *verbose)
	if plan.UnfilledCount() > 0 {
		fmt.Printf("next: fill the test data, then shrt chain lint %s\n", chainName)
		return nil
	}
	fmt.Printf("next: shrt chain lint %s\n", chainName)
	return nil
}

func planAll(e *env, lib *contract.Library, write, force bool) error {
	planned, kept, failed := 0, 0, []string{}
	for _, rpc := range lib.RPCs() {
		m, err := e.cat.Lookup(rpc)
		if err != nil {
			fmt.Printf("%s: %v\n", rpcTail(rpc), err)
			failed = append(failed, rpcTail(rpc))
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
				existed, err = planAllOne(e, plan, name, write, force)
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
		fmt.Printf("%d existing chain file(s) kept: -force overwrites them\n", kept)
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

func planAllOne(e *env, plan *contract.Plan, name string, write, force bool) (bool, error) {
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
		} else {
			raw, err := plan.YAML()
			if err != nil {
				return false, err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
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

func printPlanNotes(plan *contract.Plan, again string, notes, full bool) {
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
			"after a value: in the contract, re-plan with %s -write -force\n", n, again)
	}
	switch n := len(plan.Notes) - len(plan.FillNotes()) - len(gaps); {
	case n > 0 && !notes:
		fmt.Printf("%d more note(s) on why each probe is there: %s -notes\n", n, again)
	case n > 0 && !full:
		fmt.Printf("every note in full, and every step id: %s -notes -v\n", again)
	}
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
		out = append(out, rpcTail(r))
	}
	return out
}

func rpcTail(rpc string) string {
	if i := strings.LastIndex(rpc, "/"); i >= 0 {
		return rpc[i+1:]
	}
	return rpc
}

func planOptions(e *env) contract.PlanOptions {
	opts := contract.PlanOptions{Auth: e.cfg.Auth != nil, Redact: e.cfg.Redact}
	for _, name := range e.cfg.AuthProfileNames() {
		if name != config.DefaultAuthProfile {
			opts.Profiles = append(opts.Profiles, name)
		}
	}
	for name := range loginRPCs(e) {
		opts.Logins = append(opts.Logins, name)
	}
	sort.Strings(opts.Logins)
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
