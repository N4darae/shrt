package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	force := fs.Bool("force", false, "overwrite the installed docs and agent kit, which are build output")
	forceConfig := fs.Bool("force-config", false, "ALSO rewrite an existing .shrt/config.yaml from defaults, discarding your auth, conventions and volatile paths")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg := config.Default()
	cfg.Root = root
	cfg.Target.BaseURL = *baseURL
	if *proto != "" {
		cfg.Descriptor.Source = *proto
	}
	cfg.Volatile = []string{"**.created_at", "**.updated_at"}

	cfgPath := filepath.Join(root, config.DirName, config.FileName)
	if _, err := os.Stat(cfgPath); err == nil && !*forceConfig {
		fmt.Printf("keep  %s (already exists)\n", rel(root, cfgPath))
		if *force {
			fmt.Println("      -force refreshes the docs and agent kit only. Your config is yours: it holds " +
				"auth, conventions and volatile paths that no default can reconstruct, and rewriting it " +
				"silently is how a declared convention disappears and a red chain turns green. " +
				"Pass -force-config if you really want it rebuilt from defaults.")
		}
	} else {
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("write %s\n", rel(root, cfgPath))
		fmt.Print("\n" + config.ConventionsGuide + "\n")
	}
	loaded, err := config.Load(root)
	if err != nil {
		return err
	}

	for _, dir := range []string{loaded.Paths.Chains, loaded.Paths.Runs, loaded.Paths.SafeSpots, ".shrt/contracts"} {
		if err := os.MkdirAll(loaded.Abs(dir), 0o755); err != nil {
			return err
		}
	}
	fmt.Printf("write %s/{chains,runs,safespots}/\n", config.DirName)

	example := loaded.Abs(filepath.Join(loaded.Paths.Chains, "example.yaml.template"))
	if _, err := os.Stat(example); err != nil || *force {
		raw, err := agentkit.Read("templates/chain.example.yaml")
		if err != nil {
			return err
		}
		if err := os.WriteFile(example, raw, 0o644); err != nil {
			return err
		}
		fmt.Printf("write %s\n", rel(root, example))
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

	switch added, err := ensureGitignore(root, loaded.NeverCommit()); {
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
	}
	reportEnvelope(loaded)

	fmt.Println("\nnext:")
	fmt.Printf("  read %s/README.md\n", agentkit.DocsDir)
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
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("write %s/%s auth: %s\n", config.DirName, config.FileName, best.Method.FullName)
	for name, p := range cfg.Auth.Profiles {
		fmt.Printf("      profile %-10s %s\n", name, p.Call)
	}
	fmt.Println("      shrt GUESSED this from the descriptor — check it, and check the credential")
	fmt.Println("      variables it names before the first run")
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

const envelopeSuggestionFormat = "conventions:\n    envelope_path: %s\n    envelope_ok: <the value there meaning success>\n"

func EnvelopeSuggestion(path string) string {
	return fmt.Sprintf(envelopeSuggestionFormat, path)
}

func reportEnvelope(cfg *config.Config) {
	if cfg.Conventions.EnvelopePath != "" {
		return
	}
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		return
	}
	found := catalog.DetectEnvelope(cat)
	if len(found) == 0 || found[0].Path == chain.DefaultEnvelopePath {
		return
	}
	best := found[0]
	fmt.Printf("\n%d of your %d response message(s) carry a field at %q; the default is %q.\n",
		best.Count, len(cat.Methods()), best.Path, chain.DefaultEnvelopePath)
	fmt.Printf("shrt GUESSED that from FIELD NAMES alone and cannot tell a verdict from business data\n" +
		"that happens to be named that way, and it cannot guess envelope_ok at all — the success value\n" +
		"is data, not a name. Check it against one response you know was refused. If it is the verdict,\n" +
		"paste this at the TOP LEVEL of .shrt/config.yaml (column 0, not indented):\n\n")
	fmt.Print(EnvelopeSuggestion(best.Path))
	fmt.Printf("\n'shrt doctor' re-checks this every time, so a repo that skipped it says so rather than\n" +
		"running a whole corpus against a path no response carries.\n")
}
