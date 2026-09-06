package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

func exitWith(code int, format string, args ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string) error
}

var commands = map[string]*command{}

func register(c *command) { commands[c.name] = c }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	if isHelpArg(args[0]) {
		usage()
		return
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		os.Exit(2)
	}
	if err := cmd.run(ctx, args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "shrt %s: %v\n", cmd.name, err)
		var coded *exitError
		if errors.As(err, &coded) {
			os.Exit(coded.code)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("shrt — record, replay and verify internal API call chains")
	fmt.Println()
	fmt.Println("usage: shrt <command> [flags]")
	fmt.Println()
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("  %-10s %s\n", n, commands[n].summary)
	}
	fmt.Println()
	fmt.Println("run 'shrt <command> -h' for command flags")
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
