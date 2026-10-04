package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/doctor"
)

func captureStdout(t *testing.T, fn func()) string {
	out, _ := capturing(&os.Stdout, func() error { fn(); return nil })
	return out
}

func captureStderr(t *testing.T, fn func()) string {
	out, _ := capturing(&os.Stderr, func() error { fn(); return nil })
	return out
}

func helpOf(t *testing.T, command string, args ...string) string {
	t.Helper()
	return captureStderr(t, func() {
		_ = commands[command].run(context.Background(), append(args, "-h"))
	})
}

var cliGroups = []group{catalogGroup, chainGroup, contractGroup}

func TestEveryCommandAnswersHelpAndEveryGroupListsItsSubcommands(t *testing.T) {
	for _, name := range sortedKeys(commands) {
		for _, arg := range []string{"-h", "--help"} {
			if err := commands[name].run(context.Background(), []string{arg}); err != nil && !errors.Is(err, flag.ErrHelp) {
				t.Errorf("shrt %s %s: %v", name, arg, err)
			}
		}
	}
	readme := string(mustRead(t, "../../README.md"))
	for name := range commands {
		if !strings.Contains(readme, "shrt "+name) {
			t.Errorf("README never mentions `shrt %s`", name)
		}
	}
	for _, g := range cliGroups {
		if err := commands[g.name].run(context.Background(), []string{"help"}); err != nil && !errors.Is(err, flag.ErrHelp) {
			t.Errorf("shrt %s help: %v", g.name, err)
		}
		out := captureStdout(t, g.printHelp)
		if !strings.Contains(out, g.usage()) {
			t.Errorf("shrt %s -h does not print its usage line", g.name)
		}
		for _, s := range g.subs {
			if s.summary == "" || !strings.Contains(out, s.name) || !strings.Contains(out, s.summary) {
				t.Errorf("shrt %s -h must list %q with its summary", g.name, s.name)
			}
			if !strings.Contains(readme, "shrt "+g.name+" "+s.name) {
				t.Errorf("README never mentions `shrt %s %s`", g.name, s.name)
			}
		}
		for _, err := range []error{g.missing(), g.unknown("frobnicate")} {
			var coded *exitError
			if !errors.As(err, &coded) || coded.code != 2 || !strings.Contains(err.Error(), g.names()) {
				t.Errorf("shrt %s misuse exits 2 naming the subcommands, got %v", g.name, err)
			}
		}
	}
}

func TestHelpAnywhereInTheArgumentsAsksForHelp(t *testing.T) {
	for _, c := range []struct {
		name       string
		args, want []string
	}{
		{"contract", []string{"plan", "X", "-h"}, []string{"plan", "-h"}},
		{"chain", []string{"slice", "c", "-step", "s", "--help"}, []string{"slice", "-h"}},
		{"init", []string{"foo", "-help"}, []string{"-h"}},
		{"run", []string{"c", "-var", "a=b", "-h"}, []string{"-h"}},
		{"run", []string{"c", "-quiet"}, []string{"c", "-quiet"}},
		{"run", []string{"c", "--", "-h"}, []string{"c", "--", "-h"}},
	} {
		if got := helpArgs(c.name, c.args); !slices.Equal(got, c.want) {
			t.Errorf("%s %v: got %v, want %v", c.name, c.args, got, c.want)
		}
	}
}

