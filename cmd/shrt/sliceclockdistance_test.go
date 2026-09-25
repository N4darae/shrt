package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestSliceVerdictSaysAClockBoundWasComparedByDistance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: shrt/v1
name: login
steps:
    - id: login
      call: AuthService/Login
      expect:
          - path: status.code
            equals: SUCCESS
          - path: expires_at
            within:
                of: ${nowunix+3600}
                by: 5
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(want, got string) chain.Verdict {
		return chain.Verdict{Step: "login", Status: "failed", Expect: []chain.ExpectResult{
			{Path: "status.code", Rule: "equals", Want: "SUCCESS", Got: "SUCCESS", Passed: true},
			{Path: "expires_at", Rule: "within", Want: want, Got: got},
		}}
	}
	res := &chain.SliceResult{Target: "login", Chain: c}
	for _, tc := range []struct {
		name, sourceGot, sliceGot string
		want                      []string
	}{
		{"same distance", "1790352732", "1790352760", []string{"source 1790352732, slice 1790352760", "(source bound+3s, slice bound+3s)", "the same distance, so they match"}},
		{"timestamps", "1790352729682", "1790352757712", []string{"source 1790352729682, slice 1790352757712",
			"source bound+1.788562376953e+12s, slice bound+1.788562404955e+12s", "matched as timestamps, not by distance"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, replay := verdict("1790352729 ± 5", tc.sourceGot), verdict("1790352757 ± 5", tc.sliceGot)
			same := sameUpToFixtures(nil, nil)
			if diffs := compareSliceVerdicts(res, source, replay, same); len(diffs) != 0 {
				t.Fatalf("the verdicts match: %v", diffs)
			}
			v := &sliceVerdict{Step: "login", Outcome: sliceReproduced, Source: source, Replay: replay, SourceRun: "a", SliceRun: "b",
				ByDistance: clockDistanceLines(res, source, replay, same)}
			out := v.text()
			for _, want := range append(tc.want, "compared by distance from the bound: expires_at within") {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
		})
	}
}
