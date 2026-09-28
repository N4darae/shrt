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
		{[]string{"-step", "fetchh", "-run", "latest", "-verify"}, 1},
		{[]string{"-step", "fetch", "-verify"}, 1},
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
		{[]string{"no-such-chain", "-step", "fetch", "-run", "latest", "-verify"}, 1},
	} {
		var err error
		captureStdout(t, func() { err = chainSlice(context.Background(), tc.args) })
		if got := exitCodeOf(err); got != tc.want {
			t.Errorf("%v: exit %d, want %d: %v", tc.args, got, tc.want, err)
		}
	}

	if !strings.Contains(sliceExitCodes, "  3  ") || !strings.Contains(sliceExitCodes, "DID NOT RUN") || strings.Contains(sliceExitCodes, "  2  ") {
		t.Errorf("chain slice -h must state the 0/1/3 exit codes:\n%s", sliceExitCodes)
	}
	raw, err := coredistillation.Docs.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "| `chain slice` |") {
		t.Error("README's exit code table must have a row for chain slice")
	}
}