func TestEveryHelpCarriesItsUsageAndExitCodesInTwoKilobytes(t *testing.T) {
	exits := []string{"exit codes"}
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"run"}, append(exits, "usage: shrt run <chain>", "no verdict")},
		{[]string{"verify"}, append(exits, "usage: shrt verify <chain>", "drift")},
		{[]string{"init"}, append(exits, "usage: shrt init [flags]", "example.yaml.template")},
		{[]string{"confirm"}, []string{"usage: shrt confirm <chain>", "-approve -by"}},
		{[]string{"diff"}, append(exits, "usage: shrt diff <run-a> <run-b>", "shrt diff <chain>")},
		{[]string{"doctor"}, append(exits, "usage: shrt doctor [flags]", "a FAIL", "-strict")},
		{[]string{"version"}, append(exits, "usage: shrt version [-short]")},
		{[]string{"gate"}, nil},
		{[]string{"chain", "new"}, []string{"usage: shrt chain new -name <chain> <rpc>"}},
		{[]string{"chain", "ls"}, append(exits, "usage: shrt chain ls")},
		{[]string{"chain", "which"}, append(exits, "usage: shrt chain which", "nothing matched")},
		{[]string{"chain", "lint"}, []string{"usage: shrt chain lint [<chain>...]"}},
		{[]string{"chain", "slice"}, []string{"usage: shrt chain slice <chain> -step", "NOT REPRODUCED", "DID NOT RUN", "INCONCLUSIVE"}},
		{[]string{"chain", "hollow"}, append(exits, "usage: shrt chain hollow", "no run records")},
		{[]string{"contract", "init"}, []string{"usage: shrt contract init <domain>"}},
		{[]string{"contract", "lint"}, append(exits, "usage: shrt contract lint", "contract error")},
		{[]string{"contract", "show"}, []string{"usage: shrt contract show <rpc>"}},
		{[]string{"contract", "plan"}, []string{"usage: shrt contract plan <rpc>"}},
		{[]string{"contract", "status"}, append(exits, "usage: shrt contract status")},
		{[]string{"contract", "quality"}, append(exits, "usage: shrt contract quality", "worse", "baseline file is missing", "lower is better")},
		{[]string{"contract", "report"}, nil},
		{[]string{"catalog", "build"}, []string{"usage: shrt catalog build"}},
		{[]string{"catalog", "ls"}, append(exits, "usage: shrt catalog ls")},
		{[]string{"catalog", "describe"}, []string{"usage: shrt catalog describe <rpc>"}},
	} {
		if commands[c.args[0]] == nil {
			continue
		}
		name := strings.Join(c.args, " ")
		out := helpOf(t, c.args[0], c.args[1:]...)
		if len(out) > 2048 || strings.Contains(out, "Usage of ") {
			t.Errorf("shrt %s -h is %d bytes (at most 2KB) or prints Go's bare usage:\n%s", name, len(out), out)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("shrt %s -h lacks %q:\n%s", name, want, out)
			}
		}
	}
	head := regexp.MustCompile(`^  (\d)  `)
	for _, command := range []string{"verify", "run"} {
		out := helpOf(t, command)
		_, section, _ := strings.Cut(out, "exit codes:\n")
		seen := map[string]int{}
		for _, line := range strings.Split(strings.TrimRight(section, "\n"), "\n") {
			m := head.FindStringSubmatch(line)
			if m == nil || len(line) > 110 {
				t.Errorf("%s -h: an exit-code line is one code within 110 columns: %q", command, line)
				continue
			}
			seen[m[1]]++
		}
		if seen["0"] != 1 || seen["1"] != 1 || seen["3"] != 1 {
			t.Errorf("%s -h must list exits 0, 1 and 3 once each:\n%s", command, out)
		}
	}
	_, plan, _ := strings.Cut(helpOf(t, "contract", "plan"), "exit codes:\n")
	plan = plan + strings.Join(strings.Fields(plan), " ")
	for _, want := range []string{"  0  ", "  1  ", "streaming", "dependency cycle", "already exists", "no usable value"} {
		if !strings.Contains(plan, want) {
			t.Errorf("contract plan -h exit codes must mention %q:\n%s", want, plan)
		}
	}
	for _, name := range []string{doctor.CheckBuild, doctor.CheckDocs, doctor.CheckKit, doctor.CheckDescriptor, doctor.CheckIgnored,
		doctor.CheckTokens, doctor.CheckAuth, doctor.CheckConventions, doctor.CheckContracts, doctor.CheckSafeSpots, doctor.CheckUpgrade} {
		if !strings.Contains(doctorHelpTail, "\n  "+name+" ") {
			t.Errorf("doctor -h does not list the %s check", name)
		}
	}
	strict := []string{chain.KindUnfailable, chain.KindAssertsNone, chain.KindInertAllowFail, chain.KindExportOverwritten, chain.KindArithmetic, chain.KindEnvelopeOnly}
	lint := strings.Join(strings.Fields(helpOf(t, "chain", "lint")), " ")
	readme := strings.Join(strings.Fields(string(mustRead(t, "../../README.md"))), " ")
	_, row, _ := strings.Cut(readme, "| `shrt chain lint [<c>]` |")
	row, _, _ = strings.Cut(row, "| `shrt chain ls` |")
	for _, kind := range strict {
		if !chain.IsAssertionQualityIssue(chain.Issue{Kind: kind}) || !strings.Contains(lint, kind) || !strings.Contains(row, kind) {
			t.Errorf("%s is promoted by -strict and named by chain lint -h and README's chain lint row", kind)
		}
	}
}
