package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestARuleWhoseValueCannotFailIsNamedByItsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vacuous.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: shrt/v1
name: vacuous
steps:
    - id: fetch
      call: ThingService/Fetch
      body:
          id: x
      expect:
          - path: error.code
            equals: OK
          - path: name
            contains: ""
          - path: id
            not_empty: false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lint := []string{}
	for _, is := range chain.Lint(c, catalogtest.New()) {
		lint = append(lint, is.Message)
	}
	text := strings.Join(lint, "\n")
	for _, want := range []string{`expect on "name" is contains: ""`, `expect on "id" is not_empty: false`, "can never fail"} {
		if !strings.Contains(text, want) {
			t.Errorf("lint must name the rule and the value that make it unable to fail (%q):\n%s", want, text)
		}
	}
	if strings.Contains(text, "carries no rule") {
		t.Errorf("the rule is there; its value is what makes it vacuous:\n%s", text)
	}
	for i, want := range []string{`contains: ""`, `not_empty: false`} {
		r := c.Steps[0].Expect[i+1].Evaluate(map[string]any{"name": "w", "id": "x"})
		if !strings.Contains(r.Detail, want) || strings.Contains(r.Detail, "has no rule") {
			t.Errorf("the run result must name %s as a value that cannot fail, got %q", want, r.Detail)
		}
	}
}
