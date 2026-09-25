package main

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAFixtureVarInsideAHeaderIsAFixtureName(t *testing.T) {
	c := &chain.Chain{Name: "c", Vars: map[string]any{"tag": "t1"}, Steps: []*chain.Step{{
		ID:      "list",
		Call:    "ThingService/Fetch",
		Headers: map[string]string{"X-Tag": "t-${vars.tag}"},
		Body:    map[string]any{"id": "thing-1"},
	}}}
	if !fixtureRequestPath(c)("list", "headers.X-Tag") {
		t.Fatal("a var inside other text in a header is a fixture name, as it is in a body field")
	}
	if !fixtureOnlyVar(c)("tag") {
		t.Fatal("a var read only inside other text, here in a header, isolates the run")
	}
}
