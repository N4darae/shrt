package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func renameThingFlow(t *testing.T, edit func(string) string) {
	t.Helper()
	approvedThingFlow(t)
	raw := string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml"))
	renamed := strings.Replace(raw, "name: cli-thing-flow\n", "name: cli-renamed\n", 1)
	if edit != nil {
		renamed = edit(renamed)
	}
	writeFile(t, ".shrt/chains/cli-renamed.yaml", renamed)
	if err := os.Remove(".shrt/chains/cli-thing-flow.yaml"); err != nil {
		t.Fatal(err)
	}
}

func confirmRename(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = runConfirm(context.Background(), append([]string{"cli-renamed", "-rename-from", "cli-thing-flow"}, args...))
	})
	return out, err
}

const twoLineDefectChain = `apiVersion: shrt/v1
name: cli-two-lines
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
    - id: other
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: gizmo
    - id: fine
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
`

func twoLineWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two-lines.yaml", twoLineDefectChain)
	if err := runRun(context.Background(), []string{"cli-two-lines", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

func keptRedPaths(c *chain.Chain) string {
	parts := []string{}
	for _, k := range c.KeptRed {
		parts = append(parts, k.Step+":"+k.Path)
	}
	return strings.Join(parts, ",")
}

func writeNoisyChain(t *testing.T, createName string) {
	t.Helper()
	writeFile(t, ".shrt/chains/cli-noisy-flow.yaml", `apiVersion: shrt/v1
name: cli-noisy-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: `+createName+`
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fill
      call: ThingService/Create
      body:
          name: filler
          kind: KIND_A
          idempotency_key: ${uuid}
          meta:
              trace_id: thing-1
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`)
}

func exitCodeOf(err error) int {
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	if err != nil {
		return 1
	}
	return 0
}

const blockedReadChain = `apiVersion: shrt/v1
name: cli-blocked
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: ${fetch.name}
`

func blockedWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-blocked.yaml", blockedReadChain)
	if err := runRun(context.Background(), []string{"cli-blocked", "-quiet", "-keep-going"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

const customerChain = `apiVersion: shrt/v1
name: %s
vars:
    tag: t
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: c-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
        - path: customer.email
          equals: c-${vars.tag}@example.test
    - id: create_order
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        idempotency_key: k-${uuid}
      expect:
        - path: status.code
          equals: SUCCESS
`

func newTotallingBackend(total *int) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget", "total": *total})
		default:
			w.WriteHeader(404)
		}
	}))
}

const skuEchoChain = `apiVersion: shrt/v1
name: sku-echo
vars:
    tag: sku-echo
steps:
    - id: create
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: product.sku
          equals: ${steps.create.request.sku}
`

const stockedEchoChain = `apiVersion: shrt/v1
name: stocked-echo
vars:
    tag: stocked
steps:
    - id: create
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create.product.id_product}
        qty: "3"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: product.sku
          equals: ${steps.create.request.sku}
`

const freshTagChain = `apiVersion: shrt/v1
name: cli-fresh-flow
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
    - id: by_tag
      call: ThingService/Fetch
      body:
          id: t-${vars.tag}
      expect:
          - path: error.code
            equals: OK
`

func freshTagWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fresh-flow.yaml", freshTagChain)
	if err := runRun(context.Background(), []string{"cli-fresh-flow", "-quiet", "-var", "tag=T9"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
}

const confirmThenFetchChain = `apiVersion: shrt/v1
name: probe-confirm
kept_red:
    - step: fetch_order
      path: order.total_minor
steps:
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: cust-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-a
        price_minor: "100"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
        idempotency_key: ${uuid}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: confirm_order
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: fetch_order
      call: OrderService/FetchOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: order.total_minor
          equals: 999
`

const oneDefectChain = `apiVersion: shrt/v1
name: cli-one-defect
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: gadget
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: widget
`

func oneDefectWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-one-defect.yaml", oneDefectChain)
	if err := runRun(context.Background(), []string{"cli-one-defect", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch")
	}
}

func keptRedSlice(ctx context.Context, args []string) error {
	red := &sliceKeptRed{}
	rest := []string{}
	for _, a := range args {
		name, steps, _ := strings.Cut(a, "=")
		if name != "-kept-red" {
			rest = append(rest, a)
			continue
		}
		red.on = true
		for _, id := range strings.Split(steps, ",") {
			if id != "" && !slices.Contains(red.steps, id) {
				red.steps = append(red.steps, id)
			}
		}
	}
	return sliceChain(ctx, rest, red)
}

const twoDefectChain = `apiVersion: shrt/v1
name: cli-two-defects
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: %s
            equals: %s
    - id: fetch_again
      call: ThingService/Fetch
      body:
          id: ${fetch.id}
      expect:
          - path: name
            equals: gadget
`

func twoDefectWorkspace(t *testing.T, path, want string) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-two-defects.yaml", strings.Replace(strings.Replace(twoDefectChain, "%s", path, 1), "%s", want, 1))
	if err := runRun(context.Background(), []string{"cli-two-defects", "-quiet", "-keep-going", "-var", "tag=T9"}); err == nil {
		t.Fatalf("the chain must fail at fetch and fetch_again")
	}
}

func twoDefectSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = keptRedSlice(context.Background(), append([]string{"cli-two-defects"}, args...))
	})
	return out, err
}

func runIDsOf(t *testing.T, chainName string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(".shrt", "runs", chainName))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	return ids
}

func writeEnvFetchChain(t *testing.T) {
	t.Helper()
	writeEnvFetchChainReading(t, "${env.SHRT_LAB5_NAME}")
}

func writeEnvFetchChainReading(t *testing.T, id string) {
	t.Helper()
	writeFile(t, ".shrt/chains/cli-env-flow.yaml", `apiVersion: shrt/v1
name: cli-env-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: `+id+`
      expect:
          - path: error.code
            equals: OK
`)
}

func newLeakyRefusalBackend() *httptest.Server {
	next := 0
	totals := map[string]int{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			if meta, _ := body["meta"].(map[string]any); meta != nil {
				if id, _ := meta["trace_id"].(string); id != "" {
					totals[id]--
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED"}})
					return
				}
			}
			next++
			id := "thing-" + itoa(next)
			totals[id] = 5
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": id, "total": totals[id]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func leakyRefusalChain(takeExpects string) string {
	return `apiVersion: shrt/v1
name: cli-leaky
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body:
          name: other
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: take
      call: ThingService/Create
      body:
          name: take
          kind: KIND_A
          idempotency_key: ${uuid}
          meta:
              trace_id: ${create.id}
      expect:
          - path: error.code
            equals: ` + takeExpects + `
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: total
            equals: 5
`
}

const flakyGetChain = `apiVersion: shrt/v1
name: probe-get
steps:
    - id: create
      call: ProductService/CreateProduct
      body:
        sku: sku-${uuid}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_a
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_b
      call: ProductService/GetProduct
      body:
        id_product: ${create.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
`

const pricedOrderChain = `apiVersion: shrt/v1
name: order-happy
vars:
    tag: happy
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "1250"
      expect:
        - path: status.code
          equals: SUCCESS
        - path: product.price_minor
          equals: 1250
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_customer
      call: CustomerService/CreateCustomer
      body:
        email: c-${vars.tag}@example.test
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "2"
        idempotency_key: k-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
        - path: order.total_minor
          equals: 2500
    - id: confirm_order
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
`

type sliceBackend struct {
	fetch func(id string) (int, map[string]any)
	next  int
}

func newSliceBackend(t *testing.T, b *sliceBackend) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			b.next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(b.next)})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			code, out := b.fetch(id)
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-pair.yaml", `apiVersion: shrt/v1
name: cli-pair
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: id
            equals: thing-0
