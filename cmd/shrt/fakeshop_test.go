package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

type fakeShop struct {
	mu sync.Mutex

	cancelConfirmedBug bool
	getProductFailN    int
	priceBug           bool
	skuEchoBug         bool

	next      int
	getCalls  int
	products  map[string]map[string]any
	stock     map[string]int64
	customers map[string]map[string]any
	orders    map[string]map[string]any
	states    map[string]string
	idem      map[string]string
	emails    map[string]bool
}

func newFakeShop() *fakeShop {
	return &fakeShop{
		products: map[string]map[string]any{}, stock: map[string]int64{}, customers: map[string]map[string]any{},
		orders: map[string]map[string]any{}, states: map[string]string{}, idem: map[string]string{}, emails: map[string]bool{},
	}
}

func (s *fakeShop) id(prefix string) string {
	s.next++
	return fmt.Sprintf("%s-%04d", prefix, s.next)
}

func num64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

func ok() map[string]any { return map[string]any{"code": "SUCCESS"} }

func rejected(why string) map[string]any {
	return map[string]any{"code": "REJECTED", "message": why}
}

func (s *fakeShop) handle(path string, body map[string]any) (int, map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch path {
	case "/shop.customers.v1.CustomerService/CreateCustomer":
		email, _ := body["email"].(string)
		if s.emails[email] {
			return 200, map[string]any{"status": rejected("EmailTaken")}
		}
		s.emails[email] = true
		c := map[string]any{"id_customer": s.id("cus"), "email": email}
		s.customers[c["id_customer"].(string)] = c
		return 200, map[string]any{"status": ok(), "customer": c}
	case "/shop.catalog.v1.ProductService/CreateProduct":
		price := num64(body["price_minor"])
		if price <= 0 {
			return 200, map[string]any{"status": rejected("InvalidPrice")}
		}
		if s.priceBug && price >= 1000 {
			price -= price / 1000
		}
		p := map[string]any{"id_product": s.id("prd"), "sku": body["sku"], "price_minor": strconv.FormatInt(price, 10)}
		s.products[p["id_product"].(string)] = p
		return 200, map[string]any{"status": ok(), "product": p}
	case "/shop.catalog.v1.ProductService/GetProduct":
		s.getCalls++
		if s.getProductFailN > 0 && s.getCalls%s.getProductFailN == 0 {
			return 500, map[string]any{"code": "internal", "message": "boom"}
		}
		p, found := s.products[fmt.Sprint(body["id_product"])]
		if !found {
			return 200, map[string]any{"status": rejected("ProductNotFound")}
		}
		if s.skuEchoBug {
			wrong := map[string]any{}
			for k, v := range p {
				wrong[k] = v
			}
			wrong["sku"] = fmt.Sprintf("wrong-%v", p["sku"])
			return 200, map[string]any{"status": ok(), "product": wrong}
		}
		return 200, map[string]any{"status": ok(), "product": p}
	case "/shop.catalog.v1.StockService/AddStock":
		id := fmt.Sprint(body["id_product"])
		if _, found := s.products[id]; !found {
			return 200, map[string]any{"status": rejected("ProductNotFound")}
		}
		s.stock[id] += num64(body["qty"])
		return 200, map[string]any{"status": ok(), "qty_on_hand": strconv.FormatInt(s.stock[id], 10)}
	case "/shop.orders.v1.OrderService/CreateOrder":
		key := fmt.Sprint(body["idempotency_key"])
		if prev, seen := s.idem[key]; seen && key != "" {
			return 200, map[string]any{"status": ok(), "order": s.orders[prev]}
		}
		lines, _ := body["lines"].([]any)
		total := int64(0)
		for _, l := range lines {
			line, _ := l.(map[string]any)
			p := s.products[fmt.Sprint(line["id_product"])]
			if p == nil {
				return 200, map[string]any{"status": rejected("ProductNotFound")}
			}
			total += num64(line["qty"]) * num64(p["price_minor"])
		}
		o := map[string]any{"id_order": s.id("ord"), "id_customer": body["id_customer"], "lines": lines, "total_minor": strconv.FormatInt(total, 10)}
		s.orders[o["id_order"].(string)] = o
		s.states[o["id_order"].(string)] = "PENDING"
		s.idem[key] = o["id_order"].(string)
		return 200, map[string]any{"status": ok(), "order": o}
	case "/shop.orders.v1.OrderService/ConfirmOrder":
		id := fmt.Sprint(body["id_order"])
		o := s.orders[id]
		if o == nil {
			return 200, map[string]any{"status": rejected("OrderNotFound")}
		}
		lines, _ := o["lines"].([]any)
		for _, l := range lines {
			line, _ := l.(map[string]any)
			if s.stock[fmt.Sprint(line["id_product"])] < num64(line["qty"]) {
				return 200, map[string]any{"status": rejected("InsufficientStock")}
			}
		}
		for _, l := range lines {
			line, _ := l.(map[string]any)
			s.stock[fmt.Sprint(line["id_product"])] -= num64(line["qty"])
		}
		s.states[id] = "CONFIRMED"
		return 200, map[string]any{"status": ok(), "order": o}
	case "/shop.orders.v1.OrderService/CancelOrder":
		id := fmt.Sprint(body["id_order"])
		o := s.orders[id]
		if o == nil {
			return 200, map[string]any{"status": rejected("OrderNotFound")}
		}
		if s.states[id] == "CONFIRMED" && s.cancelConfirmedBug {
			return 200, map[string]any{"status": rejected("StillConfirmed"), "order": o}
		}
		s.states[id] = "CANCELLED"
		return 200, map[string]any{"status": ok(), "order": o}
	case "/shop.orders.v1.OrderService/FetchOrder":
		o := s.orders[fmt.Sprint(body["id_order"])]
		if o == nil {
			return 200, map[string]any{"status": rejected("OrderNotFound")}
		}
		return 200, map[string]any{"status": ok(), "order": o}
	}
	return 404, map[string]any{"code": "unimplemented", "message": path}
}

