package chain_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestEvaluate(t *testing.T) {
	products := map[string]any{"products": []any{
		map[string]any{"id_product": "p-1", "sku": "a"},
		map[string]any{"id_product": "p-12", "sku": "b", "qty": "3"},
	}, "tags": []any{"x", "y"}}
	ok, refused := chain.TransportOutcome(200, "", ""), chain.TransportOutcome(401, "unauthenticated", "token rejected")
	for _, tc := range []struct {
		name   string
		e      chain.Expectation
		resp   any
		passed bool
		detail string
	}{
		{"not_equal on an absent path fails", chain.Expectation{Path: "rows.0.qty", NotEqual: "0"}, map[string]any{"rows": []any{}}, false, "not present"},
		{"not_equal on a different value passes", chain.Expectation{Path: "rows.0.qty", NotEqual: "0"}, map[string]any{"rows": []any{map[string]any{"qty": "2000000"}}}, true, ""},
		{"includes one field", chain.Expectation{Path: "products", Includes: map[string]any{"id_product": "p-12"}}, products, true, ""},
		{"includes two fields of one item", chain.Expectation{Path: "products", Includes: map[string]any{"id_product": "p-12", "qty": 3}}, products, true, ""},
		{"includes fields of two items", chain.Expectation{Path: "products", Includes: map[string]any{"id_product": "p-1", "sku": "b"}}, products, false, ""},
		{"includes an absent item", chain.Expectation{Path: "products", Includes: map[string]any{"id_product": "p-2"}}, products, false, ""},
		{"includes on an absent list", chain.Expectation{Path: "missing", Includes: map[string]any{"id_product": "p-1"}}, products, false, ""},
		{"includes on a scalar", chain.Expectation{Path: "products.0.sku", Includes: "a"}, products, false, ""},
		{"includes a scalar item", chain.Expectation{Path: "tags", Includes: "y"}, products, true, ""},
		{"transport ok", chain.Expectation{Path: "transport.code", Equals: chain.TransportOK}, ok, true, ""},
		{"a success has no transport message", chain.Expectation{Path: "transport.message", Exists: new(false)}, ok, true, ""},
		{"http_status compares as text", chain.Expectation{Path: "transport.http_status", Equals: "401"}, refused, true, ""},
	} {
		r := tc.e.Evaluate(tc.resp)
		if r.Passed != tc.passed || !strings.Contains(r.Detail, tc.detail) {
			t.Errorf("%s: got %+v", tc.name, r)
		}
	}
	if refs := (chain.Expectation{Path: "products", Includes: map[string]any{"id_product": "${vars.id}"}}).References(); len(refs) != 1 || refs[0] != "vars.id" {
		t.Errorf("the reference inside includes is seen: %v", refs)
	}
}

func TestVacuousRuleDetailNamesTheValue(t *testing.T) {
	c := yamlChain(t, "apiVersion: shrt/v1\nname: v\nsteps:\n  - id: fetch\n    call: ThingService/Fetch\n    expect:\n      - {path: name, contains: \"\"}\n      - {path: id, not_empty: false}\n")
	for i, want := range []string{`contains: ""`, `not_empty: false`} {
		r := c.Steps[0].Expect[i].Evaluate(map[string]any{"name": "w", "id": "x"})
		if !strings.Contains(r.Detail, want) || strings.Contains(r.Detail, "has no rule") {
			t.Errorf("the result must name %s as a value that cannot fail, got %q", want, r.Detail)
		}
	}
}

