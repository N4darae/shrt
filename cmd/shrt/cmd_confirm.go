package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/N4darae/shrt/store"
)

func init() {
	register(&command{
		name:    "confirm",
		summary: "promote a recorded run to the safe spot for its chain (human only)",
		run:     runConfirm,
	})
}

func runConfirm(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("confirm", flag.ContinueOnError)
	runID := fs.String("run", "latest", "run id to promote, or 'latest'")
	by := fs.String("by", "", "who verified this run — required, and must be a person")
	note := fs.String("note", "", "what makes this run correct")
	ack := fs.Bool("i-verified", false, "acknowledge that a human inspected this run and found it correct")
	supersede := fs.Bool("supersede", false, "replace the existing safe spot, archiving the old one")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: shrt confirm <chain> -by <name> -i-verified")
	}
	e, err := loadEnv(false)
	if err != nil {
		return err
	}
	rec, err := e.store.LoadRun(rest[0], *runID)
	if err != nil {
		return err
	}
	spot, path, err := e.store.Promote(rec, store.Confirmation{
		By: *by, Note: *note, Acknowledged: *ack, Supersede: *supersede, Now: time.Now(),
	})
	if err != nil {
		return err
	}
	fmt.Printf("safe spot for %q set from run %s\n  by:     %s at %s\n  digest: %s\n  file:   %s\n",
		spot.Chain, spot.RunID, spot.ConfirmedBy, spot.ConfirmedAt.Format(time.RFC3339), spot.Digest, path)
	if spot.Supersedes != "" {
		fmt.Printf("  supersedes run %s (archived)\n", spot.Supersedes)
	}
	return nil
}
