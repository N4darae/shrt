package pathmask_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestScrubCatchesABase64OrPercentEncodedSecret(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	token := "tok-33e73f3486cf~~~"
	m.AddSecret(token)
	m.AddSecret("s3cret-admin")
	m.AddSecret("short")
	wrapped := base64.StdEncoding.EncodeToString(append([]byte{0x0a, 0x12}, []byte(token+"\x10\x01")...))
	cases := map[string]string{
		"details " + base64.StdEncoding.EncodeToString([]byte(token)):    "details <redacted>",
		"details " + base64.RawStdEncoding.EncodeToString([]byte(token)): "details <redacted>",
		"details " + base64.URLEncoding.EncodeToString([]byte(token)):    "details <redacted>",
		"details " + base64.RawURLEncoding.EncodeToString([]byte(token)): "details <redacted>",
		"pw s3cret%2Dadmin":   "pw <redacted>",
		"pw s3cret%2dadmin":   "pw <redacted>",
		"pw %73%33cret-admin": "pw <redacted>",
		"pw " + base64.StdEncoding.EncodeToString([]byte("s3cret-admin")): "pw <redacted>",
		"n " + base64.StdEncoding.EncodeToString([]byte("short")):         "n " + base64.StdEncoding.EncodeToString([]byte("short")),
		"reversed nimda-terc3s":          "reversed nimda-terc3s",
		"spaced s 3 c r e t - a d m i n": "spaced s 3 c r e t - a d m i n",
	}
	for in, want := range cases {
		if got := m.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
	raw := m.ScrubJSON(json.RawMessage(`{"code":"permission_denied","details":[{"type":"x.v1.Info","value":"` + wrapped + `"}]}`))
	for shift := 0; shift < 3; shift++ {
		enc := base64.StdEncoding.EncodeToString(append(make([]byte, shift), []byte(token)...))
		core := enc[(8*shift+5)/6 : 8*(shift+len(token))/6]
		if strings.Contains(string(raw), core[1:len(core)-1]) {
			t.Fatalf("the token base64-encoded inside a Connect error detail reached the record: %s", raw)
		}
	}
}
