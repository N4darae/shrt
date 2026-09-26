package main

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAnUnansweredStepNotRefusedForCredentialsDoesNotBlameThem(t *testing.T) {
	rec := &runner.Record{Chain: "c", Steps: []*runner.StepRecord{{ID: "create", Status: runner.StatusError}}}
	for _, tc := range []struct{ name, why, want string }{
		{"login answered by a gateway", "auth login rejected: http_502: <html><body>502 Bad Gateway</body></html>", "wait until it is up"},
		{"connection reset", "POST http://x/Create: the backend closed the connection before a response arrived (EOF): it most likely stopped", "check the backend is up"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := couldNotVerify("c", "create", tc.why, rec)
			if strings.Contains(err.Error(), "credentials") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("nothing refused the credentials here; want %q and no credentials advice: %v", tc.want, err)
			}
		})
	}
	err := couldNotVerify("c", "create", "auth login rejected: http_401: unauthenticated: bad password", rec)
	if !strings.Contains(err.Error(), "fix the credentials") {
		t.Fatalf("a login refused for its credentials keeps the credentials advice: %v", err)
	}
}
