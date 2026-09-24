package main

import (
	"context"
	"strings"
	"testing"

	coredistillation "github.com/N4darae/shrt"
)

func TestCLISliceRefusalExitCodesAreStatedForBothModes(t *testing.T) {
	freshTagWorkspace(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"-step", "fetchh", "-run", "latest"}, 1},
		{[]string{"-step", "fetchh", "-run", "latest", "-verify"}, 2},
	} {
		_, err := freshTagSlice(t, tc.args...)
		if got := exitCodeOf(err); got != tc.want {
			t.Errorf("%v: exit %d, want %d: %v", tc.args, got, tc.want, err)
		}
	}
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"no-such-chain", "-step", "fetch"}, 1},
		{[]string{"no-such-chain", "-step", "fetch", "-run", "latest", "-verify"}, 2},
	} {
		var err error
		captureStdout(t, func() { err = chainSlice(context.Background(), tc.args) })
		if got := exitCodeOf(err); got != tc.want {
			t.Errorf("%v: exit %d, want %d: %v", tc.args, got, tc.want, err)
		}
	}

	if !strings.Contains(sliceExitCodes, "plain slice") || !strings.Contains(sliceExitCodes, "the same refusal exits 2 under -verify") {
		t.Errorf("chain slice -h must state the plain-slice exit codes and how they differ under -verify:\n%s", sliceExitCodes)
	}
	raw, err := coredistillation.Docs.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "| `chain slice` (no `-verify`) |") {
		t.Error("README's exit code table must have a row for chain slice without -verify")
	}
}