func TestComparisonRules(t *testing.T) {
	now := time.Unix(1789123474, 0)
	scope := chain.NewScope(nil)
	scope.Now = func() time.Time { return now }
	within, err := (chain.Expectation{Path: "expires_at", Within: &chain.Within{Of: "${nowunix+3600}", By: 5}}).ResolveWith(scope)
	if err != nil {
		t.Fatal(err)
	}
	if r := within.EvaluateTyped(map[string]any{"expires_at": strconv.FormatInt(now.Unix()+3602, 10)}, nil, "int64"); !r.Passed {
		t.Errorf("two seconds past the hour is within 5s: %+v", r)
	}
	if r := within.EvaluateTyped(map[string]any{"expires_at": strconv.FormatInt((now.Unix()+3600)*1000, 10)}, nil, "int64"); r.Passed {
		t.Errorf("milliseconds are not within 5s: %+v", r)
	}
	gte, err := (chain.Expectation{Path: "created_at", Gte: "${nowunix-60}"}).ResolveWith(scope)
	if err != nil {
		t.Fatal(err)
	}
	if r := gte.EvaluateTyped(map[string]any{"created_at": now.UTC().Format(time.RFC3339)}, nil, ""); !r.Passed || r.Rule != "gte" {
		t.Errorf("an RFC3339 timestamp compares as unix seconds: %+v", r)
	}
	for _, e := range []chain.Expectation{
		{Path: "product.created_at", Within: &chain.Within{Of: "1789123474", By: 300}},
		{Path: "product.created_at", Gte: "1789123474"},
		{Path: "product.created_at", Lte: "1789123474"},
	} {
		canonical := map[string]any{"product": map[string]any{"id": "p1", "created_at": nil}}
		empty := map[string]any{"product": map[string]any{}}
		for _, tc := range []struct {
			canonical, sent any
			detail          string
		}{
			{canonical, map[string]any{"product": map[string]any{"id": "p1"}}, "path not present in response"},
			{empty, empty, "path not present in response"},
			{canonical, canonical, "null"},
			{map[string]any{"product": map[string]any{"created_at": "soon"}}, nil, "not a number or an RFC3339 time"},
		} {
			if r := e.EvaluateTyped(tc.canonical, tc.sent, ""); r.Passed || !strings.Contains(r.Detail, tc.detail) {
				t.Errorf("%+v: want %q, got %+v", e, tc.detail, r)
			}
		}
	}
}

func TestFailureLines(t *testing.T) {
	for r, want := range map[chain.ExpectResult]string{
		{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS"}: "FAIL status.code want≠SUCCESS got=SUCCESS",
		{Path: "n", Rule: "equals", Want: 3, Got: 1}:                              "FAIL n want=3 got=1",
		{Path: "n", Rule: "gte", Want: "3", Got: 1}:                               "FAIL n want≥3 got=1",
		{Path: "id", Rule: "exists", Want: true, Got: false}:                      "FAIL id want present got absent",
		{Path: "msg", Rule: "contains", Want: "x", Got: "y"}:                      "FAIL msg want contains x got=y",
	} {
		if got := r.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	for r, want := range map[chain.ExpectResult]string{
		{Path: "order.total_minor", Rule: "equals", Want: float64(14497), Got: "6250"}:          "order.total_minor want=14497 got=6250",
		{Path: "status.code", Rule: "not_equal", Want: "SUCCESS", Got: "SUCCESS"}:               "status.code want≠SUCCESS got=SUCCESS",
		{Path: "a.b", Rule: "equals", Want: float64(1), Detail: "path not present in response"}: "path not present",
	} {
		if got := chain.DescribeFailure(r); !strings.Contains(got, want) {
			t.Errorf("DescribeFailure = %q, want %q", got, want)
		}
	}
}

func TestNearResponsePathKeepsAnIndexIntoAListOnly(t *testing.T) {
	cat := catalogtest.Shop()
	for _, tc := range []struct{ rpc, path, want string }{
		{"shop.catalog.v1.ProductService/CreateProduct", "products.0.id_product", ` (did you mean "product.id_product"?)`},
		{"shop.catalog.v1.ProductService/ListProducts", "product.0.id_product", ` (did you mean "products.0.id_product"?)`},
	} {
		m, err := cat.Lookup(tc.rpc)
		if err != nil {
			t.Fatal(err)
		}
		if got := chain.NearResponsePath(m, tc.path); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.rpc, tc.path, got, tc.want)
		}
	}
}

