package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestTheFreshnessRefusalNamesTheValueTheSourceRunUsed(t *testing.T) {
	source := &chain.Chain{Name: "wide", Vars: map[string]any{"tag": "w1"}}
	res := &chain.SliceResult{FreshVars: []string{"tag"}, Chain: &chain.Chain{Vars: map[string]any{"tag": "w1"}}}
	rec := &runner.Record{RunID: "R1", Vars: map[string]any{"tag": "sl1"}}
	err := freshVarsError(res, source, rec, varFlags{})
	if err == nil {
		t.Fatal("a kept write interpolating tag must be refused without a fresh -var")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tag=sl1, the value run R1 used") {
		t.Fatalf("the source run created with tag=sl1, so that is the value that is not fresh: %s", msg)
	}
	if strings.Contains(msg, "tag=w1") {
		t.Fatalf("the chain default w1 is not what the source run created with: %s", msg)
	}
	same := &runner.Record{RunID: "R2", Vars: map[string]any{"tag": "w1"}}
	if msg := freshVarsError(res, source, same, varFlags{}).Error(); !strings.Contains(msg, "tag=w1, the value run R2 used") {
		t.Fatalf("a run that used the default is named as the run's value: %s", msg)
	}
}
