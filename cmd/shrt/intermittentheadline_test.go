package main

import (
	"strings"
	"testing"
)

func TestRunHeadlineLeadsWithADeterministicFailureBesideAnIntermittentOne(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	from := "      body: {id: thing-11}\n      expect:\n          - path: error.code\n            equals: OK\n"
	to := from + "          - path: name\n            equals: gadget\n"
	edited := strings.Replace(flakyChain, from, to, 1)
	if edited == flakyChain {
		t.Fatal("the chain edit did not apply")
	}
	writeFile(t, ".shrt/chains/cli-flaky.yaml", edited)
	f.set("internal", 500, 2)
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-flaky", "-quiet", "-keep-going"}) })
	wantExit1(t, "run", err, out)
	msg := err.Error()
	if !strings.Contains(out, "FINDING: intermittent failure at Fetch") {
		t.Fatalf("fetch_again's internal error is still reported as intermittent:\n%s", out)
	}
	det := strings.Index(msg, "fetch3")
	flaky := strings.Index(msg, "intermittent")
	if det < 0 || (flaky >= 0 && flaky < det) {
		t.Fatalf("fetch3 fails on every run, so the headline leads with it and the intermittent finding comes after: %v\n%s", err, out)
	}
}

func TestVerifyHeadlineCallsAnIntermittentFirstChangeIntermittentBesideARegression(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("internal", 500, 2, 3)
	f.name11 = "gadget"
	out, err := verifyOnce(t, ctx)
	wantExit1(t, "verify", err, out)
	head, _, _ := strings.Cut(out, "\n")
	if !strings.Contains(out, "FINDING: intermittent failure at Fetch") ||
		!strings.Contains(head, "DRIFT (intermittent; also regression at 1 step)") || !strings.Contains(head, "first: fetch_again") {
		t.Fatalf("fetch_again failed intermittently and fetch3 changed, so the headline calls the first change intermittent:\n%s", out)
	}
}
