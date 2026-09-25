package runner_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestAHeaderNamedLikeASecretIsDigested(t *testing.T) {
	for _, name := range []string{"X-Passphrase", "X-Passcode", "X-Pass-Phrase", "X-Pwd", "X-Private-Key", "X-Client-Secret", "X-Api-Key", "X-Credential"} {
		t.Run(name, func(t *testing.T) {
			raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "correct-horse-battery"})
			if strings.Contains(raw, "correct-horse-battery") {
				t.Fatalf("%s is named like a credential and its value is in the record in clear: %s", name, raw)
			}
			got := rec.Steps[0].Headers[name]
			if !runner.HeaderDigested(got) {
				t.Fatalf("%s recorded as %q, want a digest as X-Credential and Proxy-Authorization get", name, got)
			}
		})
	}
	for _, name := range []string{"X-Tag", "X-Request-Id", "X-Passenger-Count", "X-Compass"} {
		t.Run(name, func(t *testing.T) {
			_, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "window-seat"})
			if got := rec.Steps[0].Headers[name]; got != "window-seat" {
				t.Fatalf("%s is not named like a credential, recorded as %q, want it in clear", name, got)
			}
		})
	}
}
