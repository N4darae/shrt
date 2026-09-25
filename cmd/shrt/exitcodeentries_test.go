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

func TestVerifyAndRunHelpListEachExitCodeOnceAsShortBullets(t *testing.T) {
	for _, command := range []string{"verify", "run"} {
		out := helpOf(t, command)
		seen := map[string]int{}
		sentence, longest := 0, 0
		for _, line := range exitCodeSection(t, out) {
			if m := exitCodeHead.FindStringSubmatch(line); m != nil {
				seen[m[1]]++
			}
			if len(line) > 100 {
				t.Errorf("%s -h: exit-code line wider than 100 columns: %q", command, line)
			}
			if strings.HasPrefix(strings.TrimSpace(line), "- ") || exitCodeHead.MatchString(line) {
				sentence = 0
				continue
			}
			sentence++
			longest = max(longest, sentence)
		}
		for code, n := range seen {
			if n != 1 {
				t.Errorf("%s -h lists exit %s as %d separate entries, want one:\n%s", command, code, n, out)
			}
		}
		for _, code := range []string{"0", "1", "3"} {
			if seen[code] != 1 {
				t.Errorf("%s -h must list exit %s once:\n%s", command, code, out)
			}
		}
		if longest > 3 {
			t.Errorf("%s -h: an exit-code bullet runs %d continuation lines, want short bullets:\n%s", command, longest, out)
		}
	}
}

func TestVerifyHelpKeepsEveryExitCodeCase(t *testing.T) {
	out := strings.Join(strings.Fields(helpOf(t, "verify")), " ")
	for _, want := range []string{
		"fixture reused", "fixture collision", "re-run with a fresh -var", "validate_output",
		"undeclared enum value", "likely restarted mid-run", "may be an auth regression",
		"${uuid} or a clock value", "a uniqueness conflict on a literal field", "a chain defect",
		"sent but no answer before target.timeout", "502/503/504",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verify -h lost the case %q:\n%s", want, out)
		}
	}
}
