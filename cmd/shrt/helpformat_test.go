package main

import (
	"strings"
	"testing"
)

func TestTheRemainingHelpsCarryAUsageLineAndExitCodes(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		usage   string
	}{
		{"init", nil, "usage: shrt init [flags]"},
		{"version", nil, "usage: shrt version [-short]"},
		{"catalog", []string{"ls"}, "usage: shrt catalog ls"},
		{"chain", []string{"ls"}, "usage: shrt chain ls"},
		{"contract", []string{"status"}, "usage: shrt contract status"},
	}
	for _, c := range cases {
		out := helpOf(t, c.command, c.args...)
		name := strings.TrimSpace(c.command + " " + strings.Join(c.args, " "))
		if strings.Contains(out, "Usage of ") {
			t.Errorf("shrt %s -h prints Go's bare usage line:\n%s", name, out)
		}
		if !strings.Contains(out, c.usage) || !strings.Contains(out, "exit codes:") {
			t.Errorf("shrt %s -h must start with %q and list its exit codes:\n%s", name, c.usage, out)
		}
	}
}

func TestContractInitWithNoDomainListsThemAndExitsZero(t *testing.T) {
	defer shopStatusWorkspace(t)()
	var err error
	out := captureStdout(t, func() { err = contractInit(nil) })
	if err != nil {
		t.Fatalf("contract init with no domain is documented to list the domains, so it must exit 0, got %v", err)
	}
	if !strings.Contains(out, "domains in this catalog:") || !strings.Contains(out, "orders") {
		t.Fatalf("contract init with no domain must list the domains:\n%s", out)
	}
	if strings.Contains(out, "usage:") {
		t.Fatalf("listing the domains is not a usage error:\n%s", out)
	}
}
