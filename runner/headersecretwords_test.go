package runner_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestHeaderSecretNamesMatchWholeWordsNotSubstrings(t *testing.T) {
	for _, name := range []string{"Authorization", "X-Auth-Token", "X-Api-Key", "X-Secret", "X-Access-Token", "Cookie", "X-Hmac", "X-Session-Id", "X-Authtoken"} {
		t.Run(name, func(t *testing.T) {
			raw, rec := echoedHeaderRecord(t, name, "${vars.phrase}", map[string]any{"phrase": "correct-horse-battery"})
			if strings.Contains(raw, "correct-horse-battery") {
				t.Fatalf("%s is named like a credential and its value is in the record in clear: %s", name, raw)
			}
			if got := rec.Steps[0].Headers[name]; !runner.HeaderDigested(got) {
				t.Fatalf("%s recorded as %q, want a digest", name, got)
			}
		})
	}
	for _, name := range []string{"X-Author", "X-Authority-Region", "X-Secretary", "X-Tokens-Remaining", "X-Api-Key-Id", "X-Token-Count", "X-Keyword", "X-Pinned-Version"} {
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
