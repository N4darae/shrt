package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func TestAFreshlyPlannedChainPassesStrictLintWithNoUnassertedTimestamp(t *testing.T) {
	cat, lib := shopDemo(t)
	for _, targets := range [][]string{
		{"ListOrders", "AddStock", "ConfirmOrder", "CancelOrder"},
		{"CreateProduct", "ListProducts"},
		{"GetProduct"},
		{"AddStock"},
		{"CreateCustomer"},
		{"FetchOrder"},
	} {
		p, err := contract.BuildPlanFor(targets, lib, cat, "strict")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := p.YAML()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "strict.yaml")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := chain.LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		issues := contract.LintChain(c, cat, contract.ChainLintOptions{Strict: true, Library: lib, Chain: chain.LintOptions{Hints: true}})
		bad := []string{}
		for _, i := range issues {
			if i.IsError() || i.Kind == chain.KindUnassertedTimestamp {
				bad = append(bad, "["+i.Step+"] "+i.Message)
			}
		}
		if len(bad) > 0 {
			t.Fatalf("%v: the planned chain fails its own strict lint:\n%s", targets, strings.Join(bad, "\n"))
		}
	}
}
