package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/contract"
)

func contractPlan(args []string) error {
	fs := flag.NewFlagSet("contract plan", flag.ContinueOnError)
	name := fs.String("name", "", "chain name, defaults to one derived from the rpc")
	write := fs.Bool("write", false, "write the composed chain into the chains directory")
	force := fs.Bool("force", false, "overwrite an existing chain file")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: shrt contract plan <rpc> [-write]")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, _, err := e.library()
	if err != nil {
		return err
	}
	m, err := e.cat.Lookup(rest[0])
	if err != nil {
		return err
	}
	chainName := *name
	if chainName == "" {
		chainName = contract.DomainOf(m) + "-" + strings.ToLower(m.Name)
	}
	plan, err := contract.BuildPlan(m.FullName, lib, e.cat, chainName)
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
		if n := unfilledCount(plan); n > 0 {
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
	if n := unfilledCount(plan); n > 0 {
		fmt.Printf("\n%d required field(s) still carry no test data. 'shrt chain lint' will report an ERROR "+
			"for each until you fill them, and that is the division of labour: the plan derives the ORDER and "+
			"the WIRING, you supply the VALUES. The note lines above name every one.\n", n)
	}
	fmt.Printf("\nnext: fill the test data, then shrt chain lint %s\n", chainName)
	return nil
}

func unfilledCount(plan *contract.Plan) int {
	n := 0
	for _, note := range plan.Notes {
		if strings.Contains(note, "has no usable value") {
			n++
		}
	}
	return n
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
