package contract

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func LintAll(lib *Library, cat *catalog.Catalog, authProfiles []string) []Issue {
	issues := LintLibrary(lib, cat)
	for _, o := range lib.Overlays {
		for _, entry := range o.EmptyEntries {
			rpc, field, _ := strings.Cut(entry, " ")
			issues = append(issues, Issue{
				Domain: o.Domain, RPC: rpc, Field: field, Severity: SeverityError,
				Message: "entry is empty — a key with nothing under it parses as null, which says " +
					"nothing and used to crash every command that read it. Fill it, or delete the line",
			})
		}
	}
	issues = append(issues, LintTodos(lib)...)
	if authProfiles != nil {
		issues = append(issues, lintAuthProfiles(lib, authProfiles)...)
	}
	return issues
}

func LintTodos(lib *Library) []Issue {
	issues := []Issue{}
	if lib == nil {
		return issues
	}
	for _, o := range lib.Overlays {
		if o == nil || o.SourcePath == "" {
			continue
		}
		raw, err := os.ReadFile(o.SourcePath)
		if err != nil {
			continue
		}
		issues = append(issues, ScanTodos(o.Domain, raw)...)
	}
	return issues
}

func lintAuthProfiles(lib *Library, authProfiles []string) []Issue {
	issues := []Issue{}
	known := map[string]bool{}
	for _, name := range authProfiles {
		known[name] = true
	}
	for _, o := range lib.Overlays {
		for rpc, c := range o.RPCs {
			if c == nil || c.Auth == "" || known[c.Auth] {
				continue
			}
			message := fmt.Sprintf("auth profile %q is not declared in the config", c.Auth)
			if len(authProfiles) > 0 {
				message += " (have: " + strings.Join(authProfiles, ", ") + ")"
			}
			issues = append(issues, Issue{
				Domain: o.Domain, RPC: rpc, Field: "auth",
				Severity: SeverityError, Message: message,
			})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].RPC < issues[j].RPC })
	return issues
}

type ChainLintOptions struct {
	Library *Library
	Strict  bool
	Chain   chain.LintOptions
}

func LintChain(c *chain.Chain, cat *catalog.Catalog, opts ChainLintOptions) []chain.Issue {
	issues := chain.LintWith(c, cat, opts.Chain)
	if opts.Library != nil {
		issues = append(issues, LintChainBodies(c, opts.Library, cat)...)
		issues = append(issues, lintWiring(c, opts.Library, cat)...)
	}
	if opts.Strict {
		issues = chain.Promote(issues, chain.IsAssertionQualityIssue)
	}
	return issues
}
