package chain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	sevE = chain.SeverityError
	sevW = chain.SeverityWarn
)

type hit struct {
	sev, kind, step string
	has, lacks      []string
}

func h(sev, kind string, has ...string) hit { return hit{sev: sev, kind: kind, has: has} }

func (x hit) matches(i chain.Issue) bool {
	if (x.sev != "" && i.Severity != x.sev) || (x.kind != "" && i.Kind != x.kind) || (x.step != "" && i.Step != x.step) {
		return false
	}
	for _, s := range x.has {
		if !strings.Contains(i.Message, s) {
			return false
		}
	}
	for _, s := range x.lacks {
		if strings.Contains(i.Message, s) {
			return false
		}
	}
	return true
}

type lintCase struct {
	name   string
	cat    *catalog.Catalog
	opts   chain.LintOptions
	strict bool
	c      *chain.Chain
	pick   hit
	want   []hit
	n      int
}

func thing(steps ...*chain.Step) *chain.Chain {
	for _, st := range steps {
		if len(st.Expect) == 0 {
			st.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
		}
	}
	return &chain.Chain{Name: "t", Steps: steps}
}

func one(s *chain.Step) *chain.Chain { return &chain.Chain{Name: "t", Steps: []*chain.Step{s}} }

func yamlChain(t *testing.T, raw string) *chain.Chain {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func runLintCases(t *testing.T, cases []lintCase) {
	t.Helper()
	for _, tc := range cases {
		if err := tc.c.Normalize(); err != nil {
			t.Fatalf("%s: normalize: %v", tc.name, err)
		}
		cat := tc.cat
		if cat == nil {
			cat = catalogtest.New()
		}
		issues := chain.LintWith(tc.c, cat, tc.opts)
		if tc.strict {
			issues = chain.Promote(issues, chain.IsAssertionQualityIssue)
		}
		picked := []chain.Issue{}
		for _, i := range issues {
			if tc.pick.matches(i) {
				picked = append(picked, i)
			}
		}
		if len(tc.want) == 0 && tc.n == 0 && len(picked) != 0 {
			t.Errorf("%s: want none, got %+v", tc.name, picked)
		}
		if tc.n > 0 && len(picked) != tc.n {
			t.Errorf("%s: want %d issue(s), got %+v", tc.name, tc.n, picked)
		}
		for _, w := range tc.want {
			found := false
			for _, i := range picked {
				found = found || w.matches(i)
			}
			if !found {
				t.Errorf("%s: want %+v among %+v", tc.name, w, picked)
			}
		}
	}
}

func create(body map[string]any, export map[string]string) *chain.Step {
	return &chain.Step{ID: "create", Call: "ThingService/Create", Body: body, Export: export}
}

func fetch(body map[string]any, expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: body, Expect: expect}
}

func widget() map[string]any {
	return map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"}
}

var okCode = chain.Expectation{Path: "error.code", Equals: "OK"}

func expecting(e chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "t", Vars: map[string]any{"wanted": "abc"}, Steps: []*chain.Step{{
		ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "widget"},
		Expect: []chain.Expectation{e}}}}
}

func fetching(extra ...chain.Expectation) *chain.Chain {
	return one(fetch(map[string]any{"id": "thing-1"}, append([]chain.Expectation{okCode}, extra...)...))
}

func twoStep(ref string, extra ...chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"}, Expect: []chain.Expectation{okCode}},
		fetch(map[string]any{"id": ref}, append([]chain.Expectation{okCode}, extra...)...),
	}}
}

func crossStep(ref string, export map[string]string) *chain.Chain {
	return thing(create(widget(), export), fetch(map[string]any{"id": ref}))
}

func absentRead(producer ...chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "w"}, Expect: producer},
		{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "id", Equals: "${steps.create.response.id}"}}},
	}}
}

func stockCheck(want any) *chain.Chain {
	ok := chain.Expectation{Path: "status.code", Equals: "SUCCESS"}
	return &chain.Chain{Name: "arith", Vars: map[string]any{"n": "5"}, Steps: []*chain.Step{
		{ID: "a", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "p", "qty": "2"}, Expect: []chain.Expectation{ok}},
		{ID: "b", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "p", "qty": "3"},
			Expect: []chain.Expectation{ok, {Path: "qty_on_hand", Equals: want}}},
	}}
}

func headered(skip bool, headers map[string]string) *chain.Chain {
	s := create(map[string]any{"name": "widget", "kind": "KIND_A"}, nil)
	s.SkipAuth, s.Headers = skip, headers
	return thing(s)
}

func covered(profile, header string) chain.LintOptions {
	return chain.LintOptions{AuthHeader: func(*chain.Step) (string, string, bool) { return profile, header, profile != "" }}
}

func batchPreview(expect ...chain.Expectation) *chain.Chain {
	return one(&chain.Step{ID: "preview", Call: "BatchService/Preview", Body: map[string]any{"lines": []any{"a"}}, Expect: expect})
}

func verdictStep(e chain.Expectation) *chain.Chain {
	return one(&chain.Step{ID: "fetch", Call: "ThingService/Fetch", SkipAuth: true, Expect: []chain.Expectation{e, {Path: "id_deal", NotEmpty: true}}})
}

func probe(expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "thing-1"}, Expect: expect}
}

func invalidToken(export map[string]string) *chain.Chain {
	s := probe(chain.Expectation{Path: "transport.code", Equals: "unauthenticated"})
	s.SkipAuth, s.Auth, s.Export = false, "invalid", export
	return thing(s)
}

func prefixed(prefix string, expect ...chain.Expectation) *chain.Chain {
	return one(&chain.Step{ID: "list", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id_prefix": prefix},
		Expect: append([]chain.Expectation{okCode}, expect...)})
}

func unscoped(prefix string, e chain.Expectation) *chain.Chain {
	return one(&chain.Step{ID: "list_products", Call: "shop.catalog.v1.ProductService/ListProducts", SkipAuth: true,
		Body: map[string]any{"sku_prefix": prefix}, Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, e}})
}

func listWidgets(stepUnordered, chainUnordered []string) *chain.Chain {
	return &chain.Chain{Name: "t", Unordered: chainUnordered, Steps: []*chain.Step{{
		ID: "list", Call: "WidgetService/ListWidgets", Body: map[string]any{"id": "x"}, Unordered: stepUnordered,
		Expect: []chain.Expectation{{Path: "result.code", Equals: "OK"}}}}}
}

func tagChain(sku string) *chain.Chain {
	return &chain.Chain{Name: "t", Vars: map[string]any{"tag": "t1"}, Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": sku, "name": "n", "price_minor": "250"},
			Expect: []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}, {Path: "product.price_minor", Equals: 250}}}}}
}

func addStockFrom(qty string) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	return &chain.Chain{Name: "t", Vars: map[string]any{"qty": "5"}, Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "s", "name": "n", "price_minor": "250"},
			Expect: ok, Export: map[string]string{"pid": "product.id_product", "price": "product.price_minor"}},
		{ID: "add", Call: "shop.catalog.v1.StockService/AddStock", Body: map[string]any{"id_product": "${cp.product.id_product}", "qty": qty}, Expect: ok},
	}}
}

func deprecatedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(catalogtest.Descriptor(), fds); err != nil {
		t.Fatal(err)
	}
	f := fds.File[0]
	for _, svc := range f.Service {
		for _, m := range svc.Method {
			if svc.GetName() == "ThingService" && m.GetName() == "Fetch" {
				m.Options = &descriptorpb.MethodOptions{Deprecated: proto.Bool(true)}
			}
		}
	}
	for _, m := range f.MessageType {
		for _, fd := range m.Field {
			if (m.GetName() == "FetchResponse" && fd.GetName() == "name") || (m.GetName() == "CreateRequest" && fd.GetName() == "kind") {
				fd.Options = &descriptorpb.FieldOptions{Deprecated: proto.Bool(true)}
			}
		}
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

const orderEchoYAML = `apiVersion: shrt/v1
name: echo
vars:
    tag: t
steps:
    - id: create_a
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "Anchor", price_minor: "1"}
    - id: create_b
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "Bolt", price_minor: "2"}
    - id: customer
      call: CustomerService/CreateCustomer
      body: {email: "c-${vars.tag}@example.test"}
    - id: order
      call: OrderService/CreateOrder
      body:
        id_customer: ${customer.customer.id_customer}
        lines:
          - id_product: ${create_a.product.id_product}
            qty: "2"
          - id_product: ${SECOND.product.id_product}
            qty: "3"
      expect:
        - path: order.lines.0.id_product
          equals: ${create_a.product.id_product}
        - path: order.lines.1.id_product
          equals: ${create_b.product.id_product}
    - id: fetch
      call: OrderService/FetchOrder
      body:
        id_order: ${order.order.id_order}
      expect:
        - path: order.lines.0.id_product
          equals: ${create_a.product.id_product}
        - path: order.lines.1.id_product
          equals: ${create_b.product.id_product}
`

const listOrderYAML = `apiVersion: shrt/v1
name: order
vars:
    tag: t
steps:
    - id: create_b
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "Bolt", price_minor: "1999"}
    - id: create_a
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "Anchor", price_minor: "1"}
    - id: list
      call: ProductService/ListProducts
      body: {sku_prefix: "cp-${vars.tag}-"}
      expect:
        - path: products.0.sku
          equals: ${steps.create_a.request.sku}
        - path: products.1.id_product
          equals: ${create_b.product.id_product}
`

const oneKeyOrderYAML = `apiVersion: shrt/v1
name: order3
vars:
    tag: t
steps:
    - id: create_1
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-a", name: "Beta", price_minor: "300"}
    - id: create_2
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-c", name: "Alpha", price_minor: "100"}
    - id: create_3
      call: ProductService/CreateProduct
      body: {sku: "cp-${vars.tag}-b", name: "Gamma", price_minor: "200"}
    - id: list
      call: ProductService/ListProducts
      body: {sku_prefix: "cp-${vars.tag}-"}
      expect:
        - path: products.0.id_product
          equals: ${create_1.product.id_product}
        - path: products.1.id_product
          equals: ${create_3.product.id_product}
        - path: products.2.id_product
          equals: ${create_2.product.id_product}
`

func TestLint(t *testing.T) {
	shop, rich, hints := catalogtest.Shop(), catalogtest.Rich(), chain.LintOptions{Hints: true}
	authEnv := chain.LintOptions{
		AuthProfiles: []string{"default", "clerk"},
		AuthHeader: func(s *chain.Step) (string, string, bool) {
			if s.Auth != "" {
				return s.Auth, "Authorization", true
			}
			return "default", "Authorization", true
		},
		AuthEnv: func(profile string) []string {
			return map[string][]string{"default": {"API_PASSWORD", "API_USER"}, "clerk": {"CLERK_PASSWORD", "CLERK_USER"}}[profile]
		},
		Env: func(name string) (string, bool) { return "clerk", name == "CLERK_USER" },
	}
	clerk := func() *chain.Chain {
		s := create(map[string]any{"name": "a", "kind": "KIND_A"}, nil)
		s.Auth = "clerk"
		return thing(s)
	}
	envChain := func() *chain.Chain {
		return thing(create(map[string]any{"name": "${env.WIDGET_OWNER}", "kind": "KIND_A"}, nil))
	}
	inputs := func(vars map[string]any, name string) *chain.Chain {
		c := thing(create(map[string]any{"name": name, "kind": "KIND_A", "idempotency_key": "${uuid}"}, nil))
		c.Vars = vars
		return c
	}
	silent := inputs(nil, "widget")
	silent.Steps[0].Expect = nil
	no := false
	cases := []lintCase{
		{name: "a valid chain", c: thing(create(widget(), map[string]string{"thing_id": "id"}), fetch(map[string]any{"id": "${create.id}"}))},
		{name: "a bare export reference", c: thing(create(widget(), map[string]string{"thing_id": "id"}), fetch(map[string]any{"id": "${thing_id}"}))},
		{name: "a bare reference to nothing declared", c: thing(create(widget(), map[string]string{"thing_id": "id"}), fetch(map[string]any{"id": "${thing_i}"})),
			pick: h("", "", "thing_i"), want: []hit{h(sevE, "")}},
		{name: "an unknown rpc", c: thing(&chain.Step{ID: "x", Call: "ThingService/Vanish"}), want: []hit{h(sevE, "")}, n: 1},
		{name: "an unknown field behind a reference", c: thing(create(map[string]any{"nope": "${vars.x}"}, nil)), pick: h(sevE, "", "nope"), want: []hit{{}}},
		{name: "a forward step reference", c: thing(fetch(map[string]any{"id": "${create.id}"}), create(map[string]any{"name": "w"}, nil)),
			want: []hit{h("", "", "does not run before")}, n: 1},
		{name: "a forward reference in a header", c: thing(&chain.Step{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "x"},
			Headers: map[string]string{"X-Ref": "${create.id}"}}, create(map[string]any{"name": "w"}, nil)),
			want: []hit{h("", "", "does not run before")}, n: 1},
		{name: "an export of a field the response lacks", c: thing(create(map[string]any{"name": "w", "kind": "KIND_A"}, map[string]string{"x": "no_such_field"})),
			want: []hit{h(sevE, chain.KindBadExport)}, n: 1},
		{name: "a step that does not exist", c: thing(create(map[string]any{"name": "w"}, nil), fetch(map[string]any{"id": "${create_custmer.id}"})),
			n: 1, want: []hit{{has: []string{"does not exist"}, lacks: []string{"does not run before"}}}},
		{name: "a misspelt step id is suggested", c: thing(&chain.Step{ID: "create_customer", Call: "ThingService/Create", Body: map[string]any{"name": "w"}},
			fetch(map[string]any{"id": "${create_custmer.id}"})), want: []hit{h("", "", `did you mean "create_customer"`)}},
		{name: "an export read before its step", c: thing(fetch(map[string]any{"id": "${pid}"}), create(map[string]any{"name": "w"}, map[string]string{"pid": "id"})),
			pick: h("", "", "${pid}"), want: []hit{h("", "", "exported by create at step 2, which runs later")}},

		{name: "two rules on one expectation", c: thing(fetch(map[string]any{"id": "x"}, chain.Expectation{Path: "name", Equals: "widget", NotEmpty: true})),
			n: 1, want: []hit{h(sevE, "", "not_empty", "equals")}},
		{name: "an expectation with no rule", c: thing(fetch(map[string]any{"id": "x"}, chain.Expectation{Path: "error.code"})), n: 1, want: []hit{{}}},
		{name: "two rules split apart", c: thing(fetch(map[string]any{"id": "x"}, okCode, chain.Expectation{Path: "name", NotEmpty: true}))},
		{name: "a reference inside a var value", c: &chain.Chain{Name: "t", Vars: map[string]any{"k": "${uuid}"},
			Steps: thing(fetch(map[string]any{"id": "${vars.k}"})).Steps}, n: 1, want: []hit{h(sevE, "")}},
		{name: "a plain var value", c: &chain.Chain{Name: "t", Vars: map[string]any{"tag": "TPO", "qty": 40000},
			Steps: thing(fetch(map[string]any{"id": "${vars.tag}"})).Steps}},

		{name: "a path through a list without an index", cat: catalogtest.Batch(), c: batchPreview(chain.Expectation{Path: "results.error.code", Equals: "x"}),
			pick: h("", chain.KindUnreachable), want: []hit{h(sevE, "", "results.0.error.code")}, n: 1},
		{name: "an indexed path through a list", cat: catalogtest.Batch(), c: batchPreview(chain.Expectation{Path: "results.0.error.code", Equals: "x"}), pick: h("", chain.KindUnreachable)},
		{name: "the list itself", cat: catalogtest.Batch(), c: batchPreview(chain.Expectation{Path: "results", Equals: "x"}), pick: h("", chain.KindUnreachable)},
		{name: "exists false through a list without an index", cat: catalogtest.Batch(), c: batchPreview(chain.Expectation{Path: "results.0.error.code", Equals: "OK"},
			chain.Expectation{Path: "results.error.message", Exists: &no}), pick: h("", "", "results.error.message"), want: []hit{h(sevE, chain.KindUnfailable)}},

		{name: "a reference to what its step expects refused", c: absentRead(chain.Expectation{Path: "transport.code", Equals: "permission_denied"}),
			pick: h("", "", "resolves to nothing"), n: 2, want: []hit{h("", "", "${create.id} reads what create expects to be refused")}},
		{name: "a reference to a field its step expects absent", c: absentRead(chain.Expectation{Path: "transport.code", Equals: "ok"}, chain.Expectation{Path: "id", Exists: &no}),
			pick: h("", "", "resolves to nothing"), n: 2, want: []hit{h("", "", "expects absent (id exists: false)")}},
		{name: "a reference to a field its step answers", c: absentRead(chain.Expectation{Path: "id", NotEmpty: true}), pick: h("", "", "resolves to nothing")},
		{name: "a refusal that still asserts the read field", c: absentRead(chain.Expectation{Path: "transport.code", Equals: "permission_denied"}, chain.Expectation{Path: "id", Equals: "x"}),
			pick: h("", "", "resolves to nothing")},

		{name: "reference arithmetic", c: twoStep("${create.id}", chain.Expectation{Path: "name", Equals: "${create.name + 3}"}),
			pick: h("", "", "create.name + 3"), want: []hit{{has: []string{chain.NoArithmetic}, lacks: []string{"not a field"}}}},
		{name: "var arithmetic", c: &chain.Chain{Name: "t", Vars: map[string]any{"stock": "3"}, Steps: []*chain.Step{create(map[string]any{"name": "${vars.stock+5}"}, nil)}},
			pick: h("", "", "stock+5"), want: []hit{h(sevE, "", "${vars.stock+5} "+chain.NoArithmetic)}},
		{name: "interpolated arithmetic under -strict", cat: shop, strict: true, c: stockCheck("${a.qty_on_hand}*2"), pick: h("", chain.KindArithmetic), want: []hit{h(sevE, "")}},

		{name: "skip_auth with a hand-written Authorization", c: headered(true, map[string]string{"Authorization": "Bearer ${vars.token}"}),
			pick: h("", "", "uthorization"), n: 1, want: []hit{h(sevE, "", "profile")}},
		{name: "skip_auth with a lowercase authorization", c: headered(true, map[string]string{"authorization": "Bearer x"}),
			pick: h("", "", "uthorization"), n: 1, want: []hit{h(sevE, "")}},
		{name: "a hand-written Authorization is overwritten", c: headered(false, map[string]string{"Authorization": "Bearer x"}),
			pick: h("", "", "uthorization"), n: 1, want: []hit{h(sevW, "", "overwrites")}},
		{name: "an ordinary header", c: headered(false, map[string]string{"X-Request-Id": "${uuid}"}), pick: h("", "", "uthorization")},
		{name: "a profile covers the call", opts: covered("default", "Authorization"), c: headered(false, map[string]string{"Authorization": "Bearer ${vars.other_token}"}),
			pick: h("", "", "eader"), n: 1, want: []hit{h(sevE, "", `"default"`)}},
		{name: "the covering profile's own header", opts: covered("partner", "X-Api-Key"), c: headered(false, map[string]string{"x-api-key": "k"}),
			pick: h("", "", "eader"), n: 1, want: []hit{h(sevE, "")}},
		{name: "a header the covering profile leaves alone", opts: covered("partner", "X-Api-Key"), c: headered(false, map[string]string{"Authorization": "Bearer x"}), pick: h("", "", "eader")},
		{name: "no profile covers the call", opts: covered("", ""), c: headered(false, map[string]string{"Authorization": "Bearer x"}), pick: h("", "", "eader")},

		{name: "an unset env var a used profile's login reads", opts: authEnv, c: clerk(), pick: h("", "", "CLERK_PASSWORD"), want: []hit{h(sevW, "", `"clerk"`)}},
		{name: "an unused profile's env var", opts: authEnv, c: clerk(), pick: h("", "", "API_PASSWORD")},
		{name: "a set env var", opts: authEnv, c: clerk(), pick: h("", "", "CLERK_USER")},
		{name: "an exported env var", opts: chain.LintOptions{Env: func(k string) (string, bool) { return "owner", k == "WIDGET_OWNER" }}, c: envChain(), pick: h("", "", "WIDGET_OWNER")},
		{name: "an env var not exported", opts: chain.LintOptions{Env: func(string) (string, bool) { return "", false }}, c: envChain(),
			pick: h("", "", "WIDGET_OWNER"), n: 1, want: []hit{h(sevW, "", "not exported")}},
		{name: "no environment given", c: envChain(), pick: h("", "", "exported")},
		{name: "an undeclared var is no error", c: inputs(nil, "${vars.business_date}"), pick: h(sevE, "")},
		{name: "an undeclared var is warned", c: inputs(nil, "${vars.business_date}"), pick: h("", "", "business_date"), want: []hit{h("", "", "-var business_date=")}},
		{name: "a declared var", c: inputs(map[string]any{"business_date": "2026-01-01"}, "${vars.business_date}"), pick: h("", "", "business_date")},
		{name: "a step asserting nothing", c: silent, pick: h("", "", "asserts nothing at all"), want: []hit{h(sevW, "")}},
		{name: "a setup step is no error", c: silent, pick: h(sevE, "")},
		{name: "a step that asserts", c: inputs(nil, "widget"), pick: h("", "", "asserts nothing at all")},

		{name: "a producer path the response lacks", c: crossStep("${create.thing.id}", nil), pick: h("", "", "cannot produce it"),
			n: 1, want: []hit{h(sevE, "", "thing.id", "exports.")}},
		{name: "a misspelt producer field", c: crossStep("${create.nmae}", nil), pick: h("", "", "cannot produce it"),
			n: 1, want: []hit{{has: []string{`(did you mean "name"?)`}, lacks: []string{"would kill the run"}}}},
		{name: "an expect path the response lacks", c: fetching(chain.Expectation{Path: "no_such_field", Equals: "x"}),
			pick: h("", "", "can never be present"), n: 1, want: []hit{{sev: sevE, has: []string{"no_such_field"}, lacks: []string{"exists: false"}}}},
		{name: "real response paths", c: fetching(chain.Expectation{Path: "name", Equals: "widget"}, chain.Expectation{Path: "id", NotEmpty: true}, chain.Expectation{Path: "name", Exists: &no}),
			pick: h("", "", "can never be present")},
		{name: "a key inside a proto map", cat: rich, c: one(&chain.Step{ID: "place", Call: "OrderService/PlaceOrder",
			Expect: []chain.Expectation{{Path: "labels.anything_at_all", Equals: "x"}, {Path: "id_order", NotEmpty: true}}}), pick: h("", "", "can never be present")},
		{name: "exists false on a misspelt field", c: one(&chain.Step{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "kind": "KIND_A"},
			Expect: []chain.Expectation{okCode, {Path: "idd", Exists: &no}}}), pick: h("", "", `"idd"`), want: []hit{h(sevE, chain.KindUnfailable)}},
		{name: "a renamed leaf names its siblings", c: fetching(chain.Expectation{Path: "error.kode", Equals: "OK"}, chain.Expectation{Path: "no_such.deep", Equals: "x"}),
			pick: h("", "", "can never be present"), n: 2, want: []hit{
				h("", "", "error.kode", "if it was renamed in the proto, assert the new name: error declares code"),
				{has: []string{"no_such.deep"}, lacks: []string{"renamed"}}}},
		{name: "a folded field path", c: fetching(chain.Expectation{Path: "createdat", NotEmpty: true}), pick: h("", chain.KindUnreachable)},
		{name: "a folded exists false", c: fetching(chain.Expectation{Path: "CreatedAt", Exists: &no}), pick: h("", chain.KindUnfailable)},
		{name: "a path no folding resolves", c: fetching(chain.Expectation{Path: "no_such_field", NotEmpty: true}), pick: h("", chain.KindUnreachable), want: []hit{{}}},

		{name: "a reference in an equals value", c: expecting(chain.Expectation{Path: "id", Equals: "${vars.wanted}"}), pick: h(sevE, "")},
		{name: "a reference in a not_equal value", c: expecting(chain.Expectation{Path: "id", NotEqual: "${vars.wanted}"}), pick: h(sevE, "")},
		{name: "a reference in a contains value", c: expecting(chain.Expectation{Path: "id", Contains: "${vars.wanted}"}), pick: h(sevE, "")},
		{name: "a plain expectation", c: expecting(okCode), pick: h(sevE, "")},
		{name: "a dollar that is no reference", c: expecting(chain.Expectation{Path: "id", Equals: "costs $5 today"}), pick: h(sevE, "")},
		{name: "a reference in an expect path", c: expecting(chain.Expectation{Path: "${vars.wanted}", NotEmpty: true}), pick: h(sevE, ""), want: []hit{h("", "", "${", "path")}},
		{name: "an expectation reading a later step", c: &chain.Chain{Name: "t", Steps: []*chain.Step{
			{ID: "first", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "widget"},
				Expect: []chain.Expectation{{Path: "id", Equals: "${steps.second.response.id}"}}},
			{ID: "second", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "widget"}}}},
			pick: h(sevE, ""), want: []hit{h("", "", "second", "does not run before")}},
		{name: "an expectation reading its own request", c: expecting(chain.Expectation{Path: "id", Equals: "${steps.create.request.name}"}), pick: h(sevE, "")},
		{name: "an expectation reading its own request, short form", c: expecting(chain.Expectation{Path: "id", Equals: "${create.request.name}"}), pick: h(sevE, "")},
		{name: "a body reading its own request", c: func() *chain.Chain {
			c := expecting(chain.Expectation{Path: "id", NotEmpty: true})
			c.Steps[0].Body["kind"] = "${steps.create.request.name}"
			return c
		}(), pick: h(sevE, ""), n: 1, want: []hit{h("", "", "does not run before")}},

		{name: "an export written by two steps, -strict", strict: true, c: thing(
			&chain.Step{ID: "first", Call: "ThingService/Create", Body: map[string]any{"name": "a", "kind": "KIND_A"}, Export: map[string]string{"tid": "id"}},
			&chain.Step{ID: "second", Call: "ThingService/Create", Body: map[string]any{"name": "b", "kind": "KIND_A"}, Export: map[string]string{"tid": "id"}},
			fetch(map[string]any{"id": "${tid}"})), pick: hit{step: "second", kind: chain.KindExportOverwritten}, want: []hit{h(sevE, "", `"first"`)}},
		{name: "an export named like a step", c: thing(create(map[string]any{"name": "a", "kind": "KIND_A"}, map[string]string{"fetch": "id"}), fetch(map[string]any{"id": "${create.id}"})),
			pick: hit{step: "create", sev: sevE}, want: []hit{h("", "", `export "fetch"`, "step")}},

		{name: "a folded export and expect path", strict: true, c: func() *chain.Chain {
			c := fetching(chain.Expectation{Path: "Error.Code", Equals: "OK"})
			c.Steps[0].Export = map[string]string{"c": "createdat"}
			return c
		}(), pick: h("", chain.KindInexactPath), n: 2, want: []hit{h(sevW, "", `"created_at"`), h(sevW, "", `"error.code"`)}},
		{name: "exact paths", c: func() *chain.Chain {
			c := fetching(chain.Expectation{Path: "name", Equals: "widget"})
			c.Steps[0].Export = map[string]string{"c": "created_at"}
			return c
		}(), pick: h("", chain.KindInexactPath)},
		{name: "json names are exact", c: &chain.Chain{Name: "t", Steps: []*chain.Step{
			{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget", "idempotency_key": "k-1"}, Expect: []chain.Expectation{okCode}},
			{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${steps.create.request.idempotencyKey}"},
				Export: map[string]string{"when": "createdAt"}, Expect: []chain.Expectation{okCode, {Path: "createdAt", NotEmpty: true}}}}},
			pick: h("", chain.KindInexactPath)},
		{name: "a name that is neither proto nor json name", c: twoStep("${steps.create.request.IdempotencyKey}"), pick: hit{kind: chain.KindInexactPath, step: "fetch"}, want: []hit{{}}},

		{name: "an inert allow_fail, -strict", strict: true, c: one(&chain.Step{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true,
			Expect: []chain.Expectation{{Path: "error.code", Equals: "NOT_FOUND"}}}), pick: h("", chain.KindInertAllowFail), n: 1,
			want: []hit{h(sevE, "", "does nothing", "NO expect", "transport.code")}},
		{name: "allow_fail with no expect", c: one(&chain.Step{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true}), pick: h("", chain.KindInertAllowFail)},
		{name: "transport.code exists", c: one(probe(chain.Expectation{Path: "transport.code", Exists: new(true)})), pick: h("", chain.KindUnfailable), n: 1,
			want: []hit{{has: []string{"transport.code equals: unauthenticated"}, lacks: []string{"unauthenticated, so it passes"}}}},
		{name: "an envelope not_empty", c: one(probe(chain.Expectation{Path: "error.code", NotEmpty: true})), pick: h("", chain.KindUnfailable), n: 1, want: []hit{h(sevW, "", "error.code equals: OK")}},
		{name: "error.code exists", c: one(probe(chain.Expectation{Path: "error.code", Exists: new(true)})), pick: h(sevW, chain.KindUnfailable), n: 1},

		{name: "a literal idempotency key", c: one(&chain.Step{ID: "create", Call: "ThingService/Create", SkipAuth: true,
			Body: map[string]any{"name": "w-${vars.tag}", "idempotency_key": "fixed-key-ao-1"}, Expect: []chain.Expectation{okCode}}),
			pick: h("", chain.KindLiteralIdempotency), n: 1, want: []hit{h(sevW, "", "idempotency_key", "fixed-key-ao-1", "${uuid}")}},
		{name: "a literal idempotency header", c: one(&chain.Step{ID: "create", Call: "ThingService/Create", SkipAuth: true,
			Headers: map[string]string{"Idempotency-Key": "k-1"}, Body: map[string]any{"name": "w"}, Expect: []chain.Expectation{okCode}}),
			pick: h("", chain.KindLiteralIdempotency), n: 1, want: []hit{h("", "", "Idempotency-Key")}},

		{name: "a misspelt response field in body and expect", c: twoStep("${create.idd}", chain.Expectation{Path: "id", Equals: "${create.idd}"}),
			pick: h("", chain.KindDeadRef, `"idd"`), n: 2, want: []hit{h(sevE, "", "expect"), {sev: sevE, lacks: []string{"expect"}}}},
		{name: "a correct response reference", c: twoStep("${create.id}", chain.Expectation{Path: "id", Equals: "${create.id}"}), pick: h("", chain.KindDeadRef)},
		{name: "a misspelt request field", c: twoStep("${steps.create.request.nmae}"), pick: h("", chain.KindDeadRef, `"nmae"`), want: []hit{h(sevE, "")}},
		{name: "a correct request field", c: twoStep("${steps.create.request.name}"), pick: h("", chain.KindDeadRef)},

		{name: "not_equal a scalar on a message, -strict", strict: true, c: verdictStep(chain.Expectation{Path: "error", NotEqual: "OK"}),
			pick: h("", chain.KindUnfailable, "not_equal"), n: 1, want: []hit{h(sevE, "")}},
		{name: "not_equal on the scalar verdict", c: verdictStep(chain.Expectation{Path: "error.code", NotEqual: "OK"}), pick: h("", chain.KindUnfailable, "not_equal")},
		{name: "not_equal internal on the verdict", c: verdictStep(chain.Expectation{Path: "error.code", NotEqual: "internal"}), pick: h("", chain.KindUnfailable, "not_equal")},
		{name: "not_equal an object on an object", c: verdictStep(chain.Expectation{Path: "error", NotEqual: map[string]any{"code": "OK"}}), pick: h("", chain.KindUnfailable, "not_equal")},
		{name: "not_equal empty on the verdict", c: verdictStep(chain.Expectation{Path: "error.code", NotEqual: ""}), pick: h("", chain.KindUnfailable, "not_equal"), n: 1, want: []hit{h(sevW, "")}},
		{name: "not_equal empty on a data field", c: verdictStep(chain.Expectation{Path: "id_deal", NotEqual: ""}), pick: h("", chain.KindUnfailable, "not_equal")},
		{name: "assertions that can fail", c: one(probe(okCode, chain.Expectation{Path: "error.code", NotEqual: "internal"}, chain.Expectation{Path: "error.code", Exists: &no},
			chain.Expectation{Path: "id_deal", Exists: new(true)}, chain.Expectation{Path: "id_deal", NotEmpty: true})), pick: h("", chain.KindUnfailable)},

		{name: "a response field on a step refused without a token", c: one(&chain.Step{ID: "fetch_without_token", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "x"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}, {Path: "created_at", Equals: "2026"}}}),
			pick: h("", chain.KindUnevaluableOnRefusal), n: 1, want: []hit{h(sevE, "", "never evaluated", "created_at")}},
		{name: "a response field on a step refused with a bad token", c: one(&chain.Step{ID: "fetch_with_bad_token", Call: "ThingService/Fetch", Auth: "invalid", Body: map[string]any{"id": "x"},
			Expect: []chain.Expectation{{Path: "transport.http_status", Equals: 401}, {Path: "name", NotEmpty: true}}}),
			pick: h("", chain.KindUnevaluableOnRefusal), n: 1, want: []hit{h(sevE, "", "never evaluated", "name")}},
		{name: "a response field on an answered step", c: one(fetch(map[string]any{"id": "x"}, chain.Expectation{Path: "transport.code", Equals: "ok"}, chain.Expectation{Path: "name", NotEmpty: true})),
			pick: h("", chain.KindUnevaluableOnRefusal)},
		{name: "transport paths need no descriptor field", c: thing(probe(chain.Expectation{Path: "transport.code", Equals: "unauthenticated"},
			chain.Expectation{Path: "transport.http_status", Equals: 401}, chain.Expectation{Path: "transport.message", Contains: "token"}))},
		{name: "an unknown transport path", c: thing(probe(chain.Expectation{Path: "transport.status", Equals: 401})), n: 1, want: []hit{h(sevE, "", chain.TransportFieldNames()...)}},
		{name: "transport.code not_empty", c: thing(probe(chain.Expectation{Path: "transport.code", NotEmpty: true})), pick: h("", chain.KindUnfailable), want: []hit{{}}},
		{name: "transport.http_status exists", c: thing(probe(chain.Expectation{Path: "transport.http_status", Exists: new(true)})), pick: h("", chain.KindUnfailable), want: []hit{{}}},
		{name: "a bare connect code", c: thing(probe(chain.Expectation{Path: "code", Equals: "unauthenticated"})), n: 1, want: []hit{h("", "", "transport.code")}},
		{name: "an export from an invalid token probe", c: invalidToken(map[string]string{"name": "name"}), pick: h(sevE, ""), n: 1, want: []hit{h("", "", "export")}},
		{name: "an invalid token probe", c: invalidToken(nil)},

		{name: "a client-streaming rpc", cat: rich, c: &chain.Chain{Name: "watch", Steps: []*chain.Step{
			{ID: "place_order", Call: "OrderService/PlaceOrder"},
			{ID: "upload_orders", Call: "OrderService/UploadOrders"},
			{ID: "watch_order", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "x"}, Expect: []chain.Expectation{{Path: "messages.0.state", NotEmpty: true}}}}},
			pick: h("", "", "streaming"), n: 1, want: []hit{{sev: sevE, step: "upload_orders", has: []string{"server-streaming rpcs only"}}}},
		{name: "a server-streaming step reads under messages", cat: rich, c: one(&chain.Step{ID: "watch_order", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "x"},
			Expect: []chain.Expectation{{Path: "messages.0.state", NotEmpty: true}, {Path: "state", NotEmpty: true}}}), pick: h(sevE, ""), n: 1, want: []hit{h("", "", `"state"`)}},

		{name: "an exact count on an unscoped list", cat: shop, c: unscoped("", chain.Expectation{Path: "products.3", Exists: &no}), pick: h("", chain.KindUnscopedCount),
			n: 1, want: []hit{h(sevW, "", "products.3 exists: false", "second run", "products.2 exists: true")}},
		{name: "a count on a list a var scopes", cat: shop, c: unscoped("sku-${vars.tag}-", chain.Expectation{Path: "products.3", Exists: &no}), pick: h("", chain.KindUnscopedCount)},
		{name: "a lower bound on an unscoped list", cat: shop, c: unscoped("", chain.Expectation{Path: "products.2", Exists: new(true)}), pick: h("", chain.KindUnscopedCount)},
		{name: "a prefix ending in a var", c: prefixed("sku-${vars.tag}", chain.Expectation{Path: "items.2", Exists: &no}), pick: h("", chain.KindUnterminatedPrefix),
			n: 1, want: []hit{h(sevW, "", "id_prefix", "sku-${vars.tag}", "tag=cp-1", "cp-10", "sku-${vars.tag}-")}},
		{name: "a terminated prefix", c: prefixed("sku-${vars.tag}-", chain.Expectation{Path: "items.2", Exists: &no}), pick: h("", chain.KindUnterminatedPrefix)},
		{name: "a fixed prefix", c: prefixed("sku-fixed", chain.Expectation{Path: "items.2", Exists: &no}), pick: h("", chain.KindUnterminatedPrefix)},
		{name: "a prefix ending in a slash", c: prefixed("${vars.tag}/", chain.Expectation{Path: "items.2", Exists: &no}), pick: h("", chain.KindUnterminatedPrefix)},
		{name: "a prefix on a step counting nothing", c: prefixed("sku-${vars.tag}"), pick: h("", chain.KindUnterminatedPrefix)},
		{name: "unordered on a repeated field", cat: catalogtest.Listing(), c: listWidgets([]string{"widgets"}, []string{"widgets"}), pick: h(sevE, "", "unordered")},

		{name: "a whole-value tag", cat: shop, c: tagChain("${vars.tag}"), pick: h("", "", "${vars.tag}", "sku-${vars.tag}"), want: []hit{h(sevW, "")}},
		{name: "a tag inside text", cat: shop, c: tagChain("sku-${vars.tag}"), pick: h("", "", "whole value")},
		{name: "id-shaped volatile patterns", c: &chain.Chain{Name: "t", Volatile: []string{"**.id_product", "**.created_at", "**.*_id"}, Steps: []*chain.Step{
			{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "w"}, Volatile: []string{"**.id"}, Expect: []chain.Expectation{{Path: "id", NotEmpty: true}}}}},
			pick: h("", "", "masks id-shaped paths"), n: 2, want: []hit{{step: "create"}, {has: []string{`"**.id_product", "**.*_id" masks`, "verify already pairs ids across runs"}, lacks: []string{"created_at"}}}},
		{name: "a path naming one word of a field", c: &chain.Chain{Name: "t", Steps: []*chain.Step{
			{ID: "login", Call: "AuthService/Login", SkipAuth: true, Body: map[string]any{"username": "u", "password": "p"}, Expect: []chain.Expectation{okCode}, Export: map[string]string{"tok": "token"}},
			{ID: "create", Call: "ThingService/Create", SkipAuth: true, Body: map[string]any{"name": "${login.token}"}, Expect: []chain.Expectation{okCode}}}},
			pick: h(sevE, "", `did you mean "access_token"?`), want: []hit{h("", "", `export "tok" reads "token"`), h("", "", `${login.token} reads "token"`)}},
		{name: "a path missing its parent", c: twoStep("${create.code}"), pick: h(sevE, ""), n: 1, want: []hit{h("", "", `(did you mean "error.code"?)`)}},

		{name: "comparison rules are one rule each", c: yamlChain(t, `apiVersion: shrt/v1
name: c
steps:
    - id: login
      call: AuthService/Login
      body: {username: u, password: p}
      expect:
        - path: expires_at
          within: {of: "${nowunix+3600}", by: 5}
        - path: expires_at
          between: ["${nowunix}", "${nowunix+7200}"]
        - path: expires_at
          gt: ${later.expires_at}
        - path: expires_at
          gte: 1
          lte: 2
    - id: later
      call: AuthService/Login
      body: {username: u, password: p}
`), pick: h(sevE, ""), want: []hit{h("", "", "carries 2 rules (gte, lte)"), h("", "", "${later.expires_at}"), {lacks: []string{"carries no rule"}}}},
		{name: "a rule whose value cannot fail", c: yamlChain(t, `apiVersion: shrt/v1
name: vacuous
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: x}
      expect:
          - {path: error.code, equals: OK}
          - {path: name, contains: ""}
          - {path: id, not_empty: false}
`), want: []hit{h("", "", `expect on "name" is contains: ""`, "can never fail"), h("", "", `expect on "id" is not_empty: false`, "can never fail")}},
		{name: "no rule claimed missing on a vacuous value", c: yamlChain(t, "apiVersion: shrt/v1\nname: v\nsteps:\n  - id: fetch\n    call: ThingService/Fetch\n    body: {id: x}\n    expect:\n      - {path: name, contains: \"\"}\n"),
			pick: h("", "", "carries no rule")},

		{name: "a deprecated rpc and fields", cat: deprecatedCatalog(t), c: thing(create(map[string]any{"name": "w", "kind": "KIND_A"}, nil),
			fetch(map[string]any{"id": "${create.id}"}, okCode, chain.Expectation{Path: "name", Equals: "w"})), pick: h("", chain.KindDeprecated), n: 3, want: []hit{
			{sev: sevW, step: "fetch", has: []string{"calls shrt.test.v1.ThingService/Fetch, which the proto marks deprecated"}},
			{sev: sevW, step: "create", has: []string{`body field "kind" is deprecated`}},
			{sev: sevW, step: "fetch", has: []string{`expect on "name" reads a field`}}}},

		{name: "every candidate key sorts alike", cat: shop, opts: hints, c: yamlChain(t, listOrderYAML), pick: h("", chain.KindIndistinctOrder), want: []hit{h("", "", "name, price_minor, sku")}},
		{name: "only one key matches the asserted order", cat: shop, opts: hints, c: yamlChain(t, oneKeyOrderYAML), pick: h("", chain.KindIndistinctOrder)},
		{name: "a list echoing the creating request", cat: shop, opts: hints, c: yamlChain(t, strings.ReplaceAll(orderEchoYAML, "SECOND", "create_b")), pick: h("", chain.KindIndistinctOrder)},
		{name: "a list mirroring no request", cat: shop, opts: hints, c: yamlChain(t, strings.ReplaceAll(orderEchoYAML, "SECOND", "create_a")), pick: h("", chain.KindIndistinctOrder),
			want: []hit{{step: "order"}, {step: "fetch"}}},
	}
	for _, want := range []string{"${a.qty_on_hand}+${steps.b.request.qty}", "${a.qty_on_hand} + 3", "${a.qty_on_hand}*2", "10-${a.qty_on_hand}"} {
		cases = append(cases, lintCase{name: "arithmetic " + want, cat: shop, c: stockCheck(want), pick: h("", chain.KindArithmetic), n: 1, want: []hit{h(sevW, "", "qty_on_hand")}})
	}
	for _, want := range []any{"${a.qty_on_hand}", "5", 5, "-5", "${vars.n}"} {
		cases = append(cases, lintCase{name: fmt.Sprintf("no arithmetic %v", want), cat: shop, c: stockCheck(want), pick: h("", chain.KindArithmetic)})
	}
	for _, ref := range []string{"${create.id}", "${steps.create.id}", "${steps.create.response.id}", "${steps.create.request.name}", "${exports.thing_id}", "${thing_id}"} {
		cases = append(cases, lintCase{name: "resolvable " + ref, c: crossStep(ref, map[string]string{"thing_id": "id"}), pick: h("", "", "cannot produce it")})
	}
	for _, ref := range []string{"${steps.create.response.id}", "${create.id}", "${create}"} {
		cases = append(cases, lintCase{name: "own response " + ref, c: expecting(chain.Expectation{Path: "id", Equals: ref}), pick: h(sevE, ""), n: 1,
			want: []hit{{has: []string{"own response"}, lacks: []string{"does not run before"}}}})
	}
	for ref, suggestion := range map[string]string{
		"${create.ID}":                   "",
		"${steps.create.response.ID}":    "write ${steps.create.response.id}",
		"${steps.create.ERROR.code}":     "write ${steps.create.response.error.code}",
		"${steps.create.request.NAME}":   "write ${steps.create.request.name}",
		"${create.Error.code}":           "write ${create.error.code}",
		"${create.response.Error.code}":  "write ${create.error.code}",
		"${steps.create.response.Error}": "write ${steps.create.response.error}",
	} {
		cases = append(cases, lintCase{name: "folded reference " + ref, c: twoStep(ref), pick: hit{kind: chain.KindInexactPath, step: "fetch"}, want: []hit{h(sevW, "", ref, suggestion)}})
	}
	for name, want := range map[string]string{"a-${ uuid }": "spaces", "a=${now+1.5}": "whole", "i=${uuid": "literal", "x-${uuid.id}": "takes no path", "${today-1.25}": "whole"} {
		cases = append(cases, lintCase{name: "reference syntax " + name, c: thing(create(map[string]any{"name": name, "kind": "KIND_A"}, nil)),
			pick: h("", chain.KindRefSyntax), n: 1, want: []hit{h(sevW, "", want)}})
	}
	for _, name := range []string{"a-${uuid}", "${now+3600}", "$${uuid}", "plain $ text", "{braces}"} {
		cases = append(cases, lintCase{name: "well formed " + name, c: thing(create(map[string]any{"name": name, "kind": "KIND_A"}, nil)), pick: h("", chain.KindRefSyntax)})
	}
	for _, e := range []chain.Expectation{{Path: "transport.code", Exists: new(true)}, {Path: "transport.http_status", Exists: new(true)}} {
		cases = append(cases, lintCase{name: "unfailable " + e.Path, c: thing(probe(e)), pick: h("", chain.KindUnfailable), want: []hit{{}}})
	}
	for _, bad := range []string{"widgts", "widgets.0", "**", "widgets.*", "result", "widgets.id"} {
		cases = append(cases,
			lintCase{name: "step unordered " + bad, cat: catalogtest.Listing(), c: listWidgets([]string{bad}, nil), pick: h(sevE, "", "unordered"), n: 1, want: []hit{h("", "", bad)}},
			lintCase{name: "chain unordered " + bad, cat: catalogtest.Listing(), c: listWidgets(nil, []string{bad}), pick: h(sevE, "", "unordered"), n: 1, want: []hit{h("", "", bad)}})
	}
	for _, ref := range []string{"${pid}", "${exports.pid}", "${cp.product.sku}", "${steps.cp.request.name}"} {
		cases = append(cases, lintCase{name: "a string into int64 " + ref, cat: shop, strict: true, c: addStockFrom(ref), pick: h("", "", ref, "int64"), want: []hit{h(sevW, "")}})
	}
	for _, ref := range []string{"${price}", "${cp.product.price_minor}", "${steps.cp.request.price_minor}", "5", "${vars.qty}"} {
		cases = append(cases, lintCase{name: "a number into int64 " + ref, cat: shop, c: addStockFrom(ref), pick: h(sevE, "", "int64")})
	}
	runLintCases(t, cases)
}

