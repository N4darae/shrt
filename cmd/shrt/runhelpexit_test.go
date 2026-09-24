package main

import (
	"strings"
	"testing"
)

func TestRunHelpListsEveryRefusalTheReadmeLists(t *testing.T) {
	out := strings.Join(strings.Fields(helpOf(t, "run")), " ")
	for _, want := range []string{
		"a response field the producing step's message does not declare",
		"a request path its request does not declare",
		"the login body of an auth profile a step runs under",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run -h must list the refusal %q, as README's exit-code table does:\n%s", want, out)
		}
	}
	readme := strings.Join(strings.Fields(string(mustRead(t, "../../README.md"))), " ")
	for _, want := range []string{
		"or to a response field the producing step's message does not declare",
		"a request path its request does not declare",
		"an unset env var read by a step or by the login body of an auth profile a step runs under",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README's run row no longer says %q; keep run -h and README in step", want)
		}
	}
}
