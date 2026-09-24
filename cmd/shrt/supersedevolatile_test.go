package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestSupersedeNamesTheStepOfAStepLevelVolatile(t *testing.T) {
	rec := &runner.Record{Chain: "c", Volatile: []string{"**.trace"}, Steps: []*runner.StepRecord{
		{ID: "create"},
		{ID: "confirm_too_big", Volatile: []string{"status.message"}},
	}}
	c := &chain.Chain{Name: "c", Volatile: []string{"**.trace"}, Steps: []*chain.Step{
		{ID: "create"}, {ID: "confirm_too_big", Volatile: []string{"status.message"}},
	}}
	for _, current := range []*chain.Chain{c, nil} {
		got := []string{}
		for _, d := range unapprovedVolatileDiffers([]string{"status.message", "**.trace"}, rec, current) {
			got = append(got, d.Step+" "+d.Path)
		}
		if strings.Join(got, ",") != "confirm_too_big status.message,- **.trace" {
			t.Fatalf("a step-level volatile is labelled with its step, a chain-wide one with -, got %v", got)
		}
	}
}
