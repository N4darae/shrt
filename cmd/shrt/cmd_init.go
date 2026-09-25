package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/agentkit"
	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
)

func init() {
	register(&command{
		name:    "init",
		summary: "set up .shrt/ and the Claude agent kit in the current repo",
		run:     runInit,
	})
}

func runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	baseURL := fs.String("base-url", "http://127.0.0.1:8080", "backend the chains run against")
	proto := fs.String("proto", "", "proto module path passed to buf build (a dir with buf.yaml)")
	build := fs.Bool("build", true, "build the descriptor now")
	agents := fs.Bool("agents", true, "install the Claude skill and subagent into .claude/")
	force := fs.Bool("force", false, "overwrite the installed docs, the agent kit, .shrt/ci-gate.sh and .shrt/chains/example.yaml.template, which are build output; your config and your own chains are kept")
	verbose := fs.Bool("v", false, "also print the conventions: block to paste when init cannot observe it")
	forceConfig := fs.Bool("force-config", false, "ALSO rewrite an existing .shrt/config.yaml from defaults, discarding your auth, conventions and volatile paths")
	setUsage(fs, "usage: shrt init [flags]   write .shrt/, build the descriptor, install the Claude skill and subagent; "+
		"re-running keeps your config and chains",
		"\nexit codes:\n  0  .shrt/ written, or already there and refreshed\n"+
			"  1  init stopped: a flag that cannot be parsed, a .shrt/config.yaml that does not parse, a file it\n"+
			"     could not write\n"+
			"  2  the files were written but the descriptor did not build; fix the cause and run 'shrt catalog build'\n")
	if err := fs.Parse(args); err != nil {
		return err
	}
	baseURLGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "base-url" {
			baseURLGiven = true
		}
	})
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	portFile := ""
	if !baseURLGiven {
		if raw, err := os.ReadFile(filepath.Join(root, ".port")); err == nil {
			if port, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && port > 0 && port < 65536 {
				*baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
				portFile = *baseURL
			}
		}
	}

	cfg := config.Default()
	cfg.Root = root
	cfg.Target.BaseURL = *baseURL
	if *proto != "" {
		cfg.Descriptor.Source = *proto
	}
	cfg.Volatile = []string{"**.created_at", "**.updated_at"}
	cfg.Latency = &config.Latency{Fail: true}

	cfgPath := filepath.Join(root, config.DirName, config.FileName)
	wroteConfig := false
	if _, err := os.Stat(cfgPath); err == nil && !*forceConfig {
		fmt.Printf("keep  %s (already exists)\n", rel(root, cfgPath))
		if *force {
			fmt.Println("      -force refreshes the docs, the agent kit, .shrt/ci-gate.sh and .shrt/chains/example.yaml.template only. Your config is yours: it holds " +
				"auth, conventions and volatile paths that no default can reconstruct, and rewriting it " +
				"silently is how a declared convention disappears and a red chain turns green. " +
				"Pass -force-config if you really want it rebuilt from defaults.")
		}
	} else {
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("write %s\n", rel(root, cfgPath))
		fmt.Println("      latency: {fail: true}: a slowdown verify confirms (a slow read re-sent and slow every time) fails it, so a CI gate " +
			"is red on one; set fail: false to keep it a LATENCY warning line")
		if portFile != "" {
			fmt.Printf("      target.base_url: %s, from the port in .port at the repo root (pass -base-url to choose another)\n", portFile)
		}
		wroteConfig = true
	}
	loaded, err := config.Load(root)
	if err != nil && !wroteConfig {
		return fmt.Errorf("%s does not parse, so init stopped: %w\n%s", rel(root, cfgPath), err, brokenConfigAdvice)
	}
	if err != nil {
		return err
	}

	kept := []string{}
	for _, dir := range []string{loaded.Paths.Chains, loaded.Paths.Runs, loaded.Paths.SafeSpots, loaded.Paths.Contracts} {
		if dir == "" {
			continue
		}
		shown := strings.TrimSuffix(filepath.ToSlash(dir), "/") + "/"
		if info, err := os.Stat(loaded.Abs(dir)); err == nil && info.IsDir() {
			kept = append(kept, shown)
			continue
		}
		if err := os.MkdirAll(loaded.Abs(dir), 0o755); err != nil {
			return err
		}
		fmt.Printf("write %s\n", shown)
	}
	if len(kept) > 0 {
		fmt.Printf("keep  %s (already present)\n", strings.Join(kept, ", "))
	}

	docs, err := agentkit.Install(root, agentkit.DocAssets(), *force)
	if err != nil {
		return err
	}
	for _, w := range docs {
		fmt.Printf("write %s\n", w)
	}
	if len(docs) == 0 {
		fmt.Printf("keep  %s/ (already present)\n", agentkit.DocsDir)
	}
	switch wrote, err := agentkit.InstallGateScript(root, *force); {
	case err != nil:
		return err
	case wrote:
		fmt.Printf("write %s (static checks, then shrt gate; run it with bash %s)\n", agentkit.GateScriptPath, agentkit.GateScriptPath)
	}

	switch added, err := ensureGitignore(root, initGitignore(loaded)); {
	case err != nil:
		return err
	case added:
		fmt.Printf("write %s\n", rel(root, filepath.Join(root, ".gitignore")))
	}

	if *agents {
		written, err := agentkit.Install(root, agentkit.ClaudeAssets(), *force)
		if err != nil {
			return err
		}
		for _, w := range written {
			fmt.Printf("write %s\n", w)
		}
		if len(written) == 0 {
			fmt.Println("keep  .claude/ agent kit (already present)")
		}
	}

	if *build {
		if err := buildDescriptor(ctx, loaded); err != nil {
			if wroteConfig {
				fmt.Print("\n" + config.ConventionsGuide + "\n")
			}
			if werr := writeExampleChain(root, loaded, *force); werr != nil {
				return werr
			}
			fmt.Printf("\nwrote %s/ and the agent kit, but the descriptor did NOT build:\n\n%v\n\n",
				config.DirName, err)
			return exitWith(2, "every catalog, contract and chain command reads the descriptor, so this repo "+
				"is not usable yet.\nFix the cause above and run 'shrt catalog build' — nothing else needs redoing.")
		}
		fmt.Printf("write %s\n", loaded.Descriptor.File)
	}
	if loaded.Auth == nil {
		if err := scaffoldAuth(loaded); err != nil {
			return err
		}
	} else if !wroteConfig {
		roles, err := addMissingRoleProfiles(loaded, cfgPath)
		if err != nil {
			return err
		}
		if len(roles) > 0 {
			fmt.Printf("write %s auth.profiles, keeping the rest of it as it was:\n", rel(root, cfgPath))
			for _, r := range roles {
				fmt.Printf("      role profile %s\n", r)
			}
		} else if hint := roleProfileHint(loaded); hint != "" && len(readmeAccounts(rootReadme(root))) > 0 {
			fmt.Println(hint)
		}
	}
	unobserved, err := declareConventions(ctx, loaded, cfgPath)
	if err != nil {
		return err
	}
	if unobserved != "" || (wroteConfig && loaded.Conventions.EnvelopePath == "") {
		guidePath, _ := exampleEnvelope(loaded)
		switch {
		case wroteConfig && strings.HasPrefix(unobserved, "the login was not sent") && !*verbose:
			fmt.Printf("conventions: not written, init did not read envelope_ok (%s); export the login credentials and re-run shrt init, "+
				"which observes %s and writes them (-v prints the block to paste)\n", unobserved, guidePath)
		case wroteConfig:
			fmt.Print("\n" + config.ConventionsGuideFor(guidePath))
			if unobserved != "" {
				fmt.Printf("init did not read envelope_ok itself: %s. Export the login credentials and re-run init to have it written.\n", unobserved)
			}
			fmt.Println()
		default:
			fmt.Printf("conventions: not declared, and init did not read envelope_ok: %s (shrt doctor says what to set)\n", unobserved)
		}
	}
	if err := writeExampleChain(root, loaded, *force); err != nil {
		return err
	}

	if !wroteConfig {
		fmt.Println("\nthis repo was already set up: init kept what it says it kept and wrote only the lines marked write.")
		if baseURLGiven && loaded.Target.BaseURL != *baseURL {
			printBaseURLNext(loaded.Target.BaseURL, *baseURL, baseURLGiven)
		}
		if loaded.Auth == nil {
			fmt.Printf("  %s/%s still declares no auth:, so every authenticated call is a 401\n",
				config.DirName, config.FileName)
		}
		fmt.Println("next: shrt doctor   # check this installation before you trust a green")
		return nil
	}

	fmt.Println("\nnext:")
	fmt.Printf("  read %s/README.md\n", agentkit.DocsDir)
	printBaseURLNext(loaded.Target.BaseURL, *baseURL, baseURLGiven || portFile != "")
	if loaded.Auth == nil {
		fmt.Printf("  declare auth: in %s/%s — nothing in this descriptor looked like a login rpc, so\n",
			config.DirName, config.FileName)
		fmt.Println("    shrt could not scaffold one; until it exists every authenticated call is a 401")
	} else {
		fmt.Printf("  export the credentials %s/%s names, and check the login it picked\n",
			config.DirName, config.FileName)
	}
	fmt.Println("  shrt doctor                        # check this installation before you trust a green")
	fmt.Println("  shrt catalog ls -filter <domain>")
	fmt.Println("  shrt contract init -all            # one curated overlay per domain, then fill the TODOs")
	fmt.Println("  shrt contract quality -phase happy # what still blocks a working chain; failures come later")
	fmt.Println("  shrt contract plan <Rpc> -write    # let the contract compose the chain")
	fmt.Println("\nan agent can author the overlays for you: ask it to use the shrt-contract-author subagent,")
	fmt.Println("one domain at a time. It reads your backend's own source; it never sends traffic.")
	return nil
}

