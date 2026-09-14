package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func varChain() *chain.Chain {
	c := &chain.Chain{
		Name: "fixtures",
		Vars: map[string]any{"tag": "SEED"},
		Steps: []*chain.Step{{
			ID:   "create",
			Call: "ThingService/Create",
			Body: map[string]any{"code": "T-${vars.tag}", "note": "${vars.reason}"},
			Expect: []chain.Expectation{
				{Path: "error.code", Equals: "OK"},
				{Path: "name", Equals: "${vars.expected_name}"},
			},
		}},
	}
	_ = c.Normalize()
	return c
}

func TestAVarTheChainNeverReadsIsReported(t *testing.T) {
	got := varChain().UnusedVarNames(map[string]any{"taag": "X"})
	if len(got) != 1 || got[0] != "taag" {
		t.Fatalf("UnusedVarNames = %v, want [taag] — a chain isolates its fixtures with vars, so a "+
			"one-character typo silently collapses every run onto the same natural key", got)
	}
}

func TestEveryPlaceAVarCanBeReadCountsAsReading(t *testing.T) {
	c := varChain()
	for _, name := range []string{"tag", "reason", "expected_name"} {
		if got := c.UnusedVarNames(map[string]any{name: "X"}); len(got) != 0 {
			t.Errorf("-var %s reported unused, but the chain reads it; refusing a correct flag is worse "+
				"than the typo this check exists to catch", name)
		}
	}
	declared := strings.Join(c.DeclaredVarNames(), ",")
	if declared != "expected_name,reason,tag" {
		t.Errorf("DeclaredVarNames = %q, want the vars: block plus every ${vars.x} the steps read", declared)
	}
}

func TestNoVarsSuppliedIsNeverAnError(t *testing.T) {
	if got := varChain().UnusedVarNames(nil); got != nil {
		t.Errorf("UnusedVarNames(nil) = %v, want nil", got)
	}
}
