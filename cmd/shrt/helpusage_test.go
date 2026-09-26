package main

import (
	"strings"
	"testing"
)

func TestEveryHelpNamesItsPositionalsAndExitCodes(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    []string
	}{
		{"doctor", nil, []string{"usage: shrt doctor [flags]", "build", "agentkit", "gitignore", "tokens", "conventions", "exit codes", "a FAIL", "-strict"}},
		{"chain", []string{"which"}, []string{"usage: shrt chain which", "exit codes", "nothing matched"}},
		{"chain", []string{"slice"}, []string{"usage: shrt chain slice <chain> -step", "NOT REPRODUCED", "DID NOT RUN", "INCONCLUSIVE"}},
		{"contract", []string{"quality"}, []string{"usage: shrt contract quality", "exit codes", "worse", "baseline file is missing"}},
		{"contract", []string{"lint"}, []string{"usage: shrt contract lint", "exit codes", "contract error"}},
		{"catalog", []string{"describe"}, []string{"usage: shrt catalog describe <rpc>"}},
		{"contract", []string{"show"}, []string{"usage: shrt contract show <rpc>"}},
		{"contract", []string{"plan"}, []string{"usage: shrt contract plan <rpc>"}},
		{"contract", []string{"init"}, []string{"usage: shrt contract init <domain>"}},
		{"chain", []string{"new"}, []string{"usage: shrt chain new -name <chain> <rpc>"}},
	}
	for _, c := range cases {
		out := helpOf(t, c.command, c.args...)
		if strings.Contains(out, "Usage of ") {
			t.Errorf("shrt %s %s -h prints Go's bare usage line:\n%s", c.command, strings.Join(c.args, " "), out)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("shrt %s %s -h lacks %q:\n%s", c.command, strings.Join(c.args, " "), want, out)
			}
		}
	}
}
