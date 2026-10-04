package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func init() {
	register(&command{
		name:    "contract",
		summary: "author and use the curated RPC contracts agents write chains from",
		run:     contractGroup.run,
	})
}

var contractGroup = group{
	name: "contract",
	subs: []subcommand{
		{"init", "scaffold", "scaffold the curated contract; re-running keeps what you wrote", noCtx(contractInit)},
		{"lint", "", "validate contracts against the descriptor", noCtx(contractLint)},
		{"show", "", "generated schema plus the curated semantics for one rpc", noCtx(contractShow)},
		{"plan", "", "compose an ordered chain from the dependency graph", noCtx(contractPlan)},
		{"status", "coverage", "contract coverage per domain", noCtx(contractStatus)},
		{"quality", "", "score each contract against the curation terms", noCtx(contractQuality)},
	},
}

func (e *env) contractsDir() string { return e.cfg.Abs(e.cfg.Paths.Contracts) }

func (e *env) library() (*contract.Library, error) {
	lib, broken, err := contract.LoadLibraryIn(e.contractsDir(), e.cat)
	if err != nil {
		return nil, err
	}
	if len(broken) > 0 {
		return nil, brokenOverlaysError(e.contractsDir(), broken)
	}
	return lib, nil
}

func loadLibrary() (*env, *contract.Library, error) {
	e, err := loadEnv(true)
	if err != nil {
		return nil, nil, err
	}
	lib, err := e.library()
	return e, lib, err
}

func brokenOverlaysError(dir string, broken []error) error {
	lines := make([]string, 0, len(broken))
	for _, b := range broken {
		lines = append(lines, "  "+b.Error())
	}
	return fmt.Errorf("%d contract overlay(s) under %s do not load, so nothing was checked or composed: "+
		"reading the rest would treat every rpc they curate as having no contract and silently drop its "+
		"required/from/needs wiring and checks. Fix them, then rerun:\n%s", len(broken), dir, strings.Join(lines, "\n"))
}