func TestConventions(t *testing.T) {
	t.Cleanup(func() {
		chain.ApplyConventions(nil, chain.DefaultEnvelopePath, chain.DefaultEnvelopeOK)
		chain.ApplyCodeFields(nil)
		chain.SetItemEnvelope("")
	})
	chain.ApplyConventions(nil, "", "")
	chain.ApplyCodeFields(nil)
	for path, want := range map[string]bool{
		"error.code": true, "error.details.0.app_code": true, "error.details.0.reason": true, "error.details.0.error_code": true,
		"results.0.error.details.0.app_code": true, "error.details.1.reason": true, "transport.code": true, "transport.http_status": true,
		"status.code": false, "transport.message": false, "counterparties.0.code": false, "id_request": false, "pagination.total": false,
	} {
		if chain.IsCodePath(path) != want {
			t.Errorf("IsCodePath(%q) under the defaults, want %v", path, want)
		}
	}
	chain.ApplyConventions(nil, "status.code", "SUCCESS")
	if !chain.IsCodePath("status.code") || chain.IsCodePath("error.code") {
		t.Error("the configured envelope is the verdict path")
	}
	chain.ApplyCodeFields([]string{"failure_id"})
	if !chain.IsCodePath("status.details.0.failure_id") || chain.IsCodePath("status.details.0.app_code") {
		t.Error("code_fields replaces the default detail fields")
	}
	for call, want := range map[string]bool{
		"ShowcaseProduct": false, "Getaway": false, "Listen": false, "Counterfeit": false, "shop.v1.PromoService/ShowcaseFoo": false,
		"CreateDeal": false, "svc.v1.Service/ApproveOffset": false,
		"GetProduct": true, "Get": true, "Get2Product": true, "ListProducts": true, "shop.v1.CatalogService/ShowItem": true, "Get_product": true,
		"PreviewDealSurcharge": true, "svc.v1.Service/PreviewMovementFee": true,
	} {
		if got := chain.IsReadOnlyCall(call); got != want {
			t.Errorf("IsReadOnlyCall(%q) = %v, want %v", call, got, want)
		}
	}
	chain.ApplyConventions([]string{"Fetch", "List"}, "status.code", "SUCCESS")
	chain.ReadOnlyPrefixes()[0] = "Mutated"
	if again := chain.ReadOnlyPrefixes()[0]; again != "Fetch" {
		t.Errorf("ReadOnlyPrefixes hands back a copy, got %q", again)
	}
	chain.ApplyConventions(nil, "status.code", "SUCCESS")
	if !chain.IsReadOnlyCall("acme.v1.Service/FetchThing") {
		t.Error("an empty list restores the defaults")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(write bool) {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				if write {
					chain.ApplyConventions([]string{"Fetch"}, "error.code", "OK")
					chain.SetItemEnvelope("results[].error.code")
					continue
				}
				_, _, _, _ = chain.EnvelopePath(), chain.EnvelopeOK(), chain.EnvelopeField(), chain.ItemEnvelope()
				_, _, _ = chain.IsEnvelopePath("error.code"), chain.IsReadOnlyCall("acme.v1.Service/FetchThing"), chain.ReadOnlyPrefixes()
			}
		}(i%2 == 0)
	}
	wg.Wait()
}

