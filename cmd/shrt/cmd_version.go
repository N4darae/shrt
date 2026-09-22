package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/doctor"
)

func init() {
	register(&command{
		name:    "version",
		summary: "which build this is: version, commit, and the docs it carries",
		run:     runVersion,
	})
}

func runVersion(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	short := fs.Bool("short", false, "print the version alone, for a script")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	b := doctor.ReadBuild()
	if *short {
		fmt.Println(b.Version)
		return nil
	}
	fmt.Printf("shrt %s\n", b.Version)
	if rev := b.ShortRevision(); rev != "" {
		dirty := ""
		if b.Modified {
			dirty = "  (built from a tree with uncommitted changes)"
		}
		fmt.Printf("  commit  %s%s\n", rev, dirty)
	}
	if b.Time != "" {
		fmt.Printf("  built   %s\n", b.Time)
	}
	if b.Go != "" {
		fmt.Printf("  go      %s\n", b.Go)
	}
	fmt.Printf("  docs    %s\n", strings.Join(coredistillation.DocNames, ", "))
	if note := b.Provenance(); note != "" {
		fmt.Printf("\n%s\n", note)
	}
	fmt.Println("\nA stale binary lints with the OLD rules and reports that everything is fine.")
	fmt.Println("'shrt doctor' compares the docs above against the copy installed in this repo.")
	return nil
}
