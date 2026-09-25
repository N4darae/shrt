package main

import (
	"strings"
	"testing"
)

func TestAnExpectationEditDoesNotTurnAnIntermittentFindingIntoARegression(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	from := "      body: {id: thing-9}\n      expect:\n          - path: error.code\n            equals: OK\n    - id: fetch2"
	to := "      body: {id: thing-9}\n      expect:\n          - path: error.code\n            equals: OK\n          - path: name\n            equals: widget\n    - id: fetch2"
	edited := strings.Replace(flakyChain, from, to, 1)
	if edited == flakyChain {
		t.Fatal("the chain edit did not apply")
	}
	writeFile(t, ".shrt/chains/cli-flaky.yaml", edited)
	f.set("internal", 500, 2)
	out, err := verifyOnce(t, ctx)
	wantExit1(t, "fetch_again failed", err, out)
	if !strings.Contains(err.Error(), "intermittent failure at ThingService/Fetch") || strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("an added expectation at fetch_again cannot explain or cause its internal error, so the verdict is the intermittent finding: %v\n%s", err, out)
	}
	if strings.Contains(out, "explained by the failed changed expectation") {
		t.Fatalf("a transport error is never explained by an expectation edit:\n%s", out)
	}
}
