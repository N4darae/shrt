package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func TestTheScaffoldNeverInvitesDeletingAnEntry(t *testing.T) {
	cat := catalogtest.New()
	for domain, methods := range contract.Domains(cat.Methods()) {
		out, err := yaml.Marshal(contract.ScaffoldOverlay(domain, methods, nil, cat.Methods()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "or delete the") {
			t.Fatalf("the contract author is told never to delete a fields: entry, and a deleted export "+
				"is scored as an undeclared response field; the scaffold must not suggest deleting:\n%s", out)
		}
	}
}
