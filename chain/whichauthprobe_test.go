package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestWhichDoesNotKeepWritesForAnAuthProbe(t *testing.T) {
	ok := map[string]any{"error": map[string]any{"code": "OK"}}
	refused := map[string]any{"transport": map[string]any{"code": "unauthenticated", "http_status": 401}}
	chains := []*chain.Chain{{Name: "flow", Steps: []*chain.Step{
		{ID: "create", Call: "pkg.Svc/Create", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "confirm", Call: "pkg.Svc/Confirm", Body: map[string]any{"id": "${create.id}"}, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "cancel_without_token", Call: "pkg.Svc/Cancel", SkipAuth: true, Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}},
		{ID: "cancel_bad_token", Call: "pkg.Svc/Cancel", Auth: chain.InvalidTokenAuth, Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}},
	}}}
	opts := chain.WhichOptions{Observations: func(string) []chain.Observation {
		return []chain.Observation{
			{Run: "r1", Step: "create", Status: "passed", Reached: true, Response: ok},
			{Run: "r1", Step: "confirm", Status: "passed", Reached: true, Response: ok},
			{Run: "r1", Step: "cancel_without_token", Status: "passed", Reached: true, Response: refused},
			{Run: "r1", Step: "cancel_bad_token", Status: "passed", Reached: true, Response: refused},
		}
	}}
	hits := chain.Which(chains, chain.WhichQuery{RPC: "pkg.Svc/Cancel"}, opts)
	if len(hits) != 1 {
		t.Fatalf("want one chain, got %+v", hits)
	}
	if strings.Contains(hits[0].Command, "-keep writes") {
		t.Fatalf("an auth probe is refused before it writes anything, so earlier writes do not change its verdict: %s", hits[0].Command)
	}
	if hits[0].Command != "shrt chain slice flow -step "+hits[0].Best {
		t.Fatalf("want a plain closure slice, got %s", hits[0].Command)
	}
	for _, m := range hits[0].Matches {
		if m.Kind != chain.WhichKindAuthProbe {
			t.Fatalf("step %s is an auth probe: %+v", m.Step, m)
		}
	}
	write := chain.Which(chains, chain.WhichQuery{RPC: "pkg.Svc/Confirm"}, opts)
	if len(write) != 1 || write[0].Command != "shrt chain slice flow -step confirm -keep writes" {
		t.Fatalf("a write that is not an auth probe still keeps writes, got %+v", write)
	}
}