`)
}

func newRound2Backend(fetchCode *string) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		refuse := func(msg string) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "invalid_argument", "message": msg})
		}
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			switch body["name"] {
			case nil, "":
				refuse("name required")
			case "filler":
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "INTERNAL"}})
			default:
				next++
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
			}
		case "/shrt.test.v1.ThingService/Fetch":
			if body["id"] == nil || body["id"] == "" {
				refuse("id required")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": *fetchCode}, "id": body["id"], "name": "widget"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

const round2Chain = `apiVersion: shrt/v1
name: cli-r2-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.batch}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body:
          name: other
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fill
      call: ThingService/Create
      body:
          name: filler
          kind: KIND_A
      expect:
          - path: error.code
            equals: INTERNAL
    - id: blank
      call: ThingService/Create
      body:
          name: ""
          kind: KIND_A
      expect:
          - path: transport.code
            equals: invalid_argument
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`

func round2Workspace(t *testing.T) *string {
	t.Helper()
	code := "OK"
	srv := newRound2Backend(&code)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-r2-flow.yaml", round2Chain)
	if err := runRun(context.Background(), []string{"cli-r2-flow", "-quiet", "-var", "batch=T1"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	return &code
}

type lifecycleBackend struct {
	mu        sync.Mutex
	cancelBug bool
	note      int
}

func (b *lifecycleBackend) serve() *httptest.Server {
	states := map[string]string{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)
		op, _ := body["name"].(string)
		if meta, _ := body["meta"].(map[string]any); meta != nil {
			id, _ = meta["trace_id"].(string)
		}
		w.Header().Set("Content-Type", "application/json")
		out := map[string]any{"error": map[string]any{"code": "OK"}}
		switch {
		case r.URL.Path == "/shrt.test.v1.ThingService/Fetch":
		case r.URL.Path != "/shrt.test.v1.ThingService/Create":
			w.WriteHeader(404)
			return
		case id == "":
			next++
			id = "thing-" + itoa(next)
			states[id] = "PENDING"
		case op == "confirm":
			states[id] = "CONFIRMED"
			out["total"] = b.note
		case op == "cancel" && !(b.cancelBug && states[id] == "CONFIRMED"):
			states[id] = "CANCELLED"
		}
		out["id"], out["name"] = id, states[id]
		_ = json.NewEncoder(w).Encode(out)
	}))
}

const lifecycleChain = `apiVersion: shrt/v1
name: life
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget
      expect:
          - path: error.code
            equals: OK
    - id: confirm
      call: ThingService/Create
      body:
          name: confirm
          meta:
              trace_id: ${make.id}
      expect:
          - path: name
            equals: CONFIRMED
    - id: fetch_confirmed
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: name
            equals: CONFIRMED
    - id: cancel
      call: ThingService/Create
      body:
          name: cancel
          meta:
              trace_id: ${make.id}
      expect:
          - path: name
            equals: CANCELLED
`

func lifecycleWorkspace(t *testing.T) *lifecycleBackend {
	t.Helper()
	b := &lifecycleBackend{note: 1}
	srv := b.serve()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/life.yaml", lifecycleChain)
	return b
}

func stockBackend(fetchBias int) *httptest.Server {
	var mu sync.Mutex
	totals := map[string]int{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		n := 0
		switch q := body["qty"].(type) {
		case string:
			for _, ch := range q {
				n = n*10 + int(ch-'0')
			}
		case float64:
			n = int(q)
		}
		w.Header().Set("Content-Type", "application/json")
		ok := map[string]any{"code": "OK"}
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			id, lost := "", false
			if meta, _ := body["meta"].(map[string]any); meta != nil {
				id, _ = meta["trace_id"].(string)
				lost = meta["source"] == "lost"
			}
			if id == "" {
				next++
				id = "thing-" + itoa(next)
			}
			if lost {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "id": id, "total": totals[id] + n})
				return
			}
			totals[id] += n
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "id": id, "total": totals[id]})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "id": id, "name": "widget", "total": totals[id] + fetchBias})
		default:
			w.WriteHeader(404)
		}
	}))
}

const stockChain = `apiVersion: shrt/v1
name: stock
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget
          qty: 6
      expect:
          - path: error.code
            equals: OK
    - id: stray_add
      call: ThingService/Create
      body:
          qty: 9
          meta:
              trace_id: ${make.id}
      expect:
          - path: error.code
            equals: DENIED
    - id: fetch_total
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: total
            equals: 6
    - id: fetch_name
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: name
            equals: gadget
`

func stockWorkspace(t *testing.T, fetchBias int) {
	t.Helper()
	srv := stockBackend(fetchBias)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/stock.yaml", stockChain)
	if err := runRun(context.Background(), []string{"stock", "-quiet", "-keep-going"}); err == nil {
		t.Fatalf("the chain must fail")
	}
}

const minimalScratchChain = `apiVersion: shrt/v1
name: min-stock
vars:
    tag: min
steps:
    - id: create_product
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: peek
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "3"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: get_after
      call: ProductService/GetProduct
      body:
        id_product: ${create_product.product.id_product}
      expect:
        - path: status.code
          equals: SUCCESS
`

func minimalScratch(t *testing.T) {
	t.Helper()
	chdirToFakeShop(t, newFakeShop())
	writeFile(t, ".shrt/scratch/min-stock.yaml", minimalScratchChain)
	if err := runRun(context.Background(), []string{".shrt/scratch/min-stock.yaml", "-quiet", "-var", "tag=src"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
