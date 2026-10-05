package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{name: "chain", summary: "scaffold, list, search, lint, slice and audit chain definitions", run: chainGroup.run})
}

var chainGroup = group{
	name: "chain",
	subs: []subcommand{
		{"new", "", "scaffold a chain from real proto fields", noCtx(chainNew)},
		{"ls", "list", "one line per chain, marking which have a safe spot", noCtx(chainList)},
		{"which", "", "which chains exercise an rpc or assert a failure code", noCtx(chainWhich)},
		{"lint", "", "static validation against the catalog", noCtx(chainLint)},
		{"slice", "", "the minimal ordered sub-chain that reproduces one step", chainSlice},
		{"pin", "", "keep a red chain's failures red in a slice and run the rest green", chainPin},
		{"hollow", "", "read steps that passed while the response carried nothing", noCtx(chainHollow)},
	},
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
		if refusal := m.StreamRefusal(); refusal != "" {
			return fmt.Errorf("refusing to scaffold a step for %s: %s", m.FullName, refusal)
		}
		id := c.FreeStepID(contract.For(m).StepID(), nil)
		c.Steps = append(c.Steps, &chain.Step{ID: id, Call: m.FullName})
		refs = append(refs, m.FullName)
		ids = append(ids, id)
	}
	raw, notes, err := contract.ScaffoldChain(*name, *desc, refs, ids, lib, e.cat)
	if err != nil {
		return err
	}
	for _, n := range notes {
		fmt.Fprintf(os.Stderr, "note: %s\n", noteLead(n))
	}
	if *stdout {
		fmt.Print(string(raw))
		return nil
	}
	path := filepath.Join(e.chainsDir(), *name+".yaml")
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists, pass -force to overwrite", path)
	}
	if err := writePlanFile(path, raw); err != nil {
		return err
	}
	n := len(c.Steps)
	if written, err := chain.LoadFile(path); err == nil {
		n = len(written.Steps)
	}
	fmt.Printf("wrote %s (%d step(s))\nedit the body, then: shrt chain lint %s\n", path, n, *name)
	return nil
}

