package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestAuthBodyCheckAgreesWithTheAuthBodyResolver(t *testing.T) {
	cases := []struct {
		ref      string
		resolves bool
	}{
		{"${env.WIDGET_USER}", true},
		{"${uuid}", true},
		{"${now}", true},
		{"${nowunix+3600}", true},
		{"${today-86400}", true},
		{"prefix-${env.WIDGET_USER}", true},
		{"${vars.x}", false},
		{"${exports.token}", false},
		{"${token}", false},
		{"${login.access_token}", false},
		{"${steps.login.response.access_token}", false},
		{"${nowunix+1h}", false},
	}
	for _, c := range cases {
		t.Run(c.ref, func(t *testing.T) {
			body := map[string]any{"username": c.ref}
			sc := chain.AuthBodyScope()
			sc.Env = func(string) (string, bool) { return "set", true }
			_, err := sc.ResolveValue(body)
			if (err == nil) != c.resolves {
				t.Fatalf("the auth body resolver says resolves=%v (%v); the case table is wrong", err == nil, err)
			}
			problems := chain.AuthBodyReferenceProblems(body)
			if c.resolves && len(problems) != 0 {
				t.Fatalf("%s resolves in an auth body, so the static check may not reject it: %v", c.ref, problems)
			}
			if !c.resolves && len(problems) != 1 {
				t.Fatalf("%s dies at run time with 'auth body: unresolved reference', so the static check "+
					"must report it before then: %v", c.ref, problems)
			}
			if !c.resolves && !strings.Contains(problems[0], c.ref[strings.Index(c.ref, "${"):]) {
				t.Errorf("the problem must quote the reference: %q", problems[0])
			}
		})
	}
}
