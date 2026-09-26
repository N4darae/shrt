package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func shopWorkspace(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(catalogtest.ShopDescriptor()))
	if config != "" {
		writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), config)
	}
	return dir
}

const shopConfig = `target:
    base_url: http://127.0.0.1:8080
descriptor:
    file: .shrt/descriptor.binpb
paths:
    chains: .shrt/chains
    contracts: .shrt/contracts
    runs: .shrt/runs
    safespots: .shrt/safespots
`

func TestInitExampleChainUsesTheDetectedEnvelopeWithoutGuessingItsValue(t *testing.T) {
	dir := shopWorkspace(t, "")
	restore := chdir(t, dir)
	defer restore()
	out := captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	raw, err := os.ReadFile(filepath.Join(dir, ".shrt", "chains", "example.yaml.template"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "error.code") || strings.Contains(text, "equals: OK") {
		t.Fatalf("init detected the verdict at status.code, yet the example asserts error.code: OK:\n%s", text)
	}
	if !strings.Contains(text, "- path: status.code\n        equals: REPLACE_ME_SUCCESS_VALUE") {
		t.Fatalf("the example must point at the detected envelope with a named placeholder, not a guessed value:\n%s", text)
	}
	if strings.Contains(text, "not_empty: true\n") && strings.Contains(text, "status.code\n        not_empty") {
		t.Fatalf("not_empty on the envelope passes on every refusal too, so the example must not teach it:\n%s", text)
	}
	if strings.Contains(text, "acme.") {
		t.Fatalf("the example names placeholder rpcs, not another company's services:\n%s", text)
	}
	if !strings.Contains(out, "target.base_url") {
		t.Fatalf("init's next: list must say to set target.base_url:\n%s", out)
	}
}

func TestInitExampleChainUsesTheConfiguredEnvelope(t *testing.T) {
	dir := shopWorkspace(t, shopConfig+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	restore := chdir(t, dir)
	defer restore()
	captureStdout(t, func() {
		if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
			t.Fatalf("init: %v", err)
		}
	})
	raw, err := os.ReadFile(filepath.Join(dir, ".shrt", "chains", "example.yaml.template"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "- path: status.code\n        equals: SUCCESS") != 2 {
		t.Fatalf("the example must assert the configured envelope and ok value on both steps:\n%s", raw)
	}
}

func TestRenderExampleChainFindsTheTemplatesEnvelopeAssertion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "agentkit", "templates", "chain.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), exampleEnvelopeExpect) {
		t.Fatalf("the template no longer contains %q, so init would silently stop rewriting its envelope", exampleEnvelopeExpect)
	}
}

func TestContractPlanWritesAServerStreamingRPCsChainReadingMessages(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	restore := chdir(t, dir)
	defer restore()
	var err error
	captureStdout(t, func() { err = contractPlan([]string{"WatchOrder", "-write"}) })
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ".shrt", "chains"))
	if len(entries) != 1 {
		t.Fatalf("want one chain written, found %d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".shrt", "chains", entries[0].Name()))
	if !strings.Contains(string(raw), "path: messages.0") {
		t.Fatalf("the happy call reads the first streamed message:\n%s", raw)
	}
}

func shopStatusWorkspace(t *testing.T) func() {
	t.Helper()
	dir := shopWorkspace(t, shopConfig)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "orders.yaml"), `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [NONE]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/WatchOrder:
        summary: streams an order
        required: [NONE]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
`)
	return chdir(t, dir)
}

func TestContractStatusCountsAPlannableServerStreamingRPCAsReached(t *testing.T) {
	defer shopStatusWorkspace(t)()
	out := captureStdout(t, func() {
		if err := contractStatus(nil); err != nil {
			t.Fatalf("status: %v", err)
		}
	})
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "orders" {
			if f[3] != "3" {
				t.Fatalf("REACHED for orders = %s, want 3 (CreateOrder, ConfirmOrder, WatchOrder):\n%s", f[3], out)
			}
			return
		}
	}
	t.Fatalf("no orders row:\n%s", out)
}

func TestContractStatusGapsPrintsOnlyTheGaps(t *testing.T) {
	defer shopStatusWorkspace(t)()
	out := captureStdout(t, func() {
		if err := contractStatus([]string{"-gaps"}); err != nil {
			t.Fatalf("status -gaps: %v", err)
		}
	})
	if strings.Contains(out, "DOMAIN") || strings.Contains(out, "CONTRACT counts") {
		t.Fatalf("-gaps must not reprint the table and footer:\n%s", out)
	}
	if !strings.Contains(out, "no contract  shop.catalog.v1.ProductService/CreateProduct") {
		t.Fatalf("-gaps must list uncovered rpcs:\n%s", out)
	}
	if strings.Contains(out, "streaming    shop.orders.v1.OrderService/WatchOrder") ||
		!strings.Contains(out, "no chain     shop.orders.v1.OrderService/WatchOrder") {
		t.Fatalf("a server-streaming rpc is callable, so no chain calling it is the gap:\n%s", out)
	}
	if !strings.Contains(out, "no contract      the rpc has no entry") || strings.Contains(out, "no path to") ||
		strings.Contains(out, "one item") {
		t.Fatalf("-gaps says in one line what each kind it found means, and nothing about kinds it did not find:\n%s", out)
	}
	out = captureStdout(t, func() {
		if err := contractStatus([]string{"-gaps", "-v"}); err != nil {
			t.Fatalf("status -gaps -v: %v", err)
		}
	})
	if !strings.Contains(out, "no path to   it has a contract, but appears in no multi-step plan") {
		t.Fatalf("-gaps -v explains each kind in full:\n%s", out)
	}
}

func TestContractStatusGapsHelpNamesBothKindsOfGap(t *testing.T) {
	defer shopStatusWorkspace(t)()
	var err error
	out := captureStderr(t, func() { err = contractStatus([]string{"-h"}) })
	if !strings.Contains(out, "no contract") || !strings.Contains(out, "no path to") {
		t.Fatalf("-gaps help must describe what -gaps prints (%v):\n%s", err, out)
	}
}