type upFront struct {
	cat      *catalog.Catalog
	c        *chain.Chain
	ref      string
	prob     string
	refused  bool
	lintOnly bool
}

func orderThen(next *chain.Step) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	next.Expect = ok
	return &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "s", "name": "n", "price_minor": "250"}, Expect: ok},
		{ID: "o", Call: "shop.orders.v1.OrderService/CreateOrder",
			Body:   map[string]any{"id_customer": "c", "lines": []any{map[string]any{"id_product": "${cp.product.id_product}", "qty": 1}}},
			Expect: ok, Export: map[string]string{"lines": "order.lines", "first_qty": "order.lines.0.qty"}},
		next,
	}}
}

func productNamed(name string, headers map[string]string, vars map[string]any) *chain.Chain {
	ok := []chain.Expectation{{Path: "status.code", Equals: "SUCCESS"}}
	return &chain.Chain{Name: "t", Vars: vars, Steps: []*chain.Step{
		{ID: "cp", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "s", "name": "n", "price_minor": "250"},
			Expect: ok, Export: map[string]string{"prod": "product", "pid": "product.id_product"}},
		{ID: "cp2", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "s2", "name": name, "price_minor": "250"}, Expect: ok, Headers: headers},
	}}
}

func placeOrderWith(field, ref string) *chain.Chain {
	return &chain.Chain{Name: "t", Steps: []*chain.Step{
		{ID: "p1", Call: "shrt.test.rich.v1.OrderService/PlaceOrder",
			Body: map[string]any{"id_order": "o1", "due_at": "2026-01-01T00:00:00Z", "flagged": true, "first_line": map[string]any{"sku": "a", "qty": "1"}}},
		{ID: "p2", Call: "shrt.test.rich.v1.OrderService/PlaceOrder", Body: map[string]any{field: ref}},
	}}
}

