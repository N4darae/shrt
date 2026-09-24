package runner_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAVarReadIntoARedactedFieldIsScrubbedByValueFromTheWholeRecord(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	c := chainWithVars(t, []string{"**.password"}, map[string]any{"pw": "placeholder", "staff_user": "alice"}, "pw")
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{Vars: map[string]any{"pw": "hunter2-x9"}})
	if err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	sent := srv.bodies[0]
	srv.mu.Unlock()
	if sent["password"] != "hunter2-x9" {
		t.Fatalf("scrubbing must not change what is sent, server saw %v", sent["password"])
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2-x9") {
		t.Fatalf("a var whose value lands in a redacted field is a secret, yet the record holds it in clear: %s", raw)
	}
	if got := rec.Vars["staff_user"]; got != "alice" {
		t.Fatalf("a var sent into a field no redact pattern covers stays as it is, got %v", got)
	}
}
