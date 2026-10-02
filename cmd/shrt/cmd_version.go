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
	setUsage(fs, "usage: shrt version [-short]   which build this is: version, commit, build time and the docs it carries",
		"\nexit codes:\n  0  printed\n  1  a flag that cannot be parsed\n")
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
		fmt.Println(note)
	}
	fmt.Println("shrt doctor checks the docs installed in this repo against this build")
	return nil
}
