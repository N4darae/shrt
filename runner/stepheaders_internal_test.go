package runner

import (
	"strings"
	"testing"
)

func TestRecordedHeadersKeepInputAndHideSecrets(t *testing.T) {
	templates := map[string]string{
		"x-dry-run":     "1",
		"X-Tenant":      "${vars.tenant}",
		"Authorization": "Bearer ${env.TOKEN}",
		"X-Api-Key":     "k-123",
		"X-Request-Id":  "${uuid}",
		"X-Order":       "${order.order.id_order}",
		"X-Shipping":    "fast",
		"X-Region":      "${env.REGION}",
		"X-Session":     "${login.session}",
		"X-Signed":      "${env.API_PASSWORD}",
	}
	resolved := map[string][]string{
		"x-dry-run":     {"1"},
		"X-Tenant":      {"acme"},
		"Authorization": {"Bearer abc"},
		"X-Api-Key":     {"k-123"},
		"X-Request-Id":  {"0f0f"},
		"X-Order":       {"o-1"},
		"X-Shipping":    {"fast"},
		"X-Region":      {"eu"},
		"X-Session":     {"s-1"},
		"X-Signed":      {"pw"},
	}
	got := recordedHeaders(templates, resolved)
	want := map[string]string{
		"X-Dry-Run":     "1",
		"X-Tenant":      "acme",
		"Authorization": HeaderDigest("Authorization", "Bearer abc"),
		"X-Api-Key":     HeaderDigest("X-Api-Key", "k-123"),
		"X-Request-Id":  "${uuid}",
		"X-Order":       "${steps.order.response.order.id_order}",
		"X-Shipping":    "fast",
		"X-Region":      "eu",
		"X-Session":     "<redacted>",
		"X-Signed":      HeaderDigest("X-Signed", "pw"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header %s: got %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	for _, secret := range []string{"Bearer abc", "k-123", "pw"} {
		for k, v := range got {
			if strings.Contains(v, secret) {
				t.Errorf("header %s stores the credential %q: %q", k, secret, v)
			}
		}
	}
	if changed := recordedHeaders(map[string]string{"X-Api-Key": "k-124"}, map[string][]string{"X-Api-Key": {"k-124"}}); changed["X-Api-Key"] == got["X-Api-Key"] {
		t.Errorf("another credential value must record another digest, so the change is seen: %v", changed)
	}
	if none := recordedHeaders(nil, nil); none == nil || len(none) != 0 {
		t.Fatalf("a step sent with no headers records an empty set, so a header added later is seen: %v", none)
	}
}
