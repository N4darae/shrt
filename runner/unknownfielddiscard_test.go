package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAnUnknownResponseFieldIsDiscardedNotKeptAsSent(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.unknownField = true
	r := newRunner(t, srv)
	r.ValidateOutput = false
	rec, err := r.Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fetch := stepByID(t, rec, "fetch")
	if fetch.Status != runner.StatusPassed {
		t.Fatalf("an added optional field is backward compatible, want passed, got %s: %s", fetch.Status, fetch.Error)
	}
	resp := string(fetch.Response)
	if strings.Contains(resp, "createdAt") || !strings.Contains(resp, "created_at") {
		t.Errorf("the response must be re-encoded with proto names, got %s", resp)
	}
	if strings.Contains(resp, "no_such_field_in_the_proto") {
		t.Errorf("the unknown field must be discarded from the record, got %s", resp)
	}
	if strings.Contains(fetch.Warning, "kept as sent") || !strings.Contains(fetch.Warning, "no_such_field_in_the_proto") {
		t.Errorf("the warning must name the discarded field and not claim the body was kept as sent: %q", fetch.Warning)
	}
}

func TestDriftExpectationsDoNotClaimARefusal(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.unknownField = true
	r := newRunner(t, srv)
	r.ValidateOutput = true
	rec, err := r.Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fetch := stepByID(t, rec, "fetch")
	if !fetch.Drift || fetch.Status != runner.StatusFailed {
		t.Fatalf("validate_output must keep failing as drift, got status %s drift %v", fetch.Status, fetch.Drift)
	}
	for _, e := range fetch.Expect {
		if strings.Contains(e.Detail, "refused") {
			t.Errorf("the call was answered, not refused: %q", e.Detail)
		}
	}
	if strings.Contains(string(fetch.Response), "createdAt") {
		t.Errorf("a body that decodes once the unknown field is dropped is recorded with proto names, got %s", fetch.Response)
	}
}
