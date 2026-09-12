package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestLintErrorsOnAStepCallingAStreamingRPC(t *testing.T) {
	cat := catalogtest.Rich()
	c := &chain.Chain{
		APIVersion: chain.APIVersion,
		Name:       "watch",
		Steps: []*chain.Step{
			{ID: "place_order", Call: "OrderService/PlaceOrder"},
			{ID: "watch_order", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "x"}},
		},
	}
	issues := chain.Lint(c, cat)
	found := []chain.Issue{}
	for _, i := range issues {
		if strings.Contains(i.Message, "streaming") {
			found = append(found, i)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one streaming issue, got %v", issues)
	}
	if found[0].Severity != chain.SeverityError || found[0].Step != "watch_order" {
		t.Fatalf("the streaming issue must be an error on the calling step: %+v", found[0])
	}
	if !strings.Contains(found[0].Message, "unary-only") {
		t.Fatalf("the message must say why: %q", found[0].Message)
	}
}
