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
	loginUnsent := false
	err := initRepo(ctx, args, &loginUnsent)
	if err != nil || !loginUnsent {
		return err
	}
	vars := "the login credentials"
	if unset := unexportedLoginVars(); len(unset) > 0 {
		vars = strings.Join(unset, ", ")
	}
	return exitWith(3, "init incomplete: conventions not observed; export %s and re-run shrt init", vars)
}

func unexportedLoginVars() []string {
	root, err := os.Getwd()
	if err != nil {
		return nil
	}
	cfg, err := config.Load(root)
	if err != nil || cfg.Auth == nil {
		return nil
	}
	unset := []string{}
	for _, name := range chain.AuthBodyEnvNames(cfg.Auth.Body) {
		if _, set := os.LookupEnv(name); !set {
			unset = append(unset, name)
		}
	}
	return unset
}

func initRepo(ctx context.Context, args []string, loginUnsent *bool) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	baseURL := fs.String("base-url", "http://127.0.0.1:8080", "backend the chains run against")
	proto := fs.String("proto", "", "proto module path passed to buf build (a dir with buf.yaml)")
	build := fs.Bool("build", true, "build the descriptor now")
	agents := fs.Bool("agents", true, "install the Claude skill and subagent into .claude/")
	force := fs.Bool("force", false, "overwrite the installed docs, the agent kit, .shrt/ci-gate.sh and .shrt/chains/example.yaml.template, which are build output; your config and your own chains are kept")
	verbose := fs.Bool("v", false, "also explain what the written files do, and print the conventions: block to paste when init cannot observe it")
	forceConfig := fs.Bool("force-config", false, "ALSO rewrite an existing .shrt/config.yaml from defaults, discarding your auth, conventions and volatile paths")
	setUsage(fs, "usage: shrt init [flags]   write .shrt/, build the descriptor, install the Claude skill and subagent; "+
		"re-running keeps your config and chains",
		"\nexit codes:\n  0  .shrt/ written, or already there and refreshed\n"+
			"  1  init stopped: a flag that cannot be parsed, a .shrt/config.yaml that does not parse, a file it\n"+
			"     could not write\n"+
			"  2  the files were written but the descriptor did not build; fix the cause and run 'shrt catalog build'\n"+
			"  3  the files were written but the login credentials are not exported, so conventions were not observed;\n"+
			"     export them and re-run shrt init\n")
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
			fmt.Println("      -force keeps your config; -force-config rebuilds it from defaults, discarding its auth, conventions and volatile paths")
		}
	} else {
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("write %s: target.base_url %s%s\n", rel(root, cfgPath), cfg.Target.BaseURL, baseURLSource(portFile != "", baseURLGiven))
		wroteConfig = true
	}
	loaded, err := config.Load(root)
	if err != nil && !wroteConfig {
		return fmt.Errorf("%s does not parse, so init stopped: %w\n%s", rel(root, cfgPath), err, brokenConfigAdvice)
	}
	if err != nil {
		return err
	}

	kept, made := []string{}, []string{}
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
		made = append(made, shown)
	}
	if len(made) > 0 {
		fmt.Printf("write %s\n", strings.Join(made, ", "))
	}
	if len(kept) > 0 {
		fmt.Printf("keep  %s (already present)\n", strings.Join(kept, ", "))
	}

	docs, err := agentkit.Install(root, agentkit.DocAssets(), *force)
	if err != nil {
		return err
	}
	if len(docs) > 0 {
		fmt.Printf("write %s\n", strings.Join(docs, ", "))
	}
	if len(docs) == 0 {
		fmt.Printf("keep  %s/ (already present)\n", agentkit.DocsDir)
	}
	gateFiles := []string{}
	switch wrote, err := agentkit.InstallGateScript(root, *force); {
	case err != nil:
		return err
	case wrote:
		gateFiles = append(gateFiles, agentkit.GateScriptPath)
	}
	switch wrote, err := agentkit.InstallQualityBaseline(root); {
	case err != nil:
		return err
	case wrote:
		gateFiles = append(gateFiles, agentkit.QualityBaselinePath+" (0)")
	}
	if len(gateFiles) > 0 {
		fmt.Printf("write %s\n", strings.Join(gateFiles, ", "))
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
		if len(written) > 0 {
			fmt.Printf("write %s\n", strings.Join(written, ", "))
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
			if werr := writeExampleChain(root, loaded, *force, *verbose); werr != nil {
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
		if err := scaffoldAuth(loaded, *verbose); err != nil {
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
	*loginUnsent = strings.HasPrefix(unobserved, "the login was not sent")
	if unobserved != "" || (wroteConfig && loaded.Conventions.EnvelopePath == "") {
		guidePath, _ := exampleEnvelope(loaded)
		switch {
		case *loginUnsent && !*verbose:
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
	if err := writeExampleChain(root, loaded, *force, *verbose); err != nil {
		return err
	}
	if *loginUnsent {
		return nil
	}
	fmt.Println()
	if !wroteConfig && baseURLGiven && loaded.Target.BaseURL != *baseURL {
		fmt.Printf("-base-url %s was NOT applied: %s/%s already existed and still says target.base_url: %s; edit it, or pass -force-config to rebuild it\n",
			*baseURL, config.DirName, config.FileName, loaded.Target.BaseURL)
	}
	if loaded.Auth == nil {
		fmt.Printf("declare auth: in %s/%s: nothing in the descriptor looked like a login rpc, so every authenticated call is a 401 until it exists\n",
			config.DirName, config.FileName)
	}
	if !wroteConfig {
		fmt.Println("next: shrt doctor")
		return nil
	}
	fmt.Printf("next: shrt doctor, then %s/README.md \"Quickstart\"\n", agentkit.DocsDir)
	return nil
}

func baseURLSource(portFile, given bool) string {
	switch {
	case portFile:
		return ", from .port (-base-url picks another)"
	case given:
		return ""
	}
	return ", the default: set it to your backend, or pass -base-url"
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

func scaffoldAuth(cfg *config.Config, verbose bool) error {
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
	fmt.Printf("write %s/%s auth: %s, GUESSED from the descriptor: check it and the credential variables it names\n",
		config.DirName, config.FileName, best.Method.FullName)
	names := make([]string, 0, len(cfg.Auth.Profiles))
	for name := range cfg.Auth.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	role := map[string]bool{}
	for _, r := range roles {
		name, _, _ := strings.Cut(r, " ")
		role[name] = true
	}
	for _, name := range names {
		if !role[name] {
			fmt.Printf("      profile %-10s %s\n", name, cfg.Auth.Profiles[name].Call)
		}
	}
	for _, r := range roles {
		fmt.Printf("      role profile %s\n", r)
	}
	if hint := roleProfileHint(cfg); hint != "" && (verbose || len(readmeAccounts(rootReadme(cfg.Root))) > 0) {
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

func writeExampleChain(root string, cfg *config.Config, force, verbose bool) error {
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
	if verbose {
		fmt.Println("      every REPLACE_ME in it is a placeholder for your rpcs and fields; copy it to <name>.yaml and fill them in")
	}
	if ok == "" {
		fmt.Printf("      its steps assert %s equals %s: envelope_ok is not declared, and the\n"+
			"      success value is data shrt will not guess. Declare it, then write that value there\n", path, exampleOKPlaceholder)
	}
	return nil
}
