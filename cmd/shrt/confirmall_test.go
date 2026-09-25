package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestConfirmAllProposesPassingRunsAndApprovesThemAfterAYes(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	path := ".shrt/chains/cli-thing-flow.yaml"
	original := strings.Replace(string(mustRead(t, path)), "      call: ThingService/Fetch\n", "      call: ThingService/Fetch\n      skip_auth: true\n", 1)
	writeFile(t, path, original)
	writeFile(t, ".shrt/chains/cli-thing-again.yaml", strings.Replace(original, "name: cli-thing-flow", "name: cli-thing-again", 1))
	writeFile(t, ".shrt/chains/cli-thing-unrun.yaml", strings.Replace(original, "name: cli-thing-flow", "name: cli-thing-unrun", 1))
	captureStdout(t, func() {
		for _, c := range []string{"cli-thing-flow", "cli-thing-again"} {
			if err := runRun(ctx, []string{c, "-quiet"}); err != nil {
				t.Fatalf("shrt run %s: %v", c, err)
			}
		}
	})

	var err error
	out := captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-note", "fetch returns the created name"}) })
	if err != nil {
		t.Fatalf("confirm -all: %v\n%s", err, out)
	}
	for _, want := range []string{"proposed cli-thing-flow: run ", "proposed cli-thing-again: run ", "skip     cli-thing-unrun: no run recorded",
		"**Safe spot proposal: `cli-thing-flow`**", "only after the user says yes to every one:  shrt confirm -all -approve -by <their email>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm -all output lacks %q:\n%s", want, out)
		}
	}
	if _, serr := os.Stat(".shrt/safespots/cli-thing-flow.json"); serr == nil {
		t.Fatal("proposing writes no safe spot")
	}

	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-approve"}) })
	if err == nil {
		t.Fatalf("-all -approve without -by records no approver, so it is refused:\n%s", out)
	}
	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-approve", "-by", "alice@example.test"}) })
	if err != nil || strings.Count(out, "\n") != 2 || !strings.Contains(out, "approved cli-thing-again: safe spot from run ") {
		t.Fatalf("-all -approve approves each pending proposal in one line: %v\n%s", err, out)
	}
	for _, c := range []string{"cli-thing-flow", "cli-thing-again"} {
		if _, serr := os.Stat(".shrt/safespots/" + c + ".json"); serr != nil {
			t.Fatalf("approved %s but wrote no safe spot: %v", c, serr)
		}
	}

	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-note", "again"}) })
	if err != nil || !strings.Contains(out, "skip     cli-thing-flow: safe spot unchanged") || !strings.Contains(out, "nothing proposed") {
		t.Fatalf("a chain whose latest run is its safe spot is not proposed again: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "cli-thing-flow", "-note", "x"}) })
	if err == nil {
		t.Fatal("-all names no chain")
	}
}