func TestReferencesRefusedBeforeAnythingIsSent(t *testing.T) {
	shop, rich := catalogtest.Shop(), catalogtest.Rich()
	stock := func(field, ref string) *chain.Chain {
		body := map[string]any{"id_product": "${cp.product.id_product}", "qty": 1}
		body[field] = ref
		return orderThen(&chain.Step{ID: "x", Call: "shop.catalog.v1.StockService/AddStock", Body: body})
	}
	order := func(ref string) *chain.Chain {
		return orderThen(&chain.Step{ID: "x", Call: "shop.orders.v1.OrderService/CreateOrder", Body: map[string]any{"id_customer": "c", "lines": ref}})
	}
	cases := []upFront{
		{cat: shop, c: order("${o.order.id_order}"), ref: "${o.order.id_order}", refused: true},
		{cat: shop, c: order("${first_qty}"), ref: "${first_qty}", refused: true},
		{cat: shop, c: order("${o.order.lines}"), ref: "${o.order.lines}"},
		{cat: shop, c: stock("qty", "${o.order.lines.0.qty}"), ref: "${o.order.lines.0.qty}"},
		{cat: shop, c: addStockFrom("${cp.product}"), ref: "${cp.product}", refused: true},
		{cat: catalogtest.New(), c: twoStep("${create.idd}", chain.Expectation{Path: "id", Equals: "${create.idd}"}), ref: "idd", refused: true},
		{cat: catalogtest.New(), c: twoStep("${create.id}", chain.Expectation{Path: "id", Equals: "${create.id}"}), ref: "create.id"},
		{cat: shop, c: productNamed("n", map[string]string{"X-Prod": "${cp.product.sku}"}, nil), ref: "X-Prod", lintOnly: true},
	}
	for _, f := range [][2]string{{"id_order", "first_line"}, {"channel", "first_line"}, {"memo", "first_line"}} {
		ref := "${steps.p1.request." + f[1] + "}"
		cases = append(cases, upFront{cat: rich, c: placeOrderWith(f[0], ref), ref: ref, refused: true, lintOnly: true})
	}
	for _, f := range [][2]string{{"id_order", "due_at"}, {"memo", "flagged"}, {"id_order", "first_line.sku"}, {"first_line", "first_line"}} {
		ref := "${steps.p1.request." + f[1] + "}"
		cases = append(cases, upFront{cat: rich, c: placeOrderWith(f[0], ref), ref: ref, lintOnly: true})
	}
	for _, ref := range []string{"${o.order.lines}", "${steps.o.request.lines}", "${lines}"} {
		cases = append(cases, upFront{cat: shop, c: stock("qty", ref), ref: ref, refused: true}, upFront{cat: shop, c: stock("id_product", ref), ref: ref, refused: true})
	}
	for _, ref := range []string{"${pid}", "${exports.pid}", "${cp.product.sku}", "${steps.cp.request.name}", "${price}", "${cp.product.price_minor}", "5", "${vars.qty}"} {
		cases = append(cases, upFront{cat: shop, c: addStockFrom(ref), ref: ref, lintOnly: true})
	}
	for _, ref := range []string{"${cp.product}", "${cp.status.details}", "${prod}"} {
		cases = append(cases, upFront{cat: shop, c: productNamed("x-"+ref+" y", nil, nil), ref: ref, refused: true})
	}
	for _, ref := range []string{"${cp.product}", "${prod}", "${exports.prod}", "${cp.status}"} {
		cases = append(cases, upFront{cat: shop, c: productNamed(ref, nil, nil), ref: ref, refused: true})
	}
	for _, ref := range []string{"${pid}", "${cp.product.sku}", "${cp.status.code}"} {
		cases = append(cases, upFront{cat: shop, c: productNamed("x-"+ref+" y", nil, nil), ref: ref}, upFront{cat: shop, c: productNamed(ref, nil, nil), ref: ref})
	}
	for _, v := range []string{"${cp.product}", "p ${cp.product} q", "${prod}"} {
		cases = append(cases, upFront{cat: shop, c: productNamed("n", map[string]string{"X-Prod": v}, nil), ref: "X-Prod", prob: "header X-Prod", refused: true})
	}
	cases = append(cases,
		upFront{cat: catalogtest.New(), c: twoStep("${steps.create.request.nmae}"), ref: "nmae", refused: true},
		upFront{cat: catalogtest.New(), c: twoStep("${steps.create.request.name}"), ref: "request.name"})
	for _, tc := range cases {
		if err := tc.c.Normalize(); err != nil {
			t.Fatal(err)
		}
		ref, prob := tc.ref, tc.prob
		if prob == "" {
			prob = ref
		}
		problems := strings.Join(tc.c.ResponseRefProblems(tc.cat), " ")
		lintErr := false
		for _, i := range chain.Lint(tc.c, tc.cat) {
			lintErr = lintErr || (i.IsError() && strings.Contains(i.Message, ref))
		}
		switch {
		case tc.refused && !strings.Contains(problems, prob):
			t.Errorf("%s: must be refused before anything is sent, got %q", ref, problems)
		case tc.refused && !tc.lintOnly && !lintErr:
			t.Errorf("%s: lint must error", ref)
		case !tc.refused && problems != "":
			t.Errorf("%s: must not be refused, got %q", ref, problems)
		case !tc.refused && !tc.lintOnly && lintErr:
			t.Errorf("%s: must not be a lint error", ref)
		}
	}
}

