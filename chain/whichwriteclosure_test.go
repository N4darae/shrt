package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestWhichReproducesAWriteInClosureModeAndAReadPinned(t *testing.T) {
	ok := map[string]any{"error": map[string]any{"code": "OK"}}
	chains := []*chain.Chain{{Name: "flow", Steps: []*chain.Step{
		{ID: "create", Call: "pkg.Svc/Create", Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "confirm", Call: "pkg.Svc/Confirm", Body: map[string]any{"id": "${create.id}"}, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch", Call: "pkg.Svc/Fetch", Body: map[string]any{"id": "${create.id}"}, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}}}
	opts := chain.WhichOptions{Observations: func(string) []chain.Observation {
		return []chain.Observation{
			{Run: "r1", Step: "create", Status: "passed", Reached: true, Response: ok},
			{Run: "r1", Step: "confirm", Status: "passed", Reached: true, Response: ok},
			{Run: "r1", Step: "fetch", Status: "passed", Reached: true, Response: ok},
		}
	}}
	write := chain.Which(chains, chain.WhichQuery{RPC: "pkg.Svc/Confirm"}, opts)
	if len(write) != 1 || write[0].Command != "shrt chain slice flow -step confirm -keep writes" {
		t.Fatalf("re-sending a recorded confirm on the entity that run already confirmed reproduces nothing; want closure mode, got %+v", write)
	}
	read := chain.Which(chains, chain.WhichQuery{RPC: "pkg.Svc/Fetch"}, opts)
	if len(read) != 1 || read[0].Command != "shrt chain slice flow -step fetch -mode pin -run r1" {
		t.Fatalf("a read can be pinned to the recorded run, got %+v", read)
	}
}
