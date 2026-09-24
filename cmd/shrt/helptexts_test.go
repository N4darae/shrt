package main

import (
	"context"
	"strings"
	"testing"
)

func helpOf(t *testing.T, command string, args ...string) string {
	t.Helper()
	return captureStderr(t, func() {
		_ = commands[command].run(context.Background(), append(args, "-h"))
	})
}

func TestHelpTextsCarryUsageAndTheDocumentedExitCodes(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    []string
	}{
		{"verify", nil, []string{"usage: shrt verify <chain>", "exit codes", "drift"}},
		{"confirm", nil, []string{"usage: shrt confirm <chain>", "-approve -by"}},
		{"diff", nil, []string{"usage: shrt diff <run-a> <run-b>", "shrt diff <chain>", "exit codes"}},
		{"chain", []string{"lint"}, []string{"usage: shrt chain lint [<chain>...]"}},
		{"catalog", []string{"build"}, []string{"usage: shrt catalog build"}},
		{"chain", []string{"hollow"}, []string{"usage: shrt chain hollow", "exit codes", "no run records"}},
		{"run", nil, []string{"unknown auth profile"}},
		{"init", nil, []string{"example.yaml.template"}},
		{"contract", []string{"quality"}, []string{"lower is better"}},
	}
	for _, c := range cases {
		out := helpOf(t, c.command, c.args...)
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("shrt %s %s -h lacks %q:\n%s", c.command, strings.Join(c.args, " "), want, out)
			}
		}
	}
}
