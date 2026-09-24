package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIVerifyCallsAnAddedExpectationAChainChangeNotAnInputChange(t *testing.T) {
	approvedThingFlow(t)
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw := string(mustRead(t, path))
	edited := strings.Replace(raw, "          - path: name\n            equals: widget\n",
		"          - path: name\n            equals: widget\n          - path: id\n            not_empty: true\n", 1)
	if edited == raw {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
	var err error
	out := captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"}) })
	if !strings.Contains(out, "chain differs from the confirmed run at fetch expect") {
		t.Fatalf("the added expectation is listed as a chain difference:\n%s", out)
	}
	if strings.Contains(out, "input changed") {
		t.Fatalf("an expectation edit is a chain change, not an input change:\n%s", out)
	}
	if !strings.Contains(out, "the chain changed since it was confirmed") {
		t.Fatalf("the summary must call it a chain change:\n%s", out)
	}
	if err != nil {
		t.Fatalf("an added expectation that holds is no drift: %v", err)
	}
	cut := strings.LastIndex(edited, "not_empty: true")
	writeFile(t, path, edited[:cut]+"equals: nope"+edited[cut+len("not_empty: true"):])
	out = captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet", "-save=false"}) })
	if err == nil || !strings.HasPrefix(err.Error(), "drift after a chain change") || strings.Contains(out, "input changed") {
		t.Fatalf("a failing added expectation is drift after a chain change, got %v:\n%s", err, out)
	}
}
