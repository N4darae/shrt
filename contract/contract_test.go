package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func TestContractStepYAMLIsAValidChainStep(t *testing.T) {
	cat := catalogtest.New()
	m, err := cat.Lookup("ThingService/Create")
	if err != nil {
		t.Fatal(err)
	}
	c := contract.For(m)

	steps := []*chain.Step{}
	if err := yaml.Unmarshal([]byte(c.StepYAML), &steps); err != nil {
		t.Fatalf("generated step is not valid chain YAML: %v\n%s", err, c.StepYAML)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	ch := &chain.Chain{Name: "generated", Steps: steps}
	if err := ch.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	for _, issue := range chain.Lint(ch, cat) {
		t.Errorf("a generated step must lint clean: [%s] %s", issue.Severity, issue.Message)
	}
}

func TestContractStepYAMLKeepsProtoFieldOrder(t *testing.T) {
	cat := catalogtest.New()
	m, _ := cat.Lookup("ThingService/Create")
	yamlText := contract.For(m).StepYAML

	order := []string{"name:", "kind:", "idempotency_key:"}
	at := -1
	for _, key := range order {
		i := strings.Index(yamlText, key)
		if i < 0 {
			t.Fatalf("%s missing from:\n%s", key, yamlText)
		}
		if i < at {
			t.Fatalf("fields are not in proto order:\n%s", yamlText)
		}
		at = i
	}
	if strings.Index(yamlText, "id:") > strings.Index(yamlText, "call:") {
		t.Fatalf("step id should come first:\n%s", yamlText)
	}
}

func TestContractListsExportablePaths(t *testing.T) {
	cat := catalogtest.New()
	m, _ := cat.Lookup("ThingService/Create")
	c := contract.For(m)

	want := map[string]bool{"id": false, "error.code": false}
	for _, e := range c.Exports {
		if _, ok := want[e.Path]; ok {
			want[e.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("export hint %q missing", path)
		}
	}
}
