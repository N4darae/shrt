package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAVarReadIntoACredentialHeaderIsScrubbedFromVars(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		step     int
	}{
		{"whole value", "${vars.sig}", 0},
		{"part of the value", "Sig ${vars.sig}", 0},
		{"step never reached", "${vars.sig}", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer()
			defer srv.Close()
			srv.createRefusal = "denied"
			c := testChain()
			c.Vars = map[string]any{"sig": "sig-value-1234567", "tag": "h1"}
			c.Steps[tc.step].Headers = map[string]string{"X-Passwd": tc.template}
			if err := c.Normalize(); err != nil {
				t.Fatal(err)
			}
			rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "sig-value-1234567") {
				t.Fatalf("a var sent in a credential header is a secret, yet the record holds it in clear: %s", raw)
			}
			if rec.Vars["tag"] != "h1" {
				t.Fatalf("a var no credential header reads stays as it is, got %v", rec.Vars["tag"])
			}
		})
	}
}