func decodeBody(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestItemRefusals(t *testing.T) {
	defer chain.SetItemEnvelope("")
	defer chain.SetEnvelope("", "")
	for _, tc := range []struct {
		name, envelope, item, raw string
		lines                     []string
		code                      string
		err                       bool
	}{
		{"undeclared convention sees nothing", "", "", `{"error":{"code":"OK"},"results":[{"error":{"code":"invalid_argument"}},{"error":{"code":"OK"}}]}`, nil, "", false},
		{"a refused line", "", "results[].error.code", `{"error":{"code":"OK"},"results":[{"error":{"code":"invalid_argument"}},{"error":{"code":"OK"}}]}`, []string{"results.0"}, "", false},
		{"every item succeeded", "", "results[].error.code", `{"error":{"code":"OK"},"results":[{"error":{"code":"OK"}},{"error":{"code":"OK"}}]}`, nil, "", false},
		{"no such list", "", "results[].error.code", `{"error":{"code":"OK"},"id_deal":"abc"}`, nil, "", false},
		{"an unset item error is success", "", "results[].error.code", `{"error":{"code":"OK"},"results":[{"error":null},{"error":{"code":"invalid_argument"}},{"error":null}]}`, []string{"results.1"}, "", false},
		{"a mis-pointed item field", "", "results[].error.code", `{"status":{"code":"OK"},"results":[{"outcome":{"code":"REFUSED"}}]}`, nil, "", true},
		{"no [] separator", "", "results.error.code", `{"status":{"code":"OK"},"results":[{"outcome":{"code":"REFUSED"}}]}`, nil, "", true},
		{"not a list", "", "status[].code", `{"status":{"code":"OK"},"results":[{"outcome":{"code":"REFUSED"}}]}`, nil, "", true},
		{"an empty verdict beside a present one", "status.code", "results[].status.code", `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":"SUCCESS"}}]}`, []string{"results.0"}, chain.NoItemVerdict, false},
		{"an empty verdict beside a refusal", "status.code", "results[].status.code", `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":"REJECTED"}}]}`, []string{"results.0", "results.1"}, chain.NoItemVerdict, false},
		{"every verdict empty", "status.code", "results[].status.code", `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":""}},{"status":{"code":""}}]}`, nil, "", false},
		{"a null verdict beside a present one", "status.code", "results[].status.code", `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"status":null,"qty_on_hand":"0"}]}`, []string{"results.1"}, "", false},
		{"a missing verdict beside a present one", "status.code", "results[].status.code", `{"status":{"code":"SUCCESS"},"results":[{"status":{"code":"SUCCESS"}},{"qty_on_hand":"0"}]}`, []string{"results.1"}, "", false},
	} {
		chain.SetEnvelope(tc.envelope, map[bool]string{true: "SUCCESS", false: ""}[tc.envelope != ""])
		chain.SetItemEnvelope(tc.item)
		got, err := chain.ItemRefusals(decodeBody(t, tc.raw))
		if (err != nil) != tc.err || len(got) != len(tc.lines) {
			t.Errorf("%s: got %v %v, want lines %v err %v", tc.name, got, err, tc.lines, tc.err)
			continue
		}
		for i, line := range tc.lines {
			if got[i].Line != line || (tc.code != "" && got[0].Code != tc.code) {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.lines)
			}
		}
	}
}

func TestDeclaredRefusals(t *testing.T) {
	defer chain.SetItemEnvelope("")
	defer chain.SetEnvelope("", "")
	for _, tc := range []struct {
		path     string
		e        chain.Expectation
		declares bool
	}{
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.code", Equals: "invalid_argument"}, true},
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.code", NotEqual: "OK"}, true},
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.code", Contains: "invalid"}, true},
		{"results.1.error.code", chain.Expectation{Path: "results[1].error.code", Equals: "invalid_argument"}, true},
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.code", Exists: new(true)}, false},
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.code", NotEmpty: true}, false},
		{"results.1.error.code", chain.Expectation{Path: "results.0.error.code", Equals: "invalid_argument"}, false},
		{"results.1.error.code", chain.Expectation{Path: "results.1.error.message", Equals: "x"}, false},
	} {
		if got := chain.DeclaresVerdict([]chain.Expectation{tc.e}, tc.path); got != tc.declares {
			t.Errorf("%+v: DeclaresVerdict = %v", tc.e, got)
		}
	}
	chain.SetItemEnvelope("results[].error.code")
	two := []chain.ItemRefusal{{Path: "results.0.error.code", Code: "not_found"}, {Path: "results.1.error.code", Code: "invalid_argument"}}
	got := chain.UndeclaredRefusals(two, []chain.Expectation{{Path: "results.0.error.code", Equals: "not_found"}})
	if len(got) != 1 || got[0].String() != "results.1.error.code = invalid_argument" || len(chain.UndeclaredRefusals(two, nil)) != 2 {
		t.Errorf("only the undeclared line stays: %v", got)
	}
	notFound := []chain.ItemRefusal{{Path: "results.0.error.code", Code: "not_found"}}
	if len(chain.UndeclaredRefusals(notFound, []chain.Expectation{okCode, {Path: "results.0.error.code", NotEqual: "ZZZ_NEVER"}})) != 1 ||
		len(chain.UndeclaredRefusals(notFound, []chain.Expectation{{Path: "results.0.error.code", NotEqual: "OK"}})) != 0 {
		t.Error("not_equal an impossible value declares nothing, not_equal OK declares the refusal")
	}
	for _, tc := range []struct {
		path string
		want any
		bad  bool
	}{
		{"id_deal", "", false}, {"rows.0.parked_out_receivable_qty", "0", false}, {"pagination.total", "0", false},
		{"results.3.error.code", "NEVER", true}, {"results.3.error.code", "OK", false}, {"error.code", "NEVER", true},
	} {
		if got := chain.VacuousNotEqual(tc.path, tc.want); got != tc.bad {
			t.Errorf("VacuousNotEqual(%q, %v) = %v", tc.path, tc.want, got)
		}
	}
	if !chain.VacuousNotEqualResult("assets", "NEVER_THIS", nil) || !chain.VacuousNotEqualResult("assets", "NEVER_THIS", []any{}) || chain.VacuousNotEqualResult("id_deal", "", "abc") {
		t.Error("a scalar against a container can never differ, against a scalar it can")
	}
	chain.SetEnvelope("status.code", "SUCCESS")
	chain.SetItemEnvelope("results[].status.code")
	refusals, err := chain.ItemRefusals(map[string]any{"status": map[string]any{"code": "SUCCESS"}, "results": []any{
		map[string]any{"status": map[string]any{"code": "SUCCESS"}},
		map[string]any{"status": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1204), "reason": "ProductNotFound"}}}},
	}})
	if err != nil || len(refusals) != 1 {
		t.Fatalf("want one refusal, got %v %v", refusals, err)
	}
	for _, tc := range []struct {
		e        chain.Expectation
		declares bool
	}{
		{chain.Expectation{Path: "results.1.status.details.0.app_code", Equals: 1204}, true},
		{chain.Expectation{Path: "results.1.status.details.0.reason", Equals: "ProductNotFound"}, true},
		{chain.Expectation{Path: "results.1.status.code", Equals: "REJECTED"}, true},
		{chain.Expectation{Path: "results.0.status.details.0.app_code", Equals: 1204}, false},
		{chain.Expectation{Path: "results.10.status.details.0.app_code", Equals: 1204}, false},
		{chain.Expectation{Path: "results.1.status.details.0.app_code", Exists: new(true)}, false},
		{chain.Expectation{Path: "results.1.status.details.0.app_code", NotEmpty: true}, false},
		{chain.Expectation{Path: "results.1.status.details.0.app_code", NotEqual: 9999}, false},
		{chain.Expectation{Path: "results.1.status.details.0.reason", Equals: ""}, false},
		{chain.Expectation{Path: "results.1.id_product", Equals: "prd-x"}, false},
	} {
		if got := len(chain.UndeclaredRefusals(refusals, []chain.Expectation{tc.e})) == 0; got != tc.declares {
			t.Errorf("%+v: declares = %v", tc.e, got)
		}
	}
}

