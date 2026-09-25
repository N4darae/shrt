package main

import (
	"slices"
	"testing"
)

func TestHelpAnywhereInTheArgumentsAsksForHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"contract", []string{"plan", "X", "-h"}, []string{"plan", "-h"}},
		{"chain", []string{"slice", "c", "-step", "s", "--help"}, []string{"slice", "-h"}},
		{"init", []string{"foo", "-help"}, []string{"-h"}},
		{"run", []string{"c", "-var", "a=b", "-h"}, []string{"-h"}},
		{"run", []string{"c", "-quiet"}, []string{"c", "-quiet"}},
		{"run", []string{"c", "--", "-h"}, []string{"c", "--", "-h"}},
	}
	for _, c := range cases {
		if got := helpArgs(c.name, c.args); !slices.Equal(got, c.want) {
			t.Errorf("%s %v: got %v, want %v", c.name, c.args, got, c.want)
		}
	}
}
