package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAPathNamingOneWordOfAFieldSuggestsThatField(t *testing.T) {
	c := &chain.Chain{
		Name: "word-suggest",
		Steps: []*chain.Step{
			{ID: "login", Call: "AuthService/Login", SkipAuth: true,
				Body:   map[string]any{"username": "u", "password": "p"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
				Export: map[string]string{"tok": "token"}},
			{ID: "create", Call: "ThingService/Create", SkipAuth: true,
				Body:   map[string]any{"name": "${login.token}"},
				Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	msgs := lintMessages(t, c)
	for _, want := range []string{`export "tok" reads "token"`, `${login.token} reads "token"`} {
		found := false
		for _, m := range msgs {
			if strings.Contains(m, want) {
				found = true
				if !strings.Contains(m, `did you mean "access_token"?`) {
					t.Errorf("a path naming one word of a field suggests it: %s", m)
				}
			}
		}
		if !found {
			t.Errorf("no lint error containing %q in %v", want, msgs)
		}
	}
}