func TestItemEnvelopeSchema(t *testing.T) {
	defer chain.SetItemEnvelope("")
	verdict := []*catalog.Field{{Name: "code", Kind: "string"}}
	shape := func(repeated, truncated bool, item ...*catalog.Field) []*catalog.Field {
		return []*catalog.Field{{Name: "error", Kind: "message", Fields: verdict}, {Name: "results", Kind: "message", Repeated: repeated, Truncated: truncated, Fields: item}}
	}
	withVerdict := shape(true, false, &catalog.Field{Name: "error", Kind: "message", Fields: verdict}, &catalog.Field{Name: "amount", Kind: "string"})
	chain.SetItemEnvelope("")
	if chain.ItemEnvelopeDeclared(withVerdict) || chain.ValidateItemEnvelope(catalogtest.New()) != nil {
		t.Error("with the convention unset nothing is declared and nothing is invalid")
	}
	chain.SetItemEnvelope("results[].error.code")
	for name, tc := range map[string]struct {
		fields []*catalog.Field
		want   bool
	}{
		"the declared shape":   {withVerdict, true},
		"a receipt list":       {shape(true, false, &catalog.Field{Name: "id", Kind: "string"}), false},
		"a singular message":   {shape(false, false, &catalog.Field{Name: "error", Kind: "message", Fields: verdict}), false},
		"a list not descended": {shape(true, true), true},
	} {
		if got := chain.ItemEnvelopeDeclared(tc.fields); got != tc.want {
			t.Errorf("%s: ItemEnvelopeDeclared = %v", name, got)
		}
	}
	if chain.ValidateItemEnvelope(catalogtest.New()) == nil || chain.ValidateItemEnvelope(catalogtest.Batch()) != nil {
		t.Error("the convention must name a results list some response declares")
	}
}
