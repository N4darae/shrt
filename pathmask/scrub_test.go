package pathmask_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestScrubReplacesKnownSecretsByValue(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	m.AddSecret("s3cret-admin")
	m.AddSecret("admin")
	m.AddSecret("ab")
	cases := map[string]string{
		"s3cret-admin x":  "<redacted> x",
		"the admin said":  "the <redacted> said",
		"ab":              "<redacted>",
		"cab":             "cab",
		"<redacted> left": "<redacted> left",
	}
	for in, want := range cases {
		if got := m.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
	raw := m.ScrubJSON(json.RawMessage(`{"n":"12345678901234567890","name":"s3cret-admin"}`))
	if string(raw) != `{"n":"12345678901234567890","name":"<redacted>"}` {
		t.Fatalf("ScrubJSON should replace the secret and keep everything else as it was, got %s", raw)
	}
	untouched := json.RawMessage(`{"b": 1.50}`)
	if got := m.ScrubJSON(untouched); string(got) != string(untouched) {
		t.Fatalf("a body carrying no secret must be returned byte for byte, got %s", got)
	}
}