func chainList(args []string) error {
	fs := flag.NewFlagSet("chain ls", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	long := fs.Bool("long", false, "print the full description of each chain, one block per chain")
	setUsage(fs, "usage: shrt chain ls [-long] [-json]   one line per chain under paths.chains, marking which have a safe spot, which are kept red and which wait",
		"\nmarks: * has a safe spot, ? a proposal awaits approval, R kept red (fails on purpose, its kept_red pins name what the backend still gets wrong),\n"+
			"W waits by design (its wait: steps, total at the end of its line; shrt gate -repro leaves it out, shrt gate <chain> runs it)\n"+
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
		Waits       string `json:"waits,omitempty"`
		Description string `json:"description,omitempty"`
		Path        string `json:"path"`
		File        string `json:"file_differs,omitempty"`
	}
	sort.SliceStable(chains, func(i, j int) bool { return chains[i].Name < chains[j].Name })
	rows := make([]row, 0, len(chains))
	for _, c := range chains {
		var waits time.Duration
		for _, s := range c.Steps {
			d, _ := s.WaitFor()
			waits += d
		}
		r := row{
			Name: c.Name, Steps: len(c.Steps), SafeSpot: e.store.HasSafeSpot(c.Name), Proposed: e.store.HasProposal(c.Name),
			KeptRed:     len(c.KeptRed) > 0,
			Description: c.Description, Path: c.SourcePath, File: mismatchedFile(e, c),
		}
		if waits > 0 {
			r.Waits = waitText(waits)
		}
		rows = append(rows, r)
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
		waits := ""
		if r.Waits != "" {
			mark, waits = mark+"W", "  waits "+r.Waits
		} else {
			mark += " "
		}
		if *long {
			fmt.Printf("%s %s  %d step(s)%s  %s\n", mark, r.Name, r.Steps, waits, r.Path)
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
		fmt.Printf("%s %-*s %3d step(s)%s\n", mark, nameW, r.Name, r.Steps, waits)
		if r.File != "" {
			fmt.Printf("  %-*s  (file %s: its name: differs from its file name; chain lint says how to make them agree)\n", nameW, "", r.File)
		}
	}
	shown := map[string]bool{}
	for _, r := range rows {
		shown["*"] = shown["*"] || r.SafeSpot && !r.Proposed
		shown["?"] = shown["?"] || r.Proposed
		shown["R"] = shown["R"] || r.KeptRed
		shown["W"] = shown["W"] || r.Waits != ""
	}
	legend := []string{}
	for _, m := range []struct{ mark, means string }{{"*", "has a safe spot"}, {"?", "a proposal awaits approval"}, {"R", "kept red"},
		{"W", "waits by design (shrt gate -repro leaves it out, shrt gate <chain> runs it)"}} {
		if shown[m.mark] {
			legend = append(legend, m.mark+" = "+m.means)
		}
	}
	fmt.Printf("\n%d chain(s)", len(rows))
	if len(legend) > 0 {
		fmt.Print("; " + strings.Join(legend, ", "))
	}
	fmt.Println()
	return nil
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

type lintReport struct {
	Chain  string        `json:"chain"`
	Issues []chain.Issue `json:"issues"`
}

func chainLint(args []string) error {
	fs := flag.NewFlagSet("chain lint", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	verbose := fs.Bool("v", false, "also print each clean chain, each chain's unasserted-timestamp warning under it, not one line for the whole lint, "+
		"and each step's envelope-only warning, not one line per chain")
	strict := fs.Bool("strict", false, "treat the assertion-quality warnings as errors: an assertion that cannot fail (unfailable-assertion), "+
		"a step asserting nothing (asserts-nothing), an allow_fail that does nothing (inert-allow-fail), an export a later step "+
		"silently overwrites (export-overwritten), arithmetic such as ${a.qty}+${b.qty} in an equals on a numeric field, compared "+
		"as text and never computed (interpolated-arithmetic), and a step expecting success that asserts only the verdict although "+
		"its rpc's contract declares response facts (envelope-only). Other warnings are not promoted")
	setUsage(fs, "usage: shrt chain lint [<chain>...] [flags]   every chain under paths.chains when none is named",
		"\na chain named by a path outside paths.chains (a .shrt/scratch/ slice, a repro) gets no unasserted-timestamp or envelope-only "+
			"warning: they matter for the suite, not for a repro; -strict checks them there too\n"+
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

	reports := []lintReport{}
	errCount := len(broken)
	for _, b := range broken {
		reports = append(reports, lintReport{Chain: "<unloadable>", Issues: []chain.Issue{
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
	opts.Chain.Hints = true
	if covers, err := runner.AuthCoverage(e.cfg, e.cat); err == nil {
		opts.Chain.AuthHeader = covers
		opts.Chain.AuthEnv = runner.AuthEnv(e.cfg)
	}
	opts.Chain.AuthProfiles = append([]string{}, e.cfg.AuthProfileNames()...)
	if authIssues := lintAuthBodies(e.cfg); len(authIssues) > 0 {
		errCount += len(authIssues)
		reports = append(reports, lintReport{Chain: "<config>", Issues: authIssues})
	}
	if dupes := chain.LintCorpus(targets); len(dupes) > 0 {
		errCount += len(dupes)
		reports = append(reports, lintReport{Chain: "<corpus>", Issues: dupes})
	}
	shellUnset := map[string][]string{}
	shellChains := 0
	for _, c := range targets {
		if gaps := chain.UnsetAuthEnv(c, opts.Chain); len(gaps) > 0 {
			shellChains++
			for _, g := range gaps {
				shellUnset[g.Profile] = g.Unset
			}
		}
		throwaway := !*strict && !inChainsDir(e, c)
		issues := slices.DeleteFunc(contract.LintChain(c, e.cat, opts), func(i chain.Issue) bool {
			return i.Kind == chain.KindAuthEnvUnset || throwaway && (i.Kind == chain.KindUnassertedTimestamp || i.Kind == chain.KindEnvelopeOnly)
		})
		if mm := nameMismatchIn(e, c); mm != nil {
			issues = append([]chain.Issue{{Severity: chain.SeverityWarn, Kind: chain.KindNameMismatch, Message: mm.Error() + ": " + mm.Remedy()}}, issues...)
		}
		for _, i := range issues {
			if i.Severity == chain.SeverityError {
				errCount++
			}
		}
		reports = append(reports, lintReport{Chain: c.Name, Issues: issues})
	}
	if len(shellUnset) > 0 {
		named := []string{}
		for _, p := range sortedKeys(shellUnset) {
			named = append(named, fmt.Sprintf("%s (auth profile %q)", strings.Join(shellUnset[p], ", "), p))
		}
		reports = append([]lintReport{{Chain: "<shell>", Issues: []chain.Issue{{Severity: chain.SeverityWarn, Kind: chain.KindAuthEnvUnset, Message: fmt.Sprintf(
			"login bodies read environment variables not exported in this shell: %s — shrt run refuses "+
				"the %d chain(s) that use them before sending anything until they are set",
			strings.Join(named, "; "), shellChains)}}}}, reports...)
	}
	if *asJSON {
		if err := emitJSON(reports); err != nil {
			return err
		}
	} else {
		explained := map[string]bool{}
		stamps := &stampSummary{}
		for _, r := range reports {
			said := map[string]bool{}
			if len(r.Issues) == 0 {
				if *verbose {
					fmt.Printf("ok   %s\n", r.Chain)
				}
				continue
			}
			status := "warn"
			if slices.ContainsFunc(r.Issues, isLintError) {
				status = "FAIL"
			}
			shown, issues := []chain.Issue{}, r.Issues
			if !*verbose {
				issues = chain.FoldEnvelopeOnly(issues)
			}
			for _, i := range issues {
				if *verbose || i.Kind != chain.KindUnassertedTimestamp || !stamps.add(r.Chain, i) {
					shown = append(shown, i)
				}
			}
			if len(shown) == 0 {
				continue
			}
			fmt.Printf("%-4s %s\n", status, r.Chain)
			for _, i := range shown {
				where := ""
				if i.Step != "" {
					where = " [" + i.Step + "]"
				}
				if i.Step != "" && said[i.Severity+" "+i.Message] {
					fmt.Printf("%-5s  %s%s %s, as above\n", strings.ToUpper(i.Severity), r.Chain, where, lintLead(i.Message))
					continue
				}
				said[i.Severity+" "+i.Message] = true
				fmt.Printf("%-5s  %s%s %s\n", strings.ToUpper(i.Severity), r.Chain, where, i.Message)
				if i.Why != "" && !explained[i.Why] {
					explained[i.Why] = true
					fmt.Printf("       %s\n", i.Why)
				}
			}
		}
		stamps.print(explained)
		fmt.Println(lintTally(reports))
	}
	if errCount > 0 {
		return fmt.Errorf("%d lint error(s)", errCount)
	}
	if !*strict && !*asJSON {
		gated := map[string]bool{}
		for _, c := range targets {
			gated[c.Name] = inChainsDir(e, c)
		}
		quality := 0
		for _, r := range reports {
			if !gated[r.Chain] {
				continue
			}
			for _, i := range r.Issues {
				if i.Severity == chain.SeverityWarn && chain.IsAssertionQualityIssue(i) {
					quality++
				}
			}
		}
		if quality > 0 {
			fmt.Printf("\nexit 0, but %d warning(s) above are errors under 'shrt chain lint -strict', which .shrt/ci-gate.sh runs\n", quality)
		}
	}
	return nil
}

func lintTally(reports []lintReport) string {
	total, failing, warned := 0, 0, 0
	for _, r := range reports {
		if strings.HasPrefix(r.Chain, "<") && r.Chain != "<unloadable>" {
			continue
		}
		total++
		switch {
		case slices.ContainsFunc(r.Issues, isLintError):
			failing++
		case len(r.Issues) > 0:
			warned++
		}
	}
	if failing == 0 && warned == 0 {
		return fmt.Sprintf("%d chain(s) lint clean", total)
	}
	parts := []string{}
	if failing > 0 {
		parts = append(parts, fmt.Sprintf("%d failing", failing))
	}
	if warned > 0 {
		parts = append(parts, fmt.Sprintf("%d with warnings", warned))
	}
	if clean := total - failing - warned; clean > 0 {
		parts = append(parts, fmt.Sprintf("%d clean", clean))
	}
	return fmt.Sprintf("%d chain(s): %s", total, strings.Join(parts, ", "))
}

func isLintError(i chain.Issue) bool { return i.Severity == chain.SeverityError }

type stampSummary struct {
	paths, chains []string
	why           string
}

func (s *stampSummary) add(chainName string, i chain.Issue) bool {
	rest, ok := strings.CutPrefix(i.Message, "timestamp ")
	path, _, ok2 := strings.Cut(rest, " unasserted")
	if !ok || !ok2 {
		return false
	}
	if !slices.Contains(s.paths, path) {
		s.paths = append(s.paths, path)
	}
	if !slices.Contains(s.chains, chainName) {
		s.chains = append(s.chains, chainName)
	}
	s.why = i.Why
	return true
}

func (s *stampSummary) print(explained map[string]bool) {
	if len(s.paths) == 0 {
		return
	}
	why := ""
	if s.why != "" && !explained[s.why] {
		explained[s.why] = true
		why = ": " + s.why
	}
	fmt.Printf("WARN   timestamps unasserted in %d chain(s), %s ('chain lint -v' names each step and its expect)%s\n",
		len(s.chains), chain.ListSome(s.paths, 4), why)
}

var noteSteps = regexp.MustCompile(`^steps? [a-z0-9_, ]+: `)

func noteLead(note string) string {
	note = contract.FirstSentence(note)
	head := noteSteps.FindString(note)
	note = note[len(head):]
	for _, sep := range []string{"; ", ": ", " ("} {
		if i := strings.Index(note, sep); i > 0 {
			note = note[:i]
		}
	}
	return head + strings.TrimSuffix(note, ".")
}

func lintLead(msg string) string {
	for _, sep := range []string{", ", "; ", ". ", ": "} {
		if i := strings.Index(msg, sep); i > 0 {
			msg = msg[:i]
		}
	}
	return msg
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

func inChainsDir(e *env, c *chain.Chain) bool {
	dir, err1 := filepath.Abs(filepath.Dir(c.SourcePath))
	chains, err2 := filepath.Abs(e.chainsDir())
	return err1 == nil && err2 == nil && dir == chains
}

func nameMismatchIn(e *env, c *chain.Chain) *chain.NameMismatchError {
	var mm *chain.NameMismatchError
	if c == nil || !errors.As(chain.NameMismatch(c), &mm) || !inChainsDir(e, c) {
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

func (e *env) resolveChain(ref string) (*chain.Chain, error) {
	c, err := chain.ResolveUnique(e.chainsDir(), ref)
	var clash *chain.NameClashError
	if errors.As(err, &clash) {
		return nil, clash.Relative(func(p string) string { return rel(e.cfg.Root, p) })
	}
	return c, err
}

func (e *env) resolveChainNamed(ref, name string) (*chain.Chain, error) {
	if c, err := e.resolveChain(ref); err == nil && c.Name == name {
		return c, nil
	}
	return e.resolveChain(name)
}

func (e *env) namedChain(ref string) (string, *chain.Chain, error) {
	if strings.ContainsAny(ref, "/\\") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return ref, nil, nil
	}
	c, err := e.resolveChain(ref)
	var clash *chain.NameClashError
	if errors.As(err, &clash) {
		return "", nil, err
	}
	if err != nil {
		return ref, nil, nil
	}
	if c.Name == ref || nameMismatchIn(e, c) == nil {
		return ref, c, nil
	}
	fmt.Fprintf(os.Stderr, "shrt: %s is chain %s (its name: differs from its file name), so its runs and safe spot are %s's\n",
		rel(e.cfg.Root, c.SourcePath), c.Name, c.Name)
	return c.Name, c, nil
}