func sameYAML(a, b []byte) bool {
	var x, y any
	if yaml.Unmarshal(a, &x) != nil || yaml.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func contractInit(args []string) error {
	fs := flag.NewFlagSet("contract init", flag.ContinueOnError)
	all := fs.Bool("all", false, "scaffold every domain in the catalog")
	force := fs.Bool("force", false, "discard existing curation instead of carrying it forward")
	stdout := fs.Bool("stdout", false, "print instead of writing")
	setUsage(fs, "usage: shrt contract init <domain>... | -all [flags]   with no domain, lists the domains in the catalog",
		"\nexit codes:\n  0  scaffolded or unchanged; with no domain, the domains were listed\n"+
			"  1  an unknown domain, a contract overlay that does not parse, a flag that cannot be parsed, or a\n"+
			"     setup that cannot load\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	byDomain := contract.Domains(e.cat.Methods())

	targets := rest
	if *all {
		targets = contract.DomainNames(e.cat.Methods())
	}
	if len(targets) == 0 {
		fmt.Println("domains in this catalog:")
		for _, d := range contract.DomainNames(e.cat.Methods()) {
			fmt.Printf("  %-14s %d rpc(s)\n", d, len(byDomain[d]))
		}
		fmt.Println("\nscaffold one: shrt contract init <domain>...  (or -all)")
		return nil
	}

	lib, err := e.library()
	if err != nil {
		return err
	}
	todos := 0
	for _, domain := range targets {
		methods, ok := byDomain[domain]
		if !ok {
			return fmt.Errorf("no domain %q in the catalog", domain)
		}
		prior := lib
		if *force {
			prior = nil
		}
		path := filepath.Join(e.contractsDir(), domain+".yaml")
		node := contract.ScaffoldOverlay(domain, methods, prior, e.cat.Methods())
		if written, err := os.ReadFile(path); err == nil && !*force {
			contract.KeepWrittenStyle(node, written)
		}
		raw, err := contract.RenderOverlay(node)
		if err != nil {
			return err
		}
		if *stdout {
			fmt.Printf("--- %s\n%s\n", domain, raw)
			continue
		}
		verb, text := "wrote", raw
		if current, err := os.ReadFile(path); err == nil && sameYAML(current, raw) {
			verb, text = "unchanged", current
		} else if err := writePlanFile(path, raw); err != nil {
			return err
		}
		n := strings.Count(string(text), contract.TodoMarker)
		todos += n
		fmt.Printf("%s %s (%d rpc(s), %d %s)\n", verb, rel(e.cfg.Root, path), len(methods), n, contract.TodoMarker)
	}
	if !*stdout {
		if todos == 0 {
			fmt.Printf("\nno %s left in these overlays; check them: shrt contract lint\n", contract.TodoMarker)
		} else {
			fmt.Printf("\nfill the %d %s(s), then: shrt contract lint\n", todos, contract.TodoMarker)
		}
	}
	return nil
}

func contractLint(args []string) error {
	fs := flag.NewFlagSet("contract lint", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	only := fs.String("domain", "", "report only this domain, ignoring the rest of the library")
	quiet := fs.Bool("errors-only", false, "suppress warnings")
	setUsage(fs, "usage: shrt contract lint [<domain>] [flags]   every overlay under paths.contracts when no domain is named",
		"\nexit codes:\n  0  no contract error (warnings allowed)\n"+
			"  1  a contract error, an overlay that does not parse, no overlay to check, or bad flags\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *only == "" && len(rest) == 1 {
		*only = rest[0]
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, broken, err := contract.LoadLibraryIn(e.contractsDir(), e.cat)
	if err != nil {
		return err
	}
	if len(broken) == 0 {
		if len(lib.Overlays) == 0 {
			return fmt.Errorf("no contract overlays under %s, so nothing was checked — "+
				"a lint of nothing is not a clean lint.\nScaffold one with 'shrt contract init <domain>'; "+
				"'shrt contract init' with no argument lists the domains in this catalog", e.contractsDir())
		}
		if *only != "" && !slices.ContainsFunc(lib.Overlays, func(o *contract.Overlay) bool { return o.Domain == *only }) {
			return fmt.Errorf("no overlay for domain %q in %s, so nothing was checked — "+
				"scaffold it with 'shrt contract init %s'", *only, e.contractsDir(), *only)
		}
	}
	issues, errCount := []contract.Issue{}, len(broken)
	for _, b := range broken {
		issues = append(issues, contract.Issue{Severity: contract.SeverityError, Message: b.Error()})
	}
	for _, i := range contract.LintAll(lib, e.cat, e.cfg.AuthProfileNames()) {
		if *quiet && i.Severity != contract.SeverityError {
			continue
		}
		if *only != "" && i.Domain != *only && !(i.Domain == "" && strings.Contains(i.Message, *only)) {
			continue
		}
		if i.Severity == contract.SeverityError {
			errCount++
		}
		issues = append(issues, i)
	}

	if *asJSON {
		if err := emitJSON(issues); err != nil {
			return err
		}
	} else {
		for _, i := range issues {
			fmt.Printf("%-5s %-40s %-24s %s\n", strings.ToUpper(i.Severity), shortRPC(i.RPC), i.Field, i.Message)
		}
		if len(issues) == 0 {
			if *only != "" {
				fmt.Printf("ok   domain %s\n", *only)
			} else {
				fmt.Printf("ok   %d contract(s) across %d overlay(s)\n", lib.Count(), len(lib.Overlays))
			}
		} else {
			fmt.Printf("\n%d error(s), %d warning(s)\n", errCount, len(issues)-errCount)
		}
		if reach := contract.ReferencedOutsideLibrary(lib, e.cat, *only); len(broken) == 0 && !reach.Empty() {
			fmt.Printf("note %d referenced rpc(s) live in domains not present in this library (%s) — alias and response-path checks could not run for them\n",
				len(reach.RPCs), strings.Join(reach.Domains, ", "))
			for _, rpc := range reach.RPCs {
				fmt.Printf("     %s\n", rpc)
			}
			fmt.Printf("     author those domains ('shrt contract init %s') to turn this green into a checked green\n",
				strings.Join(reach.Domains, " "))
		}
		if pending := contract.PendingDeploy(lib); len(pending) > 0 {
			fmt.Printf("note %d failure(s) pending deploy — declared in source, absent from the running binary, so they cannot fire and are not audit debt\n", len(pending))
			for _, p := range pending {
				if *only != "" && p.Domain != *only {
					continue
				}
				fmt.Printf("     %-40s %-24s %s\n", shortRPC(p.RPC), p.Label(), p.Commit)
			}
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d contract error(s)", errCount)
	}
	return nil
}

func shortRPC(rpc string) string {
	if i := strings.LastIndex(rpc, "."); i >= 0 {
		return rpc[i+1:]
	}
	return rpc
}
