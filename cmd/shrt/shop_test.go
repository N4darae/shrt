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

func TestInitWritesAnExampleChainForTheEnvelope(t *testing.T) {
	for _, c := range []struct {
		name, config string
		has, lacks   []string
		count        map[string]int
	}{
		{"the detected envelope with a named placeholder, not a guessed value", "",
			[]string{"- path: status.code\n        equals: REPLACE_ME_SUCCESS_VALUE"}, []string{"error.code", "equals: OK", "status.code\n        not_empty", "acme."}, nil},
		{"the configured envelope and ok value on both steps", shopConfig + "conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n",
			[]string{"equals: ${vars.thing_name}", "REPLACE_ME"}, []string{"\n#", "\n    #"}, map[string]int{"- path: status.code\n        equals: SUCCESS": 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := shopWorkspace(t, c.config)
			defer chdir(t, dir)()
			out := captureStdout(t, func() {
				if err := runInit(t.Context(), []string{"-build=false", "-agents=false"}); err != nil {
					t.Fatalf("init: %v", err)
				}
			})
			text := string(mustRead(t, filepath.Join(dir, ".shrt", "chains", "example.yaml.template")))
			for _, s := range c.has {
				if !strings.Contains(text, s) {
					t.Errorf("want %q in:\n%s", s, text)
				}
			}
			for _, s := range c.lacks {
				if strings.Contains(text, s) {
					t.Errorf("want no %q in:\n%s", s, text)
				}
			}
			for s, n := range c.count {
				if strings.Count(text, s) != n {
					t.Errorf("want %q %d times in:\n%s", s, n, text)
				}
			}
			if c.config == "" && !strings.Contains(out, "target.base_url") {
				t.Errorf("without a config, init's next steps say to set target.base_url:\n%s", out)
			}
			if body := readGitignore(t, dir); !strings.Contains(body, ".shrt/safespots/pending/\n") || strings.Contains(body, ".shrt/safespots/\n") {
				t.Errorf("only pending safe-spot proposals are gitignored:\n%s", body)
			}
		})
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "agentkit", "templates", "chain.example.yaml"))
	if err != nil || !strings.Contains(string(raw), exampleEnvelopeExpect) {
		t.Errorf("the template no longer contains %q, so init would silently stop rewriting its envelope (%v)", exampleEnvelopeExpect, err)
	}
}

func TestContractCommandsOnTheShop(t *testing.T) {
	defer shopStatusWorkspace(t)()
	var err error
	stderr := captureStderr(t, func() { captureStdout(t, func() { err = contractShow([]string{"ConfirmOrder"}) }) })
	if err != nil || !strings.Contains(stderr, "id_order") || !strings.Contains(stderr, "CreateOrder") {
		t.Errorf("contract show names the producer the chain step lacks (%v):\n%s", err, stderr)
	}
	out := captureStdout(t, func() { err = contractStatus(nil) })
	reached := ""
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 4 && f[0] == "orders" {
			reached = f[3]
		}
	}
	if err != nil || reached != "3" {
		t.Errorf("a plannable server-streaming rpc counts as reached: orders REACHED %q, want 3 (%v):\n%s", reached, err, out)
	}
	usage := captureStderr(t, func() { _ = contractStatus([]string{"-h"}) })
	if !strings.Contains(usage, "no contract") || !strings.Contains(usage, "no path to") {
		t.Errorf("-gaps help names both kinds of gap:\n%s", usage)
	}
	captureStdout(t, func() { err = contractPlan([]string{"WatchOrder", "-write"}) })
	entries, _ := os.ReadDir(".shrt/chains")
	if err != nil || len(entries) != 1 || !strings.Contains(string(mustRead(t, filepath.Join(".shrt", "chains", entries[0].Name()))), "path: messages.0") {
		t.Errorf("contract plan writes one chain for a server-streaming rpc, reading its first message (%v, %d files)", err, len(entries))
	}
}

func TestContractStatusGaps(t *testing.T) {
	const oneLine = `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create_order
      call: OrderService/CreateOrder
      body:
          id_customer: c
          lines:
              - id_product: p
                qty: "3"
`
	const twoLines = `apiVersion: shrt/v1
name: two
steps:
    - id: create_order
      call: OrderService/CreateOrder
      body:
          lines:
              - id_product: p
                qty: "3"
              - id_product: q
                qty: "1"
`
	const sameProduct = `apiVersion: shrt/v1
name: same
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
          sku: s
    - id: create_order
      call: OrderService/CreateOrder
      body:
          lines:
              - id_product: ${create_product.product.id_product}
                qty: "2"
              - id_product: ${create_product.product.id_product}
                qty: "3"
`
	for _, c := range []struct {
		name      string
		setup     func(t *testing.T) func()
		args      []string
		has, lack []string
	}{
		{"-gaps prints only the gaps and one line of what to run for them", shopStatusWorkspace, []string{"-gaps"},
			[]string{"no contract  shop.catalog.v1.ProductService/CreateProduct", "no chain     shop.orders.v1.OrderService/WatchOrder", "\nnext: shrt contract init <domain> (no contract); shrt contract plan <rpc> (no chain"},
			[]string{"DOMAIN", "CONTRACT counts", "streaming    shop.orders.v1.OrderService/WatchOrder", "no path to", "one item", "no contract      the rpc has no entry", "-v explains", "\n\n"}},
		{"-gaps -v explains each kind in full", shopStatusWorkspace, []string{"-gaps", "-v"},
			[]string{"no path to   it has a contract, but appears in no multi-step plan"}, nil},
		{"a repeated field no chain sends twice", func(t *testing.T) func() { return shopChains(t, oneLine) }, []string{"-gaps"},
			[]string{"one item     shop.orders.v1.OrderService/CreateOrder lines: at most 1 item in every chain that sends it (cli-thing-flow)", "; a step sending two items with different values (one item)"}, nil},
		{"a chain sending two items leaves no single-item gap", func(t *testing.T) func() { return shopChains(t, oneLine, twoLines) }, []string{"-gaps"},
			nil, []string{"one item     shop"}},
		{"items that all point at one resource", func(t *testing.T) func() { return shopChains(t, sameProduct) }, []string{"-gaps"},
			[]string{"same resource shop.orders.v1.OrderService/CreateOrder lines: every chain that sends two or more items points them all at ${create_product.product.id_product} (same)", "shrt contract plan <rpc> (", "same resource"},
			[]string{"no gaps"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer c.setup(t)()
			var err error
			out := captureStdout(t, func() { err = contractStatus(c.args) })
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range c.has {
				if !strings.Contains(out, s) {
					t.Errorf("want %q in:\n%s", s, out)
				}
			}
			for _, s := range c.lack {
				if strings.Contains(out, s) {
					t.Errorf("want no %q in:\n%s", s, out)
				}
			}
		})
	}
}

func shopChains(t *testing.T, chains ...string) func() {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	for _, c := range chains {
		_, rest, _ := strings.Cut(c, "name: ")
		name, _, _ := strings.Cut(rest, "\n")
		writeFile(t, ".shrt/chains/"+name+".yaml", c)
	}
	return func() {}
}
