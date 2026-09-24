package pathmask_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestScrubCatchesASecretEchoedInAnotherCaseOrWithoutItsPrefix(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	m.AddSecret("tok-33e73f3486cf")
	m.AddSecret("sk_live9f8e7d6c5b")
	m.AddSecret("Adm1")
	cases := map[string]string{
		"bearer TOK-33E73F3486CF rejected": "bearer <redacted> rejected",
		"session 33e73f3486cf expired":     "session <redacted> expired",
		"session 33E73F3486CF expired":     "session <redacted> expired",
		"key SK_LIVE9F8E7D6C5B":            "key <redacted>",
		"key live9f8e7d6c5b":               "key <redacted>",
		"tok-33e73f3486cf":                 "<redacted>",
		"ADM1 stays":                       "ADM1 stays",
		"Adm1 goes":                        "<redacted> goes",
		"33e73f34 is too short a tail":     "33e73f34 is too short a tail",
	}
	for in, want := range cases {
		if got := m.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
	raw := m.ScrubJSON(json.RawMessage(`{"code":"unauthenticated","message":"bad token TOK-33E73F3486CF (33e73f3486cf)"}`))
	if strings.Contains(strings.ToLower(string(raw)), "33e73f3486cf") {
		t.Fatalf("a token echoed upper-cased or without its prefix reached the record: %s", raw)
	}
}

func TestScrubDoesNotStripAPrefixThatIsNotShortAndAlphabetic(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	m.AddSecret("account-12345678")
	m.AddSecret("x1-abcdefghij")
	for _, in := range []string{"12345678", "abcdefghij"} {
		if got := m.ScrubText(in); got != in {
			t.Errorf("ScrubText(%q) = %q: only a short alphabetic prefix before a separator marks a token tail", in, got)
		}
	}
}
