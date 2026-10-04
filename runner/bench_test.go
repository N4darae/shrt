package runner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func longChain(n int) *chain.Chain {
	c := &chain.Chain{Name: "long-flow", Volatile: []string{"**.created_at", "**.id"}}
	for i := range n {
		create, fetch := fmt.Sprintf("create_%d", i), fmt.Sprintf("fetch_%d", i)
		c.Steps = append(c.Steps,
			&chain.Step{
				ID: create, Call: "ThingService/Create",
				Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "id", NotEmpty: true}},
			},
			&chain.Step{
				ID: fetch, Call: "ThingService/Fetch", Body: map[string]any{"id": "${" + create + ".id}"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "name", Equals: "widget"}},
			})
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func BenchmarkRunChain(b *testing.B) {
	srv := newFakeServer()
	defer srv.Close()
	r := buildRunner(b, testConfig(srv.URL), catalogtest.New())
	c := longChain(20)
	b.ReportAllocs()
	for b.Loop() {
		rec, err := r.Run(context.Background(), c, runner.Options{})
		if err != nil || !rec.Passed() {
			b.Fatalf("run: %v %s", err, rec.Failure)
		}
	}
}
