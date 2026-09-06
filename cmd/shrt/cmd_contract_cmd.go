package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/contract"
)

func init() {
	register(&command{
		name:    "contract",
		summary: "author and use the curated RPC contracts agents write chains from",
		run:     runContract,
	})
}

var contractGroup = group{
	name: "contract",
	subs: []subcommand{
		{"init", "scaffold the curated contract; re-running keeps what you wrote"},
		{"lint", "validate contracts against the descriptor"},
		{"show", "generated schema plus the curated semantics for one rpc"},
		{"plan", "compose an ordered chain from the dependency graph"},
		{"status", "contract coverage per domain"},
		{"quality", "score each contract against the curation terms"},
	},
}

func runContract(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return contractGroup.missing()
	}
	if isHelpArg(args[0]) {
		contractGroup.printHelp()
		return nil
	}
	switch args[0] {
	case "init", "scaffold":
		return contractInit(args[1:])
	case "lint":
		return contractLint(args[1:])
	case "show":
		return contractShow(args[1:])
	case "plan":
		return contractPlan(args[1:])
	case "status", "coverage":
		return contractStatus(args[1:])
	case "quality":
		return contractQuality(args[1:])
	default:
		return contractGroup.unknown(args[0])
	}
}

func (e *env) contractsDir() string { return e.cfg.Abs(e.cfg.Paths.Contracts) }

func (e *env) library() (*contract.Library, []error, error) {
	lib, broken, err := contract.LoadLibrary(e.contractsDir())
	for _, b := range broken {
		fmt.Fprintf(os.Stderr,
			"warning: an overlay under %s did not load, so every rpc it curates is treated as having NO "+
				"contract — a plan or slice built now silently drops its required/from/needs wiring: %v\n",
			e.contractsDir(), b)
	}
	return lib, broken, err
}

func contractInit(args []string) error {
	fs := flag.NewFlagSet("contract init", flag.ContinueOnError)
	all := fs.Bool("all", false, "scaffold every domain in the catalog")
	force := fs.Bool("force", false, "discard existing curation instead of carrying it forward")
	stdout := fs.Bool("stdout", false, "print instead of writing")
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
		return fmt.Errorf("usage: shrt contract init <domain>... | -all")
	}

	lib, _, err := e.library()
	if err != nil {
		return err
	}
	for _, domain := range targets {
		methods, ok := byDomain[domain]
		if !ok {
			return fmt.Errorf("no domain %q in the catalog", domain)
		}
		prior := lib
		if *force {
			prior = nil
		}
		raw, err := contract.RenderOverlay(contract.ScaffoldOverlay(domain, methods, prior, e.cat.Methods()))
		if err != nil {
			return err
		}
		if *stdout {
			fmt.Printf("--- %s\n%s\n", domain, raw)
			continue
		}
		path := filepath.Join(e.contractsDir(), domain+".yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d rpc(s))\n", rel(e.cfg.Root, path), len(methods))
	}
	if !*stdout {
		fmt.Printf("\nfill every %s, then: shrt contract lint\n", contract.TodoMarker)
	}
	return nil
}

func contractLint(args []string) error {
	fs := flag.NewFlagSet("contract lint", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	only := fs.String("domain", "", "report only this domain, ignoring the rest of the library")
	quiet := fs.Bool("errors-only", false, "suppress warnings")
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
	lib, broken, err := e.library()
	if err != nil {
		return err
	}
	if len(broken) == 0 {
		if len(lib.Overlays) == 0 {
			return fmt.Errorf("no contract overlays under %s, so nothing was checked — "+
				"a lint of nothing is not a clean lint.\nScaffold one with 'shrt contract init <domain>'; "+
				"'shrt contract init' with no argument lists the domains in this catalog", e.contractsDir())
		}
		if *only != "" && !hasDomain(lib, *only) {
			return fmt.Errorf("no overlay for domain %q in %s, so nothing was checked — "+
				"scaffold it with 'shrt contract init %s'", *only, e.contractsDir(), *only)
		}
	}
	all := []contract.Issue{}
	for _, b := range broken {
		all = append(all, contract.Issue{Severity: contract.SeverityError, Message: b.Error()})
	}
	all = append(all, contract.LintAll(lib, e.cat, e.cfg.AuthProfileNames())...)

	issues := []contract.Issue{}
	for _, i := range all {
		if *quiet && i.Severity != contract.SeverityError {
			continue
		}
		if *only != "" && i.Domain != *only && !(i.Domain == "" && strings.Contains(i.Message, *only)) {
			continue
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
			fmt.Printf("\n%s\n", tally(issues))
		}
		if reach := contract.ReferencedOutsideLibrary(lib, e.cat, *only); !reach.Empty() {
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
	errCount := 0
	for _, i := range issues {
		if i.Severity == contract.SeverityError {
			errCount++
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d contract error(s)", errCount)
	}
	return nil
}

func hasDomain(lib *contract.Library, domain string) bool {
	for _, o := range lib.Overlays {
		if o.Domain == domain {
			return true
		}
	}
	return false
}

func tally(issues []contract.Issue) string {
	errs, warns := 0, 0
	for _, i := range issues {
		if i.Severity == contract.SeverityError {
			errs++
			continue
		}
		warns++
	}
	return fmt.Sprintf("%d error(s), %d warning(s)", errs, warns)
}

func shortRPC(rpc string) string {
	if i := strings.LastIndex(rpc, "."); i >= 0 {
		return rpc[i+1:]
	}
	return rpc
}