func printBaseURLNext(have, flagValue string, given bool) {
	where := config.DirName + "/" + config.FileName
	switch {
	case !given:
		fmt.Printf("  set target.base_url in %s to the backend the chains run against — it is %s now,\n", where, have)
		fmt.Println("    and every run and confirm goes there (or pass -base-url to init)")
	case have != flagValue:
		fmt.Printf("  -base-url %s was NOT applied: %s already existed and still says target.base_url: %s,\n",
			flagValue, where, have)
		fmt.Println("    and every run and confirm goes there — edit it, or pass -force-config to rebuild it")
	default:
		fmt.Printf("  check target.base_url in %s: every run and confirm goes to %s\n", where, have)
	}
}

func initGitignore(cfg *config.Config) []string {
	out := cfg.NeverCommit()
	dir := cfg.Paths.SafeSpots
	if dir == "" || filepath.IsAbs(dir) {
		dir = config.DirName + "/safespots"
	}
	pending := strings.TrimSuffix(filepath.ToSlash(dir), "/") + "/pending/"
	for _, extra := range []string{pending, config.ScratchDir} {
		if !slices.Contains(out, extra) {
			out = append(out, extra)
		}
	}
	return out
}

func ensureGitignore(root string, want []string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	missing := []string{}
	for _, line := range want {
		if !present[line] {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	body := string(existing)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func scaffoldAuth(cfg *config.Config) error {
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		return nil
	}
	found := catalog.DetectLogins(cat)
	if len(found) == 0 || found[0].Score < loginConfidence {
		return nil
	}
	best := found[0]
	cfg.Auth = authFromCandidate(best, "API")
	for _, c := range found[1:] {
		if c.Score < loginConfidence || contract.DomainOf(c.Method) == contract.DomainOf(best.Method) {
			continue
		}
		name := contract.DomainOf(c.Method)
		if cfg.Auth.Profiles == nil {
			cfg.Auth.Profiles = map[string]*config.Auth{}
		}
		profile := authFromCandidate(c, strings.ToUpper(name))
		profile.Calls = []string{contract.PackageRootOf(c.Method) + ".*"}
		cfg.Auth.Profiles[name] = profile
	}
	roles := addRoleProfiles(cfg, best, os.Environ())
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("write %s/%s auth: %s\n", config.DirName, config.FileName, best.Method.FullName)
	names := make([]string, 0, len(cfg.Auth.Profiles))
	for name := range cfg.Auth.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("      profile %-10s %s\n", name, cfg.Auth.Profiles[name].Call)
	}
	for _, r := range roles {
		fmt.Printf("      role profile %s\n", r)
	}
	fmt.Println("      shrt GUESSED this from the descriptor — check it, and check the credential")
	fmt.Println("      variables it names before the first run")
	if hint := roleProfileHint(cfg); hint != "" {
		fmt.Println(hint)
	}
	return nil
}

const loginConfidence = 6

func authFromCandidate(c catalog.LoginCandidate, envPrefix string) *config.Auth {
	body := map[string]any{}
	if c.UserField != "" {
		body[c.UserField] = "${env." + envPrefix + "_USER}"
	}
	if c.PasswordName != "" {
		body[c.PasswordName] = "${env." + envPrefix + "_PASSWORD}"
	}
	return &config.Auth{
		Call:        c.Method.FullName,
		Body:        body,
		TokenPath:   c.TokenPath,
		ExpiresPath: c.ExpiresPath,
	}
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

const exampleEnvelopeExpect = "      - path: " + chain.DefaultEnvelopePath + "\n        equals: " + chain.DefaultEnvelopeOK + "\n"

func exampleEnvelope(cfg *config.Config) (path, ok string) {
	if p := strings.TrimSpace(cfg.Conventions.EnvelopePath); p != "" {
		ok = strings.TrimSpace(cfg.Conventions.EnvelopeOK)
		if ok == "" {
			ok = chain.DefaultEnvelopeOK
		}
		return p, ok
	}
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		return chain.DefaultEnvelopePath, chain.DefaultEnvelopeOK
	}
	if found := catalog.DetectEnvelope(cat); len(found) > 0 && found[0].Path != chain.DefaultEnvelopePath {
		return found[0].Path, ""
	}
	return chain.DefaultEnvelopePath, chain.DefaultEnvelopeOK
}

const exampleOKPlaceholder = "REPLACE_ME_SUCCESS_VALUE"

var examplePlaceholderExpect = regexp.MustCompile(`      - path: \S+\n        equals: ` + exampleOKPlaceholder + `\n`)

func untouchedExample(existing, template []byte) bool {
	return strings.Contains(string(existing), exampleOKPlaceholder) &&
		examplePlaceholderExpect.ReplaceAllLiteralString(string(existing), exampleEnvelopeExpect) == string(template)
}

func renderExampleChain(template []byte, path, ok string) []byte {
	if ok == "" {
		ok = exampleOKPlaceholder
	}
	expect := "      - path: " + path + "\n        equals: " + ok + "\n"
	return []byte(strings.ReplaceAll(string(template), exampleEnvelopeExpect, expect))
}

func writeExampleChain(root string, cfg *config.Config, force bool) error {
	example := cfg.Abs(filepath.Join(cfg.Paths.Chains, "example.yaml.template"))
	raw, err := agentkit.Read("templates/chain.example.yaml")
	if err != nil {
		return err
	}
	path, ok := exampleEnvelope(cfg)
	if existing, err := os.ReadFile(example); err == nil && !force {
		if ok == "" || !untouchedExample(existing, raw) {
			return nil
		}
		if err := os.WriteFile(example, renderExampleChain(raw, path, ok), 0o644); err != nil {
			return err
		}
		fmt.Printf("write %s (still the scaffold: its steps now assert %s equals %s)\n", rel(root, example), path, ok)
		return nil
	}
	if err := os.WriteFile(example, renderExampleChain(raw, path, ok), 0o644); err != nil {
		return err
	}
	fmt.Printf("write %s\n", rel(root, example))
	fmt.Println("      every REPLACE_ME in it is a placeholder for your rpcs and fields; copy it to <name>.yaml and fill them in")
	if ok == "" {
		fmt.Printf("      its steps assert %s equals %s: envelope_ok is not declared, and the\n"+
			"      success value is data shrt will not guess. Declare it, then write that value there\n", path, exampleOKPlaceholder)
	}
	return nil
}
