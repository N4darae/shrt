package runner

import (
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
	}
	resolved := map[string][]string{
		"x-dry-run":     {"1"},
		"X-Tenant":      {"acme"},
		"Authorization": {"Bearer abc"},
		"X-Api-Key":     {"k-123"},
		"X-Request-Id":  {"0f0f"},
		"X-Order":       {"o-1"},
		"X-Shipping":    {"fast"},
	}
	got := recordedHeaders(templates, resolved)
	want := map[string]string{
		"X-Dry-Run":     "1",
		"X-Tenant":      "acme",
		"Authorization": "<redacted>",
		"X-Api-Key":     "<redacted>",
		"X-Request-Id":  "${uuid}",
		"X-Order":       "${steps.order.response.order.id_order}",
		"X-Shipping":    "fast",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header %s: got %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	if none := recordedHeaders(nil, nil); none == nil || len(none) != 0 {
		t.Fatalf("a step sent with no headers records an empty set, so a header added later is seen: %v", none)
	}
}
