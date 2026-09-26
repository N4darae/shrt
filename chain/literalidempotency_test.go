package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestALiteralIdempotencyKeyIsWarned(t *testing.T) {
	got := issuesOfKind(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "w-${vars.tag}", "idempotency_key": "fixed-key-ao-1"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	}, chain.KindLiteralIdempotency)
	if len(got) != 1 || got[0].Severity != chain.SeverityWarn {
		t.Fatalf("want 1 literal-idempotency-key warning, got %v", got)
	}
	for _, want := range []string{"idempotency_key", "fixed-key-ao-1", "${uuid}"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("message must say %q: %s", want, got[0].Message)
		}
	}
	for _, key := range []string{"${uuid}", "k-${vars.tag}"} {
		if got := issuesOfKind(&chain.Step{
			ID: "create", Call: "ThingService/Create", SkipAuth: true,
			Body:   map[string]any{"name": "w", "idempotency_key": key},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
		}, chain.KindLiteralIdempotency); len(got) != 0 {
			t.Fatalf("a key built from %s is not a literal, got %v", key, got)
		}
	}
}

func TestALiteralIdempotencyHeaderIsWarned(t *testing.T) {
	got := issuesOfKind(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Headers: map[string]string{"Idempotency-Key": "k-1"},
		Body:    map[string]any{"name": "w"},
		Expect:  []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	}, chain.KindLiteralIdempotency)
	if len(got) != 1 || !strings.Contains(got[0].Message, "Idempotency-Key") {
		t.Fatalf("want 1 literal-idempotency-key warning for the header, got %v", got)
	}
}
