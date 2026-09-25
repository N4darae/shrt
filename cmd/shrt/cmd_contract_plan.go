package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
)

func contractPlan(args []string) error {
	fs := flag.NewFlagSet("contract plan", flag.ContinueOnError)
	name := fs.String("name", "", "chain name, defaults to one derived from the rpc")
	write := fs.Bool("write", false, "write the composed chain into the chains directory")
	force := fs.Bool("force", false, "overwrite an existing chain file")
	showNotes := fs.Bool("notes", false, "print every note in full, and every step id")
	verbose := fs.Bool("v", false, "print every step id")
	setUsage(fs, "usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write [-name <chain>] [-force]] [-notes] [-v]",
		"\nwithout -write it prints the order, the step count per probe group, and a gap: line for each thing it could\n"+
			"not plan or assert; -write writes the chain to the chains directory.\n"+
			"\nexit codes:\n"+
			"  0  the plan was printed, or with -write written; also when a required field still has no\n"+
			"     usable value (a note names it and chain lint errors on it until you fill it)\n"+
			"  1  nothing was planned or written\n"+
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
	if len(rest) == 0 {
		return fmt.Errorf("usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write]")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, err := e.library()
	if err != nil {
		return err
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
		if *verbose || *showNotes {
			ids := make([]string, 0, len(plan.Chain.Steps))
			for _, st := range plan.Chain.Steps {
				ids = append(ids, st.ID)
			}
			fmt.Printf("step ids: %s\n", strings.Join(ids, ", "))
		}
		printPlanNotes(plan, again, *showNotes)
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
	printPlanNotes(plan, again, *showNotes)
	if plan.UnfilledCount() > 0 {
		fmt.Printf("next: fill the test data, then shrt chain lint %s\n", chainName)
		return nil
	}
	fmt.Printf("next: shrt chain lint %s\n", chainName)
	return nil
}

func printPlanNotes(plan *contract.Plan, again string, all bool) {
	if all {
		for _, n := range plan.Notes {
			fmt.Printf("note: %s\n", n)
		}
	}
	gaps := plan.GapNotes()
	if !all {
		for _, n := range plan.FillNotes() {
			fmt.Printf("fill: %s\n", n)
		}
		for _, n := range gaps {
			fmt.Printf("gap: %s\n", clipText(n, planGapWidth))
		}
	}
	if n := plan.UnfilledCount(); n > 0 {
		fmt.Printf("%d required field(s) carry no test data, and chain lint errors on each until filled; "+
			"after a value: in the contract, re-plan with %s -write -force\n", n, again)
	}
	if n := len(plan.Notes) - len(plan.FillNotes()) - len(gaps); n > 0 && !all {
		more := ""
		if len(gaps) > 0 {
			more = ", and each gap in full"
		}
		fmt.Printf("%d more note(s) on why each probe is there%s: %s -notes\n", n, more, again)
	}
}

const planGapWidth = 160

func clipText(text string, width int) string {
	if len(text) <= width {
		return text
	}
	cut := width - 3
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "..."
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
	opts := contract.PlanOptions{Auth: e.cfg.Auth != nil}
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
