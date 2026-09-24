package contract

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

const readsATag = `apiVersion: shrt/contract/v1
domain: demo
rpcs:
    shrt.test.v1.ThingService/Create:
        summary: creates a thing
        required: [name]
        fields:
            name:
                value: w-${vars.tag}
            idempotency_key:
                value: ${vars.key}
        status: draft
`

func TestPlanDeclaresAnInterpolatedVarItReads(t *testing.T) {
	p, err := BuildPlan("shrt.test.v1.ThingService/Create", libraryFrom(t, readsATag), catalogtest.New(), "demo-create")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if got := p.Chain.Vars["tag"]; got != "demo-create" {
		t.Fatalf("plan must declare the interpolated var it reads so lint does not warn on every planned "+
			"chain; want tag: demo-create, got vars %v", p.Chain.Vars)
	}
	if _, declared := p.Chain.Vars["key"]; declared {
		t.Fatalf("a var that is a field's whole value has no safe default and must stay undeclared, so run "+
			"refuses without -var key=...; got vars %v", p.Chain.Vars)
	}
	missing, _ := chain.ExternalInputs(p.Chain)
	if strings.Join(missing, ",") != "key" {
		t.Fatalf("only key should still need a -var, got %v", missing)
	}
	for _, n := range p.Notes {
		if strings.Contains(n, "${vars.tag}") && !strings.Contains(n, "-var tag=") {
			t.Errorf("the note about the declared tag must still say a re-run needs a fresh -var tag: %q", n)
		}
	}
}
