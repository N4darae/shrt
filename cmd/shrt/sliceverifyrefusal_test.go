package main

import (
	"testing"
)

func TestCLISliceVerifyRefusedUpFrontExitsDidNotRunNotNotReproduced(t *testing.T) {
	freshTagWorkspace(t)
	for _, args := range [][]string{
		{"-step", "fetch", "-run", "latest", "-verify"},
		{"-step", "fetch", "-verify"},
		{"-step", "fetch", "-run", "no-such-run", "-verify"},
	} {
		_, err := freshTagSlice(t, args...)
		if err == nil {
			t.Fatalf("%v: want a refusal", args)
		}
		if got := exitCodeOf(err); got != 2 {
			t.Errorf("%v: a refusal before anything was sent exits %d, want 2 (did not run), never 1 (NOT REPRODUCED): %v", args, got, err)
		}
	}
	if _, err := freshTagSlice(t, "-step", "nope", "-run", "latest"); exitCodeOf(err) != 1 {
		t.Errorf("without -verify a refusal keeps exit 1, got %d: %v", exitCodeOf(err), err)
	}
}
