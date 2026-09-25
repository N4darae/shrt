package main

import (
	"strings"
	"testing"
)

func TestRunHelpListsEveryUpFrontRefusal(t *testing.T) {
	out := strings.Join(strings.Fields(helpOf(t, "run")), " ")
	for _, want := range []string{
		"a response field the producing step's message does not declare",
		"a request path its request does not declare",
		"a reference whose declared type cannot fill the numeric field it is sent in",
		"the login body of an auth profile a step runs under",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("run -h must list the refusal %q:\n%s", want, out)
		}
	}
}
