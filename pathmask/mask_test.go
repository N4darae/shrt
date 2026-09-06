package pathmask_test

import (
	"encoding/json"
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestMatchSupportsWildcardsAndNameTolerance(t *testing.T) {
	yes := [][2]string{
		{"id", "id"},
		{"data.id", "data.id"},
		{"data.access_token", "data.accessToken"},
		{"data.*", "data.id"},
		{"**.id_book", "deal.legs.0.idBook"},
		{"**", "anything.at.all"},
		{"data.**.id", "data.a.b.id"},
		{"data.**.id", "data.id"},
		{"deal.legs.*.qty", "deal.legs.0.qty"},
	}
	for _, c := range yes {
		if !pathmask.Match(c[0], c[1]) {
			t.Errorf("pattern %q should match %q", c[0], c[1])
		}
	}
	no := [][2]string{
		{"id", "data.id"},
		{"data.id", "id"},
		{"data.*", "data.a.b"},
		{"data.id", "data.identifier"},
		{"deal.legs.*.qty", "deal.legs.0.1.qty"},
	}
	for _, c := range no {
		if pathmask.Match(c[0], c[1]) {
			t.Errorf("pattern %q should not match %q", c[0], c[1])
		}
	}
}

func decode(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("fixture is not json: %v", err)
	}
	return v
}

func reencode(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestApplyReplacesOnlyMatchedPaths(t *testing.T) {
	body := decode(t, `{"id_deal":"d1","created_at":"2026-01-01","legs":[{"qty":"5"},{"qty":"7"}]}`)
	masked := pathmask.NewMasker([]string{"created_at", "legs.*.qty"}).Apply(body)

	got := masked.(map[string]any)
	if got["id_deal"] != "d1" {
		t.Fatalf("an unmatched field must survive, got %v", got["id_deal"])
	}
	if got["created_at"] != pathmask.MaskVolatile {
		t.Fatalf("created_at not masked, got %v", got["created_at"])
	}
	for i, leg := range got["legs"].([]any) {
		if leg.(map[string]any)["qty"] != pathmask.MaskVolatile {
			t.Fatalf("legs.%d.qty not masked, got %v", i, leg)
		}
	}
}

func TestApplyDoesNotMutateItsInput(t *testing.T) {
	body := decode(t, `{"token":"secret","nested":{"token":"secret"}}`)
	before := reencode(t, body)

	pathmask.NewRedactor([]string{"**.token", "token"}).Apply(body)

	if after := reencode(t, body); after != before {
		t.Fatalf("Apply mutated its input:\n before %s\n after  %s", before, after)
	}
}

func TestRedactorUsesItsOwnMarker(t *testing.T) {
	body := decode(t, `{"password":"hunter2"}`)
	got := pathmask.NewRedactor([]string{"password"}).Apply(body).(map[string]any)
	if got["password"] != pathmask.MaskRedacted {
		t.Fatalf("want %q, got %v", pathmask.MaskRedacted, got["password"])
	}
	if pathmask.MaskRedacted == pathmask.MaskVolatile {
		t.Fatal("a redaction must be distinguishable from a volatile mask in a diff")
	}
}

func TestApplyMasksAWholeSubtree(t *testing.T) {
	body := decode(t, `{"auth":{"access_token":"t","expires_at":"1"},"id":"x"}`)
	got := pathmask.NewRedactor([]string{"auth"}).Apply(body).(map[string]any)
	if got["auth"] != pathmask.MaskRedacted {
		t.Fatalf("a matched message must be replaced wholesale, got %v", got["auth"])
	}
	if got["id"] != "x" {
		t.Fatalf("siblings must survive, got %v", got["id"])
	}
}

func TestApplyIsIdentityWithoutPatterns(t *testing.T) {
	body := decode(t, `{"a":1}`)
	if got := pathmask.NewMasker(nil).Apply(body); reencode(t, got) != reencode(t, body) {
		t.Fatalf("no patterns must mean no change, got %v", got)
	}
	var nilMasker *pathmask.Masker
	if got := nilMasker.Apply(body); reencode(t, got) != reencode(t, body) {
		t.Fatalf("a nil masker must be a safe identity, got %v", got)
	}
	if nilMasker.Patterns() != nil {
		t.Fatal("a nil masker has no patterns")
	}
}

func TestIndexKeyMatchesTheJSONPathSegmentsApplyProduces(t *testing.T) {
	for i, want := range map[int]string{0: "0", 9: "9", 10: "10", 42: "42", 100: "100"} {
		if got := pathmask.IndexKey(i); got != want {
			t.Errorf("IndexKey(%d) = %q, want %q", i, got, want)
		}
	}
	body := []any{}
	for i := 0; i < 12; i++ {
		body = append(body, map[string]any{"qty": "1"})
	}
	got := pathmask.NewMasker([]string{"10.qty"}).Apply(body).([]any)
	if got[10].(map[string]any)["qty"] != pathmask.MaskVolatile {
		t.Fatalf("a two-digit index must be addressable, got %v", got[10])
	}
	if got[1].(map[string]any)["qty"] != "1" {
		t.Fatalf("index 10 must not also match index 1, got %v", got[1])
	}
}

func TestJoinBuildsDottedPaths(t *testing.T) {
	if got := pathmask.Join("", "a"); got != "a" {
		t.Fatalf("Join at the root = %q", got)
	}
	if got := pathmask.Join("a.b", "c"); got != "a.b.c" {
		t.Fatalf("Join = %q", got)
	}
}
