package transport_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/transport"
)

func TestATokenRefusalAnEarlierBuildRecordedRoundTripsSoItsRunKeepsItsSeal(t *testing.T) {
	raw := `{"token":"5f8c09ca","refused_at":"2026-09-27T21:37:58Z","cached":true,"first_use":true,"relogins":["2026-09-27T21:37:40Z"]}`
	var r transport.TokenRefusal
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	back, err := json.Marshal(r)
	if err != nil || string(back) != raw {
		t.Fatalf("a run record's seal covers every field it was written with, so relogins must survive: %v\n%s", err, back)
	}
}
