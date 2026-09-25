package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func prefixStep(prefix string, expect ...chain.Expectation) *chain.Step {
	return &chain.Step{
		ID: "list", Call: "ThingService/Fetch", SkipAuth: true,
		Body:   map[string]any{"id_prefix": prefix},
		Expect: append([]chain.Expectation{{Path: "error.code", Equals: "OK"}}, expect...),
	}
}

func TestAPrefixEndingInAVarIsWarnedWhenTheStepCountsItsItems(t *testing.T) {
	no := false
	got := issuesOfKind(prefixStep("sku-${vars.tag}", chain.Expectation{Path: "items.2", Exists: &no}), chain.KindUnterminatedPrefix)
	if len(got) != 1 || got[0].Severity != chain.SeverityWarn {
		t.Fatalf("want one unterminated-prefix warning, got %v", got)
	}
	for _, want := range []string{"id_prefix", "sku-${vars.tag}", "tag=cp-1", "cp-10", "sku-${vars.tag}-"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("the warning must say %q: %s", want, got[0].Message)
		}
	}
	for _, prefix := range []string{"sku-${vars.tag}-", "sku-fixed", "${vars.tag}/"} {
		if got := issuesOfKind(prefixStep(prefix, chain.Expectation{Path: "items.2", Exists: &no}), chain.KindUnterminatedPrefix); len(got) != 0 {
			t.Fatalf("%q ends with a terminator or reads no var: %v", prefix, got)
		}
	}
	if got := issuesOfKind(prefixStep("sku-${vars.tag}"), chain.KindUnterminatedPrefix); len(got) != 0 {
		t.Fatalf("a step asserting no item position or count cannot be thrown by extra items: %v", got)
	}
}
