package pathmask_test

import (
	"encoding/base32"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestScrubCatchesAHexHTMLEntityOrBase32EncodedSecret(t *testing.T) {
	m := pathmask.NewRedactor(nil)
	m.AddSecret("s3cret-admin")
	secret := []byte("s3cret-admin")
	b32 := base32.StdEncoding.EncodeToString(secret)
	cases := map[string]string{
		"hex " + hex.EncodeToString(secret):                               "hex <redacted>",
		"hex " + strings.ToUpper(hex.EncodeToString(secret)):              "hex <redacted>",
		"hex 0a" + hex.EncodeToString(secret) + "ff":                      "hex 0a<redacted>ff",
		"ent s3cret&#45;admin":                                            "ent <redacted>",
		"ent &#x73;3cret&#X2D;admin":                                      "ent <redacted>",
		"ent &#115;&#51;cret&#0045;admin":                                 "ent <redacted>",
		"b32 " + b32:                                                      "b32 <redacted>",
		"b32 " + strings.TrimRight(b32, "="):                              "b32 <redacted>",
		"b32 " + strings.ToLower(b32):                                     "b32 <redacted>",
		"b32 " + base32.HexEncoding.EncodeToString(secret):                "b32 <redacted>",
		"hex " + hex.EncodeToString([]byte("short")):                      "hex " + hex.EncodeToString([]byte("short")),
		"plain & text &#45; with entities":                                "plain & text &#45; with entities",
		"b32 " + base32.StdEncoding.EncodeToString([]byte("other-value")): "b32 " + base32.StdEncoding.EncodeToString([]byte("other-value")),
	}
	for in, want := range cases {
		if got := m.ScrubText(in); got != want {
			t.Errorf("ScrubText(%q) = %q, want %q", in, got, want)
		}
	}
	for shift := 0; shift < 5; shift++ {
		enc := base32.StdEncoding.EncodeToString(append(append(make([]byte, shift), secret...), 1, 2, 3))
		got := m.ScrubText("b32 " + enc)
		core := enc[(8*shift+4)/5 : 8*(shift+len(secret))/5]
		if strings.Contains(got, core) {
			t.Errorf("base32 at byte offset %d kept the secret: %q", shift, got)
		}
	}
}
