package pathmask_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestScrubReplacesAKnownSecretUsedAsAnObjectKey(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	m.AddSecret("tok-abcdef")
	raw := m.ScrubJSON(json.RawMessage(`{"tok-abcdef":1,"sessions":{"tok-abcdef":{"n":2}},"name":"x"}`))
	if strings.Contains(string(raw), "tok-abcdef") {
		t.Fatalf("a secret used as an object key must be scrubbed, got %s", raw)
	}
	got, ok := m.ScrubValue(map[string]any{"tok-abcdef": 1, "<redacted>": 2}).(map[string]any)
	if !ok || len(got) != 2 {
		t.Fatalf("scrubbing a key must not drop an entry whose scrubbed key collides with another, got %v", got)
	}
	for k := range got {
		if strings.Contains(k, "tok-abcdef") {
			t.Fatalf("ScrubValue left the secret in a key: %v", got)
		}
	}
}
