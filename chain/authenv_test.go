package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestLintWarnsAboutAnUnsetEnvVarTheLoginOfAUsedProfileReads(t *testing.T) {
	c := &chain.Chain{Name: "clerk-flow", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Auth: "clerk", Body: map[string]any{"name": "a", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	opts := chain.LintOptions{
		AuthProfiles: []string{"default", "clerk"},
		AuthHeader: func(s *chain.Step) (string, string, bool) {
			if s.Auth != "" {
				return s.Auth, "Authorization", true
			}
			return "default", "Authorization", true
		},
		AuthEnv: func(profile string) []string {
			return map[string][]string{"default": {"API_PASSWORD", "API_USER"}, "clerk": {"CLERK_PASSWORD", "CLERK_USER"}}[profile]
		},
		Env: func(name string) (string, bool) {
			if name == "CLERK_USER" {
				return "clerk", true
			}
			return "", false
		},
	}
	issues := chain.LintWith(c, catalogtest.New(), opts)
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "CLERK_PASSWORD") {
			found = true
			if i.Severity != chain.SeverityWarn || !strings.Contains(i.Message, `"clerk"`) {
				t.Fatalf("want a warning naming the profile, got %+v", i)
			}
		}
		if strings.Contains(i.Message, "API_PASSWORD") || strings.Contains(i.Message, "CLERK_USER") {
			t.Fatalf("only the unset variables of profiles this chain uses matter, got %+v", i)
		}
	}
	if !found {
		t.Fatalf("the clerk login reads CLERK_PASSWORD, which is unset, so the run is refused; lint must say so, got %+v", issues)
	}
}
