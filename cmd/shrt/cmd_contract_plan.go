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
	setUsage(fs, "usage: shrt contract plan <rpc>[@alias] [<rpc>[@alias] ...] [-write [-name <chain>] [-force]]",
		"\nexit codes:\n"+
			"  0  the plan was printed, or with -write written; also when a required field still has no\n"+
			"     usable value (a note names it and chain lint errors on it until you fill it)\n"+
			"  1  nothing was planned or written\n"+
			"     - no rpc named, an rpc the catalog does not have, or an alias its contract does not declare\n"+
			"     - a streaming target, or a contract graph that pulls in a streaming rpc\n"+
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
	if !*write {
		fmt.Printf("# order: %s\n", strings.Join(shortNames(plan.Order), " -> "))
		for _, n := range plan.Notes {
			fmt.Printf("# note: %s\n", n)
		}
		if n := plan.UnfilledCount(); n > 0 {
			fmt.Printf("# %d required field(s) carry no test data yet — chain lint ERRORs on each until "+
				"filled. The plan derives order and wiring; the values are yours.\n", n)
		}
		fmt.Print(string(raw))
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
	fmt.Printf("wrote %s\n  order: %s\n", rel(e.cfg.Root, path), strings.Join(shortNames(plan.Order), " -> "))
	for _, n := range plan.Notes {
		fmt.Printf("  note: %s\n", n)
	}
	if n := plan.UnfilledCount(); n > 0 {
		fmt.Printf("\n%d required field(s) still carry no test data. 'shrt chain lint' will report an ERROR "+
			"for each until you fill them, and that is the division of labour: the plan derives the ORDER and "+
			"the WIRING, you supply the VALUES. The note lines above name every one.\n", n)
	}
	fmt.Printf("\nnext: fill the test data, then shrt chain lint %s\n", chainName)
	return nil
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
	return opts
}
