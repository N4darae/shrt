package main

import (
	"regexp"
	"strings"
	"testing"
)

var exitCodeHead = regexp.MustCompile(`^  (\d)  `)

func exitCodeSection(t *testing.T, out string) []string {
	t.Helper()
	_, section, ok := strings.Cut(out, "exit codes:\n")
	if !ok {
		t.Fatalf("no exit codes section:\n%s", out)
	}
	return strings.Split(strings.TrimRight(section, "\n"), "\n")
}

func TestVerifyAndRunHelpListEachExitCodeOnOneLine(t *testing.T) {
	for _, command := range []string{"verify", "run"} {
		out := helpOf(t, command)
		seen := map[string]int{}
		for _, line := range exitCodeSection(t, out) {
			m := exitCodeHead.FindStringSubmatch(line)
			if m == nil {
				t.Errorf("%s -h: exit-code section line is not one code: %q", command, line)
				continue
			}
			seen[m[1]]++
			if len(line) > 110 {
				t.Errorf("%s -h: exit-code line wider than 110 columns: %q", command, line)
			}
		}
		for _, code := range []string{"0", "1", "3"} {
			if seen[code] != 1 {
				t.Errorf("%s -h must list exit %s once:\n%s", command, code, out)
			}
		}
	}
}

func TestEveryHelpFitsInTwoKilobytes(t *testing.T) {
	for _, args := range [][]string{
		{"run"}, {"verify"}, {"init"}, {"confirm"}, {"diff"}, {"doctor"}, {"version"}, {"gate"},
		{"chain", "new"}, {"chain", "ls"}, {"chain", "which"}, {"chain", "lint"}, {"chain", "slice"}, {"chain", "hollow"},
		{"contract", "init"}, {"contract", "lint"}, {"contract", "show"}, {"contract", "plan"}, {"contract", "status"},
		{"contract", "quality"}, {"contract", "report"}, {"catalog", "build"}, {"catalog", "ls"}, {"catalog", "describe"},
	} {
		if commands[args[0]] == nil {
			continue
		}
		if out := helpOf(t, args[0], args[1:]...); len(out) > 2048 {
			t.Errorf("shrt %s -h is %d bytes, want at most 2KB:\n%s", strings.Join(args, " "), len(out), out)
		}
	}
}

func TestContractPlanHelpListsItsExitCodes(t *testing.T) {
	out := helpOf(t, "contract", "plan")
	section := exitCodeSection(t, out)
	lines := strings.Join(section, "\n") + "\n" + strings.Join(strings.Fields(strings.Join(section, " ")), " ")
	for _, want := range []string{"  0  ", "  1  ", "streaming", "dependency cycle", "already exists", "no usable value"} {
		if !strings.Contains(lines, want) {
			t.Errorf("contract plan -h exit codes must mention %q:\n%s", want, out)
		}
	}
}
