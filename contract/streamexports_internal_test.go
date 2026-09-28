package contract

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestContractLintAcceptsAStreamingRPCsExportUnderMessages(t *testing.T) {
	lib := libraryFrom(t, `apiVersion: shrt/contract/v1
rpcs:
    shop.orders.v1.OrderService/WatchOrder:
        summary: streams the order
        required: [NONE]
        exports:
            messages: each message
            messages.0.order: the first message's order
            order: the order
            messages.0.nope: no such field
        status: draft
`)
	var bad []string
	for _, is := range LintLibrary(lib, catalogtest.Shop()) {
		if strings.Contains(is.Message, "exports names") {
			bad = append(bad, is.Message)
		}
	}
	if len(bad) != 1 || !strings.Contains(bad[0], `"messages.0.nope"`) {
		t.Fatalf("messages and messages.0.<field> are paths of a streaming rpc; only the unknown field is refused: %v", bad)
	}
}
