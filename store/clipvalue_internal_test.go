package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortValueClipsAtWordAndSegmentBoundariesAndKeepsTheTail(t *testing.T) {
	for _, tc := range []struct {
		in       string
		keep     []string
		mustNot  []string
		endsWith string
	}{
		{in: "order is already confirmed", keep: []string{"order", "confirmed"}, mustNot: []string{"alr…", "…dy"}},
		{in: "cust-pre2-order-flow@example.test", keep: []string{"cust-", "@example.test"}, endsWith: "@example.test"},
		{in: "ord-9f8e7d6c-5b4a-3210-aaaa-bbbbccccdddd", endsWith: "bbbbccccdddd"},
	} {
		got := shortValue(tc.in)
		if utf8.RuneCountInString(got) > summaryValue+2 {
			t.Errorf("shortValue(%q) = %q, longer than the cell allows", tc.in, got)
		}
		if !strings.Contains(got, "…") {
			t.Errorf("shortValue(%q) = %q, want it clipped", tc.in, got)
		}
		for _, k := range tc.keep {
			if !strings.Contains(got, k) {
				t.Errorf("shortValue(%q) = %q, want it to keep %q", tc.in, got, k)
			}
		}
		for _, k := range tc.mustNot {
			if strings.Contains(got, k) {
				t.Errorf("shortValue(%q) = %q cuts a word: %q", tc.in, got, k)
			}
		}
		if tc.endsWith != "" && !strings.HasSuffix(got, tc.endsWith) {
			t.Errorf("shortValue(%q) = %q, want the tail %q kept", tc.in, got, tc.endsWith)
		}
	}
	if got := shortValue("short"); got != "short" {
		t.Errorf("a value that fits is kept as it is, got %q", got)
	}
}

func TestACellIsClippedAtAWordOrSegmentBoundary(t *testing.T) {
	in := "lines.0.qty=5 lines.1.qty=4 lines.0.id_product=prd-a273c881ff55 lines.1.id_product=prd-3b0a1b2c3d4e"
	got := clip(in, 90)
	if !strings.HasSuffix(got, "…") || strings.Contains(got, "prd-3b0…") {
		t.Fatalf("clip(%q) = %q, want it cut before the value it cannot hold whole", in, got)
	}
	if !strings.HasPrefix(in, strings.TrimSuffix(got, "…")) {
		t.Fatalf("clip keeps a prefix of the text, got %q", got)
	}
}
