package chain_test

import (
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
)

func newScope() *chain.Scope {
	s := chain.NewScope(map[string]any{"book_id": "BOOK-A", "nested": map[string]any{"n": 7}})
	s.Env = func(k string) (string, bool) {
		if k == "API_USER" {
			return "staff", true
		}
		return "", false
	}
	s.Now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	s.Record("create", map[string]any{"name": "widget"}, map[string]any{
		"id":    "thing-1",
		"deals": []any{map[string]any{"id": "d1"}, map[string]any{"id": "d2"}},
		"count": float64(2),
	})
	s.Exports["thing_id"] = "thing-1"
	return s
}

func TestResolveReferences(t *testing.T) {
	s := newScope()
	cases := []struct {
		in   string
		want any
	}{
		{"${vars.book_id}", "BOOK-A"},
		{"${vars.nested.n}", 7},
		{"${env.API_USER}", "staff"},
		{"${create.id}", "thing-1"},
		{"${steps.create.response.id}", "thing-1"},
		{"${steps.create.request.name}", "widget"},
		{"${create.deals.1.id}", "d2"},
		{"${exports.thing_id}", "thing-1"},
		{"${thing_id}", "thing-1"},
		{"deal-${create.id}-suffix", "deal-thing-1-suffix"},
		{"${nowunix}", "1700000000"},
		{"plain", "plain"},
	}
	for _, c := range cases {
		got, err := s.ResolveValue(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%s = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestUnresolvedReferenceIsAnError(t *testing.T) {
	s := newScope()
	for _, in := range []string{"${vars.missing}", "${env.NOT_SET}", "${later.id}", "${create.nope}"} {
		if _, err := s.ResolveValue(in); err == nil {
			t.Fatalf("%s should not resolve", in)
		}
	}
}

func TestResolvePreservesNonStringTypes(t *testing.T) {
	s := newScope()
	got, err := s.ResolveValue(map[string]any{"n": "${create.count}", "flag": true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	m := got.(map[string]any)
	if m["n"] != float64(2) {
		t.Fatalf("want the numeric type preserved, got %#v", m["n"])
	}
	if m["flag"] != true {
		t.Fatalf("want flag untouched, got %#v", m["flag"])
	}
}

func TestUUIDIsFreshPerReference(t *testing.T) {
	s := newScope()
	a, _ := s.ResolveValue("${uuid}")
	b, _ := s.ResolveValue("${uuid}")
	if a == b {
		t.Fatal("${uuid} must produce a new value each time")
	}
}