func (s *fakeShop) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		code, out := s.handle(r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func chdirToFakeShop(t *testing.T, shop *fakeShop) {
	t.Helper()
	srv := shop.server()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: `+srv.URL+`
descriptor:
    file: .shrt/descriptor.binpb
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
conventions:
    envelope_path: status.code
    envelope_ok: SUCCESS
`)
	removeFile(t, ".shrt/chains/cli-thing-flow.yaml")
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

const cancelConfirmedChain = `apiVersion: shrt/v1
name: probe-orders
vars:
    tag: probe
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
    - id: create_product_2
      call: ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-b
        price_minor: "200"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: add_stock
      call: StockService/AddStock
      body:
        id_product: ${create_product.product.id_product}
        qty: "50"
      expect:
        - path: status.code
          equals: SUCCESS
    - id: create_order_single
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "1"
        idempotency_key: single-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: retry_single
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "1"
        idempotency_key: ${steps.create_order_single.request.idempotency_key}
      expect:
        - path: order.id_order
          equals: ${create_order_single.order.id_order}
    - id: create_order_three
      call: OrderService/CreateOrder
      body:
        id_customer: ${create_customer.customer.id_customer}
        lines:
            - id_product: ${create_product.product.id_product}
              qty: "1"
            - id_product: ${create_product_2.product.id_product}
              qty: "2"
        idempotency_key: three-${vars.tag}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: cancel_pending
      call: OrderService/CancelOrder
      body:
        id_order: ${create_order_three.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: confirm_single
      call: OrderService/ConfirmOrder
      body:
        id_order: ${create_order_single.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
    - id: cancel_confirmed
      call: OrderService/CancelOrder
      body:
        id_order: ${create_order_single.order.id_order}
      expect:
        - path: status.code
          equals: SUCCESS
`
