package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/agentkit"
	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{
		name:    "doctor",
		summary: "check this repo's .shrt/ installation: docs, descriptor, ignores, tokens, auth",
		run:     runDoctor,
	})
}

const doctorHelpTail = "\nchecks:\n" +
	"  build        which shrt build is running\n" +
	"  docs         the installed README, GRAMMAR, PLAYBOOK and PITFALLS against the copy in the binary\n" +
	"  agentkit     the .claude/ agent kit init installed, against the copy in the binary\n" +
	"  descriptor   the descriptor file against a rebuild from descriptor.source\n" +
	"  gitignore    .gitignore against the paths that must never be committed\n" +
	"  tokens       the token cache's file mode, and how many entries serve this target's logins;\n" +
	"               how a cached token is dropped and re-minted is explained only when one is expired,\n" +
	"               from another base_url, or for another login\n" +
	"  auth         the auth profiles and every ${env.*} they read\n" +
	"  conventions  the envelope conventions against the response messages\n" +
	"  contracts    the contract overlays under paths.contracts load, and none sit in a subdirectory\n" +
	"               shrt does not read\n" +
	"  safespots    every safe spot under paths.safespots has its chain under paths.chains\n" +
	"  upgrade      records from an older build or another target: unsealed runs, safe spots with no\n" +
	"               auth_principal, runs, safe spots and cached tokens from another base_url\n" +
	"\nexit codes:\n" +
	"  0  no FAIL (warnings allowed)\n" +
	"  1  a FAIL, or a warning under -strict; also a config that does not load\n"

func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "exit non-zero on warnings too, for a CI job that refuses them")
	asJSON := fs.Bool("json", false, "emit the findings as JSON")
	setUsage(fs, "usage: shrt doctor [flags]   check this repo's .shrt/ installation, one line per finding (OK, WARN or FAIL)", doctorHelpTail)
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	kit := []doctor.KitFile{}
	for _, a := range agentkit.ClaudeAssets() {
		want, err := agentkit.ReadAsset(a)
		if err != nil {
			return err
		}
		kit = append(kit, doctor.KitFile{Path: filepath.ToSlash(a.Dest), Want: want})
	}
	report := doctor.Run(ctx, e.cfg, doctor.Options{
		Docs:      coredistillation.Docs,
		DocNames:  coredistillation.DocNames,
		Kit:       kit,
		TokenKeys: tokenKeys(ctx),
	})
	if *asJSON {
		if err := emitJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Print(report.Text())
		fmt.Printf("\n%s: %s\n", report.Root, report.Summary())
	}
	if !report.Failed(*strict) {
		return nil
	}
	if *strict && report.Count(doctor.LevelError) == 0 {
		fmt.Fprintln(os.Stderr, "doctor: -strict, so the warnings above are failures")
	}
	return exitWith(1, "%s", report.Summary())
}

func tokenKeys(ctx context.Context) func(cfg *config.Config, target string) []string {
	return func(cfg *config.Config, target string) []string {
		cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
		if err != nil || cfg.Auth == nil {
			return nil
		}
		at := *cfg
		at.Target.BaseURL = target
		deps, err := runner.Build(ctx, &at, cat, nil)
		if err != nil {
			return nil
		}
		keys := []string{}
		for _, src := range deps.Sources {
			if k := src.CacheKey(); k != "" {
				keys = append(keys, k)
			}
		}
		return keys
	}
}
