package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
	"gopkg.in/yaml.v3"
)

func init() {
	register(&command{name: "chain", summary: "scaffold, list, search, lint, slice and audit chain definitions", run: runChain})
}

var chainGroup = group{
	name: "chain",
	subs: []subcommand{
		{"new", "scaffold a chain from real proto fields"},
		{"ls", "one line per chain, marking which have a safe spot"},
		{"which", "which chains exercise an rpc or assert a failure code"},
		{"lint", "static validation against the catalog"},
		{"slice", "the minimal ordered sub-chain that reproduces one step"},
		{"hollow", "read steps that passed while the response carried nothing"},
	},
}

func runChain(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return chainGroup.missing()
	}
	if isHelpArg(args[0]) {
		chainGroup.printHelp()
		return nil
	}
	switch args[0] {
	case "new":
		return chainNew(args[1:])
	case "ls", "list":
		return chainList(args[1:])
	case "which":
		return chainWhich(args[1:])
	case "lint":
		return chainLint(args[1:])
	case "slice":
		return chainSlice(ctx, args[1:])
	case "hollow":
		return chainHollow(args[1:])
	default:
		return chainGroup.unknown(args[0])
	}
}

func chainNew(args []string) error {
	fs := flag.NewFlagSet("chain new", flag.ContinueOnError)
	name := fs.String("name", "", "chain name (also the file name)")
	desc := fs.String("description", "", "what state this chain reproduces")
	force := fs.Bool("force", false, "overwrite an existing chain file")
	stdout := fs.Bool("stdout", false, "print to stdout instead of writing a file")
	setUsage(fs, "usage: shrt chain new -name <chain> <rpc>... [flags]   one step per rpc, in the order given", "")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: shrt chain new -name <chain> <rpc>...")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("-name is required")
	}

	lib, err := e.library()
	if err != nil {
		return err
	}
	c := &chain.Chain{APIVersion: chain.APIVersion, Name: *name, Description: *desc}
	refs := make([]string, 0, len(rest))
	ids := make([]string, 0, len(rest))
	for _, ref := range rest {
		m, err := e.cat.Lookup(ref)
		if err != nil {
			return err
		}
		if m.Streaming() {
			return fmt.Errorf("refusing to scaffold a step for %s: %s", m.FullName, m.StreamRefusal())
		}
		id := uniqueID(c, contractID(m))
		c.Steps = append(c.Steps, &chain.Step{ID: id, Call: m.FullName})
		refs = append(refs, m.FullName)
		ids = append(ids, id)
	}
	built, notes, err := contract.ScaffoldSteps(refs, ids, lib, e.cat)
	if err != nil {
		return err
	}
	stepNodes := &yaml.Node{Kind: yaml.SequenceNode, Content: built}
	for _, n := range notes {
		fmt.Fprintf(os.Stderr, "note: %s\n", n)
	}

	doc := &yaml.Node{}
	if err := doc.Encode(&chain.Chain{APIVersion: chain.APIVersion, Name: *name, Description: *desc}); err != nil {
		return err
	}
	setKey(doc, "steps", stepNodes)
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if *stdout {
		fmt.Print(string(raw))
		return nil
	}
	path := filepath.Join(e.chainsDir(), *name+".yaml")
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists, pass -force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d step(s))\nedit the body, then: shrt chain lint %s\n", path, len(c.Steps), *name)
	return nil
}

func setKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func contractID(m *catalog.Method) string {
	return contract.For(m).StepID()
}

func uniqueID(c *chain.Chain, base string) string {
	id := base
	for i := 2; ; i++ {
		if _, exists := c.Step(id); !exists {
			return id
		}
		id = fmt.Sprintf("%s_%d", base, i)
	}
}

