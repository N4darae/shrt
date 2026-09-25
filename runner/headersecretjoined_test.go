package runner_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestJoinedAndSecondFactorHeaderNamesAreCredentials(t *testing.T) {
	for _, name := range []string{
		"X-Apitoken", "X-Apisecret", "X-Csrftoken", "X-Clientsecret", "X-Sessiontoken", "X-Refreshtoken",
		"X-Mfa-Code", "X-Totp", "X-2fa-Code", "X-Oauth", "X-Authz", "X-Passw0rd", "X-Recovery-Code",
		"X-Magic-Link", "X-Signed-Url", "X-Webhooksecret", "X-Xsrf-Header",
	} {
		t.Run(name, func(t *testing.T) {
			raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "correct-horse-battery"})
			if strings.Contains(raw, "correct-horse-battery") {
				t.Fatalf("%s is named like a credential and its value is in the record in clear: %s", name, raw)
			}
			if got := rec.Steps[0].Headers[name]; !runner.HeaderDigested(got) {
				t.Fatalf("%s recorded as %q, want a digest", name, got)
			}
			if got := rec.Vars["phrase"]; got == "correct-horse-battery" {
				t.Fatalf("the var %s is sent in reads is a secret, yet vars holds it in clear", name)
			}
		})
	}
	for _, name := range []string{"X-Secret-Id", "X-Api-Key-Id", "X-Author", "X-Secretary", "X-Pass-Through", "X-Session-Region", "X-Keyboard", "X-Monkey", "X-Signedness-Mode"} {
		t.Run(name, func(t *testing.T) {
			raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "window-seat"})
			if got := rec.Steps[0].Headers[name]; got != "window-seat" {
				t.Fatalf("%s is not named like a credential, recorded as %q, want it in clear", name, got)
			}
			if strings.Contains(raw, "<redacted>") {
				t.Fatalf("%s is not a credential, so its value is not scrubbed from the record: %s", name, raw)
			}
		})
	}
}
