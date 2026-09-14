package main

import (
	"fmt"
	"strings"
)

type subcommand struct {
	name    string
	summary string
}

type group struct {
	name string
	subs []subcommand
}

func (g group) names() string {
	out := make([]string, 0, len(g.subs))
	for _, s := range g.subs {
		out = append(out, s.name)
	}
	return strings.Join(out, "|")
}

func (g group) usage() string {
	return fmt.Sprintf("usage: shrt %s <%s> [flags]", g.name, g.names())
}

func (g group) width() int {
	w := 0
	for _, s := range g.subs {
		if len(s.name) > w {
			w = len(s.name)
		}
	}
	return w
}

func (g group) printHelp() {
	fmt.Println(g.usage())
	fmt.Println()
	w := g.width()
	for _, s := range g.subs {
		fmt.Printf("  %-*s  %s\n", w, s.name, s.summary)
	}
	fmt.Println()
	fmt.Printf("run 'shrt %s <subcommand> -h' for its flags\n", g.name)
}

func (g group) missing() error {
	return exitWith(2, "%s", g.usage())
}

func (g group) unknown(sub string) error {
	return exitWith(2, "unknown %s subcommand %q\n\n%s", g.name, sub, g.usage())
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "-help" || arg == "--help" || arg == "help"
}