func chainList(args []string) error {
	fs := flag.NewFlagSet("chain ls", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	long := fs.Bool("long", false, "print the full description of each chain, one block per chain")
	setUsage(fs, "usage: shrt chain ls [-long] [-json]   one line per chain under paths.chains, marking which have a safe spot and which are kept red",
		"\nexit codes:\n  0  listed, a chain that does not load included as such\n"+
			"  1  a flag that cannot be parsed, or a setup that cannot load (no .shrt/config.yaml)\n")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	chains, broken, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil {
		return err
	}
	type row struct {
		Name        string `json:"name"`
		Steps       int    `json:"steps"`
		SafeSpot    bool   `json:"safe_spot"`
		Proposed    bool   `json:"proposed,omitempty"`
		KeptRed     bool   `json:"kept_red,omitempty"`
		Description string `json:"description,omitempty"`
		Path        string `json:"path"`
		File        string `json:"file_differs,omitempty"`
	}
	sort.SliceStable(chains, func(i, j int) bool { return chains[i].Name < chains[j].Name })
	rows := make([]row, 0, len(chains))
	for _, c := range chains {
		rows = append(rows, row{
			Name: c.Name, Steps: len(c.Steps), SafeSpot: e.store.HasSafeSpot(c.Name), Proposed: e.store.HasProposal(c.Name),
			KeptRed:     len(c.KeptRed) > 0,
			Description: c.Description, Path: c.SourcePath, File: mismatchedFile(e, c),
		})
	}
	if *asJSON {
		return emitJSON(rows)
	}
	for _, b := range broken {
		fmt.Printf("! %s\n", b)
	}
	nameW := 0
	for _, r := range rows {
		if n := len([]rune(r.Name)); n > nameW {
			nameW = n
		}
	}
	for _, r := range rows {
		mark := " "
		if r.SafeSpot {
			mark = "*"
		}
		if r.Proposed {
			mark = "?"
		}
		if r.KeptRed {
			mark += "R"
		} else {
			mark += " "
		}
		if *long {
			fmt.Printf("%s %s  %d step(s)  %s\n", mark, r.Name, r.Steps, r.Path)
			for _, line := range descriptionLines(r.Description) {
				if line == "" {
					fmt.Println()
					continue
				}
				fmt.Printf("    %s\n", line)
			}
			fmt.Println()
			continue
		}
		fmt.Printf("%s %-*s %2d step(s)  %s\n", mark, nameW, r.Name, r.Steps, summarise(r.Description, descWidth(nameW)))
		if r.File != "" {
			fmt.Printf("  %-*s  (file %s: its name: differs from its file name; chain lint says how to make them agree)\n", nameW, "", r.File)
		}
	}
	fmt.Printf("\n%d chain(s), * = has a safe spot, ? = a proposal awaits approval, R = kept red (fails on purpose, "+
		"its kept_red pins name what the backend still gets wrong)", len(rows))
	if *long {
		fmt.Print("\n")
		return nil
	}
	fmt.Print("; -long for the full description, -json for every field\n")
	return nil
}

const lsLineWidth = 110

func descWidth(nameW int) int {
	w := lsLineWidth - (nameW + 16)
	if w < 24 {
		return 24
	}
	return w
}

func summarise(description string, width int) string {
	first, _, _ := strings.Cut(description, "\n")
	first = strings.TrimSpace(strings.ReplaceAll(first, "\r", ""))
	r := []rune(first)
	if len(r) <= width {
		return first
	}
	if width < 2 {
		return string(r[:width])
	}
	return strings.TrimRight(string(r[:width-1]), " ") + "\u2026"
}

func descriptionLines(description string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(description, "\r\n", "\n"), "\n") {
		out = append(out, strings.TrimRight(line, " \t"))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func chainLint(args []string) error {
	fs := flag.NewFlagSet("chain lint", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	strict := fs.Bool("strict", false, "treat the assertion-quality warnings as errors: an assertion that cannot fail (unfailable-assertion), "+
		"a step asserting nothing (asserts-nothing), an allow_fail that does nothing (inert-allow-fail), an export a later step "+
		"silently overwrites (export-overwritten), arithmetic such as ${a.qty}+${b.qty} in an equals on a numeric field, compared "+
		"as text and never computed (interpolated-arithmetic), and a step expecting success that asserts only the verdict although "+
		"its rpc's contract declares response facts (envelope-only). Other warnings are not promoted")
	setUsage(fs, "usage: shrt chain lint [<chain>...] [flags]   every chain under paths.chains when none is named",
		"\nexit codes:\n  0  no lint error\n  1  a lint error, or under -strict an assertion-quality warning\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	targets, broken, err := resolveChains(e, rest)
	if err != nil {
		return err
	}
	if len(targets) == 0 && len(broken) == 0 {
		return fmt.Errorf("no chains under %s, so nothing was checked — "+
			"a lint of nothing is not a clean lint.\nScaffold one with 'shrt chain new -name <chain> <rpc>...', "+
			"or let the contract compose it: 'shrt contract plan <rpc> -write'", e.chainsDir())
	}

	type report struct {
		Chain  string        `json:"chain"`
		Issues []chain.Issue `json:"issues"`
	}
	reports := []report{}
	errCount := len(broken)
	for _, b := range broken {
		reports = append(reports, report{Chain: "<unloadable>", Issues: []chain.Issue{
			{Severity: chain.SeverityError, Message: b.Error()},
		}})
	}
	lib, err := e.library()
	if err != nil {
		return err
	}
	opts := contract.ChainLintOptions{Strict: *strict, Library: lib}
	opts.Chain.Env = os.LookupEnv
	opts.Chain.Redact = append([]string{}, e.cfg.Redact...)
	if covers, err := runner.AuthCoverage(e.cfg, e.cat); err == nil {
		opts.Chain.AuthHeader = covers
		opts.Chain.AuthEnv = runner.AuthEnv(e.cfg)
	}
	opts.Chain.AuthProfiles = append([]string{}, e.cfg.AuthProfileNames()...)
	if authIssues := lintAuthBodies(e.cfg); len(authIssues) > 0 {
		errCount += len(authIssues)
		reports = append(reports, report{Chain: "<config>", Issues: authIssues})
	}
	if dupes := chain.LintCorpus(targets); len(dupes) > 0 {
		errCount += len(dupes)
		reports = append(reports, report{Chain: "<corpus>", Issues: dupes})
	}
	for _, c := range targets {
		issues := contract.LintChain(c, e.cat, opts)
		if mm := nameMismatchIn(e, c); mm != nil {
			issues = append([]chain.Issue{{Severity: chain.SeverityWarn, Kind: chain.KindNameMismatch, Message: mm.Error() + ": " + mm.Remedy()}}, issues...)
		}
		for _, i := range issues {
			if i.Severity == chain.SeverityError {
				errCount++
			}
		}
		reports = append(reports, report{Chain: c.Name, Issues: issues})
	}
	if *asJSON {
		if err := emitJSON(reports); err != nil {
			return err
		}
	} else {
		for _, r := range reports {
			if len(r.Issues) == 0 {
				fmt.Printf("ok   %s\n", r.Chain)
				continue
			}
			status := "warn"
			for _, i := range r.Issues {
				if i.Severity == chain.SeverityError {
					status = "FAIL"
				}
			}
			fmt.Printf("%-4s %s\n", status, r.Chain)
			for _, i := range r.Issues {
				where := ""
				if i.Step != "" {
					where = " [" + i.Step + "]"
				}
				fmt.Printf("%-5s %s %s\n", strings.ToUpper(i.Severity), where, i.Message)
			}
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d lint error(s)", errCount)
	}
	if !*strict && !*asJSON {
		quality := 0
		for _, r := range reports {
			for _, i := range r.Issues {
				if i.Severity == chain.SeverityWarn && chain.IsAssertionQualityIssue(i) {
					quality++
				}
			}
		}
		if quality > 0 {
			fmt.Printf("\nexit 0, with %d assertion-quality warning(s) above: a step that asserts "+
				"nothing, or whose assertion cannot fail, is reported but does not fail this command. "+
				"'shrt chain lint -strict' fails on them, and is what a CI gate should run — "+
				"'lint && run' without it is green on a chain that proves nothing.\n", quality)
		}
	}
	return nil
}

func lintAuthBodies(cfg *config.Config) []chain.Issue {
	issues := []chain.Issue{}
	if names := cfg.HandWrittenAuthHeaders(); len(names) > 0 {
		issues = append(issues, chain.Issue{Severity: chain.SeverityError, Message: config.HandWrittenAuthProblem(names)})
	}
	profiles := cfg.AuthProfiles()
	for _, name := range cfg.AuthProfileNames() {
		p := profiles[name]
		if p == nil {
			continue
		}
		for _, problem := range chain.AuthBodyReferenceProblems(p.Body) {
			issues = append(issues, chain.Issue{Severity: chain.SeverityError, Message: fmt.Sprintf(
				"auth profile %q body: %s, so every run that needs this profile dies at its first step", name, problem)})
		}
	}
	return issues
}

func resolveChains(e *env, args []string) ([]*chain.Chain, []error, error) {
	if len(args) == 0 {
		return chain.LoadDirPartial(e.chainsDir())
	}
	out := make([]*chain.Chain, 0, len(args))
	for _, ref := range args {
		c, err := chain.Resolve(e.chainsDir(), ref)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, c)
	}
	return out, nil, nil
}

type optionalString struct {
	set   bool
	value string
}

func (o *optionalString) String() string { return o.value }

func (o *optionalString) IsBoolFlag() bool { return true }

func (o *optionalString) Set(s string) error {
	o.set = true
	if s != "true" {
		o.value = s
	}
	return nil
}

func nameMismatchIn(e *env, c *chain.Chain) *chain.NameMismatchError {
	var mm *chain.NameMismatchError
	if c == nil || !errors.As(chain.NameMismatch(c), &mm) {
		return nil
	}
	dir, err1 := filepath.Abs(filepath.Dir(c.SourcePath))
	chains, err2 := filepath.Abs(e.chainsDir())
	if err1 != nil || err2 != nil || dir != chains {
		return nil
	}
	return mm
}

func mismatchedFile(e *env, c *chain.Chain) string {
	if nameMismatchIn(e, c) == nil {
		return ""
	}
	return filepath.Base(c.SourcePath)
}

func (e *env) chainName(ref string) string {
	if strings.ContainsAny(ref, "/\\") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return ref
	}
	c, err := chain.Resolve(e.chainsDir(), ref)
	if err != nil || c.Name == ref || nameMismatchIn(e, c) == nil {
		return ref
	}
	fmt.Fprintf(os.Stderr, "shrt: %s is chain %s (its name: differs from its file name), so its runs and safe spot are %s's\n",
		rel(e.cfg.Root, c.SourcePath), c.Name, c.Name)
	return c.Name
}