func TestLintAcceptsEveryReferenceTheGrammarDocuments(t *testing.T) {
	sc := chain.ReferenceExampleScope()
	vars := map[string]any{}
	for name, v := range sc.Vars {
		if s, ok := v.(string); ok && chain.HasReference(s) {
			v = "verbatim"
		}
		vars[name] = v
	}
	export := map[string]string{}
	for name := range sc.Exports {
		export[name] = "id"
	}
	build := func(headers map[string]string, expect []chain.Expectation) *chain.Chain {
		c := &chain.Chain{Name: "g", Vars: vars, Steps: []*chain.Step{
			{ID: chain.ReferenceExampleStep, Call: "ThingService/Create", Body: widget(), Export: export, Expect: []chain.Expectation{okCode}},
			{ID: "use", Call: "ThingService/Fetch", Body: map[string]any{"id": "thing-1"}, Headers: headers, Expect: append([]chain.Expectation{okCode}, expect...)},
		}}
		if err := c.Normalize(); err != nil {
			t.Fatal(err)
		}
		return c
	}
	errsOf := func(c *chain.Chain) []chain.Issue {
		errs := []chain.Issue{}
		for _, i := range chain.Lint(c, catalogtest.New()) {
			if i.IsError() && i.Kind != chain.KindDeadRef {
				errs = append(errs, i)
			}
		}
		return errs
	}
	if len(chain.ReferenceExamples) == 0 {
		t.Fatal("the GRAMMAR reference table is empty")
	}
	headers, expect := map[string]string{}, []chain.Expectation{}
	for n, ex := range append(append([]chain.ReferenceExample{}, chain.ReferenceExamples...), chain.OlderReferenceExamples...) {
		headers[fmt.Sprintf("X-Example-%d", n)] = ex.Ref
		expect = append(expect, chain.Expectation{Path: "name", Equals: ex.Ref})
	}
	if errs := errsOf(build(headers, expect)); len(errs) != 0 {
		t.Fatalf("GRAMMAR documents these references, so lint may not reject any: %+v", errs)
	}
	for _, ref := range []string{"${nowunix+1h}", "${today-x}", "${now+5s}"} {
		if errs := errsOf(build(map[string]string{"X-When": ref}, nil)); len(errs) != 1 || !strings.Contains(errs[0].Message, "whole number of seconds") {
			t.Errorf("%s dies at run time, so lint rejects it with the resolver's reason: %+v", ref, errs)
		}
	}
}

