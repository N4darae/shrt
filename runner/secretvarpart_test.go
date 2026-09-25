package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func partSecretChain(t *testing.T, username, password string) *chain.Chain {
	t.Helper()
	c := &chain.Chain{
		Name:   "login-part-secret",
		Redact: []string{"**.password"},
		Vars:   map[string]any{"tag": "fixture-tag-q7", "pw": "hunter2-x9"},
		Steps: []*chain.Step{{
			ID: "login", Call: "AuthService/Login", SkipAuth: true,
			Body: map[string]any{"username": username, "password": password},
		}},
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func runPartSecret(t *testing.T, c *chain.Chain) (*runner.Record, string) {
	t.Helper()
	srv := newFakeServer()
	defer srv.Close()
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return rec, string(raw)
}

func TestAFixtureVarInsideASecretFieldStaysVisibleWhereItIsNotSecret(t *testing.T) {
	rec, raw := runPartSecret(t, partSecretChain(t, "u-${vars.tag}", "wrong-${vars.tag}"))
	if strings.Contains(raw, "wrong-fixture-tag-q7") {
		t.Fatalf("the whole secret field value must stay redacted: %s", raw)
	}
	if got := rec.Vars["tag"]; got != "fixture-tag-q7" {
		t.Fatalf("a fixture tag also sent in the clear is not a secret, rec.Vars[tag] = %v", got)
	}
	if !strings.Contains(raw, "u-fixture-tag-q7") {
		t.Fatalf("the username built from the tag must not be scrubbed: %s", raw)
	}
}

func TestAVarUsedOnlyInSecretFieldsOrThatIsTheSecretStaysRedacted(t *testing.T) {
	rec, raw := runPartSecret(t, partSecretChain(t, "alice", "wrong-${vars.tag}"))
	if got := rec.Vars["tag"]; got != pathmask.MaskRedacted || strings.Contains(raw, "fixture-tag-q7") {
		t.Fatalf("a var used only inside a secret field is part of the secret, rec.Vars[tag] = %v: %s", got, raw)
	}
	rec, raw = runPartSecret(t, partSecretChain(t, "u-${vars.pw}", "${vars.pw}"))
	if got := rec.Vars["pw"]; got != pathmask.MaskRedacted || strings.Contains(raw, "hunter2-x9") {
		t.Fatalf("a var that is the whole password is a secret everywhere, rec.Vars[pw] = %v: %s", got, raw)
	}
	rec, raw = runPartSecret(t, partSecretChain(t, "u-${vars.pw}", "x-${vars.pw}"))
	if got := rec.Vars["pw"]; got != pathmask.MaskRedacted || strings.Contains(raw, "hunter2-x9") {
		t.Fatalf("a var named like a secret stays one, rec.Vars[pw] = %v: %s", got, raw)
	}
}
