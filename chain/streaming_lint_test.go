package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestLintErrorsOnAStepCallingAClientStreamingRPC(t *testing.T) {
	cat := catalogtest.Rich()
	c := &chain.Chain{
		APIVersion: chain.APIVersion,
		Name:       "watch",
		Steps: []*chain.Step{
			{ID: "place_order", Call: "OrderService/PlaceOrder"},
			{ID: "upload_orders", Call: "OrderService/UploadOrders"},
			{ID: "watch_order", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "x"},
				Expect: []chain.Expectation{{Path: "messages.0.state", NotEmpty: true}}},
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
	if found[0].Severity != chain.SeverityError || found[0].Step != "upload_orders" {
		t.Fatalf("the streaming issue must be an error on the calling step: %+v", found[0])
	}
	if !strings.Contains(found[0].Message, "server-streaming rpcs only") {
		t.Fatalf("the message must say why: %q", found[0].Message)
	}
}

func TestLintReadsAServerStreamingStepUnderMessages(t *testing.T) {
	c := &chain.Chain{
		APIVersion: chain.APIVersion,
		Name:       "watch",
		Steps: []*chain.Step{{ID: "watch_order", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "x"},
			Expect: []chain.Expectation{{Path: "messages.0.state", NotEmpty: true}, {Path: "state", NotEmpty: true}}}},
	}
	bad := []string{}
	for _, i := range chain.Lint(c, catalogtest.Rich()) {
		if i.Severity == chain.SeverityError {
			bad = append(bad, i.Message)
		}
	}
	if len(bad) != 1 || !strings.Contains(bad[0], `"state"`) {
		t.Fatalf("messages.0.state is a path of the stream and state is not, got %v", bad)
	}
}