func TestTimestampHints(t *testing.T) {
	envPath, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	chain.ApplyConventions(nil, "status.code", "SUCCESS")
	t.Cleanup(func() { chain.ApplyConventions(nil, envPath, ok) })
	head := "apiVersion: shrt/v1\nname: stamped\nsteps:\n    - id: create_item\n      call: ItemService/CreateItem\n      body: {name: widget}\n      expect:\n        - {path: status.code, equals: SUCCESS}\n"
	for _, tc := range []struct {
		yaml string
		has  []string
		lack string
	}{
		{head + "        - {path: item.expires_at, gte: \"${nowunix}\"}\n    - id: get_item\n      call: ItemService/GetItem\n      body: {id_item: \"${create_item.item.id_item}\"}\n      expect:\n        - {path: status.code, equals: SUCCESS}\n        - {path: item.expires_at, gte: \"${nowunix}\"}\n        - {path: item.updated_at, gte: \"${nowunix-300}\"}\n",
			[]string{`create_item timestamp item.created_at unasserted; expect within: {of: "${nowunix}", by: 300}`,
				"get_item timestamp item.created_at unasserted; expect equals: ${create_item.item.created_at}"}, "nowunix+3600"},
		{head + "        - {path: item.created_at, within: {of: \"${nowunix}\", by: 300}}\n        - {path: item.updated_at, within: {of: \"${nowunix}\", by: 300}}\n",
			[]string{`create_item timestamp item.expires_at unasserted; expect within: {of: "${nowunix+3600}", by: 5}`}, ""},
		{head + "        - {path: item.created_at, within: {of: \"${nowunix}\", by: 300}}\n        - {path: item.updated_at, within: {of: \"${nowunix}\", by: 300}}\n        - {path: item.expires_at, gte: \"${nowunix}\"}\n    - id: create_item_same_name\n      call: ItemService/CreateItem\n      body: {name: widget}\n      expect:\n        - {path: status.code, not_equal: SUCCESS}\n        - {path: item, exists: false}\n",
			nil, "timestamp"},
	} {
		out := []string{}
		for _, i := range chain.LintWith(yamlChain(t, tc.yaml), catalogtest.Stamped(), chain.LintOptions{Hints: true}) {
			if i.Kind == chain.KindUnassertedTimestamp {
				out = append(out, i.Step+" "+i.Message)
			}
		}
		got := strings.Join(out, "\n")
		for _, want := range tc.has {
			if !strings.Contains(got, want) {
				t.Errorf("want %q in %q", want, got)
			}
		}
		if tc.lack != "" && strings.Contains(got, tc.lack) {
			t.Errorf("want no %q in %q", tc.lack, got)
		}
	}
}

func TestStrictPromotesByKindNotByText(t *testing.T) {
	for _, k := range []string{chain.KindUnfailable, chain.KindAssertsNone, chain.KindUnreachable, chain.KindDeadRef, chain.KindBadExport} {
		got := chain.Promote([]chain.Issue{{Severity: sevW, Kind: k, Message: "reworded"}}, chain.IsAssertionQualityIssue)
		if got[0].Severity != sevE {
			t.Errorf("kind %q must be promoted by -strict", k)
		}
	}
	prose := chain.Issue{Severity: sevW, Message: "this message contains the words cannot fail but carries no Kind"}
	if chain.Promote([]chain.Issue{prose}, chain.IsAssertionQualityIssue)[0].Severity != sevW {
		t.Error("promotion fired on message text alone")
	}
}

func TestDescribeMarksADeprecatedMethod(t *testing.T) {
	m, err := deprecatedCatalog(t).Lookup("ThingService/Fetch")
	if err != nil || !m.Deprecated() {
		t.Fatalf("the method reports its deprecation: %v", err)
	}
	if text := catalog.DescribeMessage(m.Output()).Text(); !strings.Contains(text, "DEPRECATED") {
		t.Fatalf("describe marks a deprecated field:\n%s", text)
	}
}
