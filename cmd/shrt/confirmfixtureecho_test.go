package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func echoRecord(t *testing.T, id string, steps ...[3]any) *runner.Record {
	t.Helper()
	rec := &runner.Record{Chain: "echo", RunID: id, Status: runner.StatusPassed, Target: "http://t"}
	for i, s := range steps {
		req, err := json.Marshal(s[1])
		if err != nil {
			t.Fatal(err)
		}
		resp, err := json.Marshal(s[2])
		if err != nil {
			t.Fatal(err)
		}
		rec.Steps = append(rec.Steps, &runner.StepRecord{Index: i + 1, ID: s[0].(string), Call: "ThingService/Create",
			Status: runner.StatusPassed, Request: req, Response: resp})
	}
	return rec
}

func echoChain() *chain.Chain {
	return &chain.Chain{Name: "echo", Vars: map[string]any{"tag": "a"}, Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget ${vars.tag}"}},
		{ID: "refuse", Call: "ThingService/Create", Body: map[string]any{"name": "other"}},
	}}
}

func TestConfirmDoesNotWarnAboutAValueThatOnlyEchoesTheFixtureName(t *testing.T) {
	prev := echoRecord(t, "r1",
		[3]any{"create", map[string]any{"name": "widget first"}, map[string]any{"name": "widget first", "label": "made widget first"}},
		[3]any{"refuse", map[string]any{"name": "other"}, map[string]any{"message": "no stock for 5"}})
	rec := echoRecord(t, "r2",
		[3]any{"create", map[string]any{"name": "widget second"}, map[string]any{"name": "widget second", "label": "made widget second"}},
		[3]any{"refuse", map[string]any{"name": "other"}, map[string]any{"message": "no stock for 7"}})
	got := unstableAgainst(prev, rec, nil, echoChain())
	if strings.Join(got, ",") != "refuse message" {
		t.Fatalf("verify masks a fixture echo, so confirm must not warn about it; only the real difference is unstable, got %v", got)
	}
}
