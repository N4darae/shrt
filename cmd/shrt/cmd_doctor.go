package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/agentkit"
	"github.com/N4darae/shrt/doctor"
)

func init() {
	register(&command{
		name:    "doctor",
		summary: "check this repo's .shrt/ installation: docs, descriptor, ignores, tokens, auth",
		run:     runDoctor,
	})
}

func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "exit non-zero on warnings too, for a CI job that refuses them")
	asJSON := fs.Bool("json", false, "emit the findings as JSON")
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
		Docs:     coredistillation.Docs,
		DocNames: coredistillation.DocNames,
		Kit:      kit,
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
