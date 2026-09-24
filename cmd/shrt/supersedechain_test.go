package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLISupersedeProposalListsExpectationEditsAndATargetChange(t *testing.T) {
	first := approvedThingFlow(t)
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw := string(mustRead(t, path))
	edited := strings.Replace(raw, "          - path: name\n            equals: widget\n",
		"          - path: name\n            equals: widget\n          - path: id\n            not_empty: true\n", 1)
	if edited == raw {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
	second := newEchoNameBackend()
	t.Cleanup(second.Close)
	cfg := string(mustRead(t, ".shrt/config.yaml"))
	writeFile(t, ".shrt/config.yaml", strings.Replace(cfg, first.URL, second.URL, 1))
	ctx := context.Background()
	var err error
	out := captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		err = runConfirm(ctx, []string{"cli-thing-flow", "-supersede", "-note", "id is now asserted"})
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := out[strings.Index(out, "Safe spot proposal"):]
	if !strings.Contains(summary, "chain expect") || !strings.Contains(summary, "absent -> id not_empty true") {
		t.Fatalf("the approver signs off on the added expectation, so the proposal lists it:\n%s", summary)
	}
	if !strings.Contains(summary, "target base_url "+first.URL+" -> "+second.URL) {
		t.Fatalf("the approver signs off on the new target, so the proposal lists it:\n%s", summary)
	}
}
