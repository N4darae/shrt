package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

const (
	shopCreateProduct  = "shop.catalog.v1.ProductService/CreateProduct"
	shopCreateCustomer = "shop.customers.v1.CustomerService/CreateCustomer"
	shopCreateOrder    = "shop.orders.v1.OrderService/CreateOrder"
	shopConfirmOrder   = "shop.orders.v1.OrderService/ConfirmOrder"
	shopWatchOrder     = "shop.orders.v1.OrderService/WatchOrder"
)

func shopScaffold(t *testing.T, domain string, existing *contract.Library) []byte {
	t.Helper()
	cat := catalogtest.Shop()
	methods := contract.Domains(cat.Methods())[domain]
	if len(methods) == 0 {
		t.Fatalf("no rpcs in domain %s", domain)
	}
	raw, err := contract.RenderOverlay(contract.ScaffoldOverlay(domain, methods, existing, cat.Methods()))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertNoYAMLComments(t *testing.T, raw []byte) {
	t.Helper()
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Fatalf("line %d is a comment line (%q)\n%s", i+1, line, raw)
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
			t.Fatalf("the scaffold carries a YAML comment (%q %q %q) — a repo whose pre-commit hook "+
				"blocks new comments cannot commit it as written\n%s", n.HeadComment, n.LineComment, n.FootComment, raw)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
}

func loadShopOverlay(t *testing.T, domain string, raw []byte) *contract.Overlay {
	t.Helper()
	path := filepath.Join(t.TempDir(), domain+".yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := contract.LoadOverlay(path)
	if err != nil {
		t.Fatalf("the scaffold does not load: %v\n%s", err, raw)
	}
	return o
}

func TestContractInitScaffoldCarriesItsTodosWithoutYAMLComments(t *testing.T) {
	for _, domain := range []string{"catalog", "customers", "orders"} {
		raw := shopScaffold(t, domain, nil)
		assertNoYAMLComments(t, raw)
		if !strings.Contains(string(raw), "required: ['"+contract.RequiredTodoText+"']") {
			t.Fatalf("%s: required must carry its TODO as a value now that it cannot be a comment:\n%s", domain, raw)
		}
	}
}

func TestRequiredTodoValueIsSeenByLintPlanAndQuality(t *testing.T) {
	cat := catalogtest.Shop()
	raw := shopScaffold(t, "catalog", nil)
	o := loadShopOverlay(t, "catalog", raw)
	c := o.RPCs[shopCreateProduct]
	if len(c.Required) != 0 {
		t.Fatalf("the TODO marker must not be read as a field name, got required %v", c.Required)
	}
	if !c.IsUnfilled("required") {
		t.Fatal("required carrying the TODO value must be reported unfilled")
	}
	lib := contract.NewLibrary([]*contract.Overlay{o})

	warned := false
	for _, i := range contract.LintAll(lib, cat, nil) {
		if i.IsError() && i.RPC == shopCreateProduct && strings.HasPrefix(i.Field, "required") {
			t.Fatalf("lint errors on the required TODO instead of warning: %+v", i)
		}
		if i.RPC == shopCreateProduct && i.Field == "required" && strings.Contains(i.Message, "unfilled TODO") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("lint must still warn that required is an unfilled TODO")
	}

	plan, err := contract.BuildPlan(shopCreateProduct, lib, cat, "p")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "required is an unfilled TODO") {
		t.Fatalf("plan lost the unfilled-required note: %v", plan.Notes)
	}

	for _, row := range contract.Measure(lib, cat, "").RPCs {
		if row.RPC == shopCreateProduct && !row.EmptyRequired {
			t.Fatalf("quality must still score an unfilled required as empty: %+v", row)
		}
	}
}

func TestCommentTodoOverlaysAreStillRecognisedAndRewrittenWithoutComments(t *testing.T) {
	legacy := []byte(`apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product with zero stock
        required: [] # TODO: which fields the server rejects without
        status: draft
`)
	o := loadShopOverlay(t, "catalog", legacy)
	if !o.RPCs[shopCreateProduct].IsUnfilled("required") {
		t.Fatal("an existing overlay carrying the old comment TODO must still be recognised")
	}
	raw := shopScaffold(t, "catalog", contract.NewLibrary([]*contract.Overlay{o}))
	assertNoYAMLComments(t, raw)
	again := loadShopOverlay(t, "catalog", raw)
	c := again.RPCs[shopCreateProduct]
	if c.Summary != "adds a product with zero stock" {
		t.Fatalf("carry-forward lost the curated summary: %q", c.Summary)
	}
	if !c.IsUnfilled("required") {
		t.Fatalf("carry-forward dropped the required TODO, so the re-run reads as filled:\n%s", raw)
	}
}

func TestCarryForwardKeepsAFilledRequired(t *testing.T) {
	filled := []byte(`apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product with zero stock
        required: [sku, name]
        status: draft
`)
	o := loadShopOverlay(t, "catalog", filled)
	raw := shopScaffold(t, "catalog", contract.NewLibrary([]*contract.Overlay{o}))
	c := loadShopOverlay(t, "catalog", raw).RPCs[shopCreateProduct]
	if strings.Join(c.Required, ",") != "sku,name" || c.IsUnfilled("required") {
		t.Fatalf("a filled required must carry forward as written, got %v unfilled=%v", c.Required, c.IsUnfilled("required"))
	}
}

func scaffoldedFrom(t *testing.T, raw []byte, rpc, field string) contract.FieldContract {
	t.Helper()
	o := loadShopOverlay(t, "x", raw)
	c, ok := o.RPCs[rpc]
	if !ok {
		t.Fatalf("no %s in\n%s", rpc, raw)
	}
	f, ok := c.Fields[field]
	if !ok {
		t.Fatalf("%s has no fields.%s in\n%s", rpc, field, raw)
	}
	return *f
}

func TestScaffoldWiresIDsNestedInWriteResponses(t *testing.T) {
	orders := shopScaffold(t, "orders", nil)
	cases := []struct{ rpc, field, from string }{
		{shopCreateOrder, "id_customer", shopCreateCustomer + "->customer.id_customer"},
		{shopCreateOrder, "lines.id_product", shopCreateProduct + "->product.id_product"},
		{shopConfirmOrder, "id_order", shopCreateOrder + "->order.id_order"},
	}
	for _, c := range cases {
		if got := scaffoldedFrom(t, orders, c.rpc, c.field).From; got != c.from {
			t.Errorf("%s fields.%s from = %q, want %q", c.rpc, c.field, got, c.from)
		}
	}
	if got := scaffoldedFrom(t, shopScaffold(t, "catalog", nil), "shop.catalog.v1.StockService/AddStock", "id_product").From; got != shopCreateProduct+"->product.id_product" {
		t.Errorf("AddStock id_product from = %q", got)
	}
}

func TestProducersOfSkipsEchoesForeignKeysAndStreams(t *testing.T) {
	cat := catalogtest.Shop()
	refs := func(field string) string {
		out := []string{}
		for _, p := range contract.ProducersOf(field, cat.Methods(), "") {
			out = append(out, p.Ref())
		}
		return strings.Join(out, ", ")
	}
	if got := refs("id_order"); got != shopCreateOrder+"->order.id_order" {
		t.Errorf("id_order producers = %q: ConfirmOrder and CancelOrder echo the id they were sent, and "+
			"WatchOrder streams, so none of them mints it", got)
	}
	if got := refs("id_customer"); got != shopCreateCustomer+"->customer.id_customer" {
		t.Errorf("id_customer producers = %q: order.id_customer is a reference inside an order, not the "+
			"customer's own id", got)
	}
	if got := refs("lines.id_product"); got != shopCreateProduct+"->product.id_product" {
		t.Errorf("a nested request key pairs by its leaf name, got %q", got)
	}
}

func TestScaffoldStaysATodoWhenTwoRPCsMintTheSameID(t *testing.T) {
	cat := catalogtest.New()
	producers := contract.ProducersOf("access_token", cat.Methods(), "")
	if len(producers) != 2 {
		t.Fatalf("two logins mint access_token; the scaffolder must offer both, not pick one: %v", producers)
	}
}

func shopLibrary(t *testing.T, docs ...string) *contract.Library {
	t.Helper()
	overlays := []*contract.Overlay{}
	for i, doc := range docs {
		o, err := contract.LoadOverlayBytes(filepath.Join("mem", string(rune('a'+i))+".yaml"), []byte(doc))
		if err != nil {
			t.Fatalf("overlay %d: %v", i, err)
		}
		overlays = append(overlays, o)
	}
	return contract.NewLibrary(overlays)
}

const shopCatalogOverlay = `apiVersion: shrt/contract/v1
domain: catalog
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [sku]
        fields:
            sku:
                value: SKU-1
        aliases:
            a:
                note: the first product
            b:
                note: the second product
        status: draft
`

const shopCustomersOverlay = `apiVersion: shrt/contract/v1
domain: customers
rpcs:
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: adds a customer
        required: [NONE]
        fields:
            email:
                value: a@example.test
        status: draft
`

const twoLineOrder = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            id_customer:
                from: shop.customers.v1.CustomerService/CreateCustomer->customer.id_customer
            lines.0.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct@a->product.id_product
            lines.0.qty:
                value: "2"
            lines.1.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct@b->product.id_product
            lines.1.qty:
                value: "3"
        status: draft
`

func orderLines(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	list, ok := body["lines"].([]any)
	if !ok {
		t.Fatalf("lines is %T", body["lines"])
	}
	out := []map[string]any{}
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}
	return out
}

func TestPlanBuildsOneListEntryPerDeclaredIndex(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, twoLineOrder)
	plan, err := contract.BuildPlan(shopCreateOrder, lib, cat, "two-lines")
	if err != nil {
		t.Fatal(err)
	}
	step, ok := plan.Chain.Step("create_order")
	if !ok {
		t.Fatalf("no create_order step in %v", plan.Order)
	}
	lines := orderLines(t, step.Body)
	if len(lines) != 2 {
		t.Fatalf("the contract declares lines.0 and lines.1, the plan built %d line(s): %v", len(lines), lines)
	}
	want := []struct{ id, qty string }{
		{"${create_product_a.product.id_product}", "2"},
		{"${create_product_b.product.id_product}", "3"},
	}
	for i, w := range want {
		if lines[i]["id_product"] != w.id || lines[i]["qty"] != w.qty {
			t.Errorf("lines.%d = %v, want id_product %s qty %s", i, lines[i], w.id, w.qty)
		}
	}
	raw, err := plan.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "- id_product: ${create_product_a.product.id_product}") ||
		!strings.Contains(string(raw), "- id_product: ${create_product_b.product.id_product}") {
		t.Fatalf("the written chain must carry both lines:\n%s", raw)
	}
}

func TestUnindexedKeyAppliesToEveryEntryAndIndexedKeysOverride(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            lines.qty:
                value: "5"
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct@a->product.id_product
            lines.1.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct@b->product.id_product
        status: draft
`)
	plan, err := contract.BuildPlan(shopCreateOrder, lib, cat, "shared")
	if err != nil {
		t.Fatal(err)
	}
	step, _ := plan.Chain.Step("create_order")
	lines := orderLines(t, step.Body)
	if len(lines) != 2 || lines[0]["qty"] != "5" || lines[1]["qty"] != "5" {
		t.Fatalf("lines.qty must reach every entry: %v", lines)
	}
	if lines[0]["id_product"] != "${create_product_a.product.id_product}" ||
		lines[1]["id_product"] != "${create_product_b.product.id_product}" {
		t.Fatalf("lines.1.id_product must override the shared key for entry 1 only: %v", lines)
	}
}

func TestContractShowAndChainNewEmitAnEntryPerIndex(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, twoLineOrder)
	m, err := cat.Lookup(shopCreateOrder)
	if err != nil {
		t.Fatal(err)
	}
	shown := contract.ForCurated(m, lib, cat).StepYAML
	if strings.Count(shown, "qty:") != 2 || !strings.Contains(shown, `qty: "2"`) || !strings.Contains(shown, `qty: "3"`) {
		t.Fatalf("contract show's CHAIN STEP must list both declared lines:\n%s", shown)
	}
	nodes, _, err := contract.ScaffoldSteps([]string{shopCreateOrder}, []string{"create_order"}, lib, cat)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "qty:") != 2 {
		t.Fatalf("chain new must scaffold both declared lines:\n%s", raw)
	}
}

func lintShop(t *testing.T, fields string) []contract.Issue {
	t.Helper()
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
`+fields+`        status: draft
`)
	out := []contract.Issue{}
	for _, i := range contract.LintLibrary(lib, cat) {
		if i.RPC == shopCreateOrder {
			out = append(out, i)
		}
	}
	return out
}

func TestLintValidatesIndexSegments(t *testing.T) {
	bad := map[string]string{
		"index on a scalar":        "            id_customer.0:\n                value: x\n",
		"index on an index":        "            lines.0.1.qty:\n                value: \"1\"\n",
		"leading zero":             "            lines.01.qty:\n                value: \"1\"\n",
		"index past the cap":       "            lines.100.qty:\n                value: \"1\"\n",
		"index then no field":      "            lines.7.bogus:\n                value: \"1\"\n",
		"index on a nested scalar": "            lines.0.qty.0:\n                value: \"1\"\n",
	}
	for name, fields := range bad {
		errs := 0
		for _, i := range lintShop(t, fields) {
			if i.IsError() {
				errs++
			}
		}
		if errs == 0 {
			t.Errorf("%s: lint must reject the key, got %v", name, lintShop(t, fields))
		}
	}
}

func TestLintAcceptsContiguousIndexesAndWarnsOnAGap(t *testing.T) {
	good := "            lines.0.qty:\n                value: \"2\"\n            lines.1.qty:\n                value: \"3\"\n"
	if issues := lintShop(t, good); len(issues) != 0 {
		t.Fatalf("contiguous indexes must lint clean, got %v", issues)
	}
	gap := "            lines.2.qty:\n                value: \"3\"\n"
	issues := lintShop(t, gap)
	if len(issues) != 1 || issues[0].IsError() || !strings.Contains(issues[0].Message, "lines.0, lines.1") {
		t.Fatalf("a gap below the highest index must warn and name the missing entries, got %v", issues)
	}
}

func TestPlanNotesAScaffoldZeroOnAFieldNotInRequired(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay)
	plan, err := contract.BuildPlan(shopCreateProduct, lib, cat, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "price_minor still carries the scaffold's numeric zero") {
		t.Fatalf("a numeric zero left by the scaffold must be noted even when the field is not required: %v", plan.Notes)
	}
	if anyNote(plan.Notes, "price_minor is required") {
		t.Fatalf("the soft note must not read as the required one: %v", plan.Notes)
	}

	lib = shopLibrary(t, strings.Replace(shopCatalogOverlay, "            sku:\n                value: SKU-1\n",
		"            sku:\n                value: SKU-1\n            price_minor:\n                value: \"1250\"\n", 1))
	plan, err = contract.BuildPlan(shopCreateProduct, lib, cat, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if anyNote(plan.Notes, "numeric zero") {
		t.Fatalf("a field the contract fills must not be noted: %v", plan.Notes)
	}
}

func TestPlanNotesANestedScaffoldZero(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, shopCatalogOverlay, shopCustomersOverlay, `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
        status: draft
`)
	plan, err := contract.BuildPlan(shopCreateOrder, lib, cat, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if !anyNote(plan.Notes, "step create_order: lines.0.qty, lines.1.qty still carry the scaffold's numeric zero") {
		t.Fatalf("required: [lines] is satisfied by the wired id, so the zero qty must still be noted: %v", plan.Notes)
	}
}

func TestPlanOfAServerStreamingRPCProbesOnlyItsToken(t *testing.T) {
	cat := catalogtest.Shop()
	p, err := contract.BuildPlanWith([]string{shopWatchOrder}, contract.NewLibrary(nil), cat, "watch", contract.PlanOptions{Auth: true, Profiles: []string{"clerk"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, st := range p.Chain.Steps {
		if st.Call != shopWatchOrder || len(st.Expect) != 1 {
			t.Fatalf("want the happy call and the two token probes, each with one assertion, got %+v", st)
		}
		got[st.ID] = st.Expect[0].Path
	}
	want := map[string]string{"watch_order": "messages.0", "watch_order_without_token": "transport.code", "watch_order_with_bad_token": "transport.code"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for id, path := range want {
		if !strings.HasPrefix(got[id], path) {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	lib := shopLibrary(t, `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/FetchOrder:
        summary: reads an order
        required: [NONE]
        needs: [shop.orders.v1.OrderService/WatchOrder]
        status: draft
`)
	if _, err := contract.BuildPlan("shop.orders.v1.OrderService/FetchOrder", lib, cat, "fetch"); err == nil ||
		!strings.Contains(err.Error(), "streaming") {
		t.Fatalf("a plan that would call a streaming rpc as setup must be refused, got %v", err)
	}
}

func TestQualityMatchesAnUnindexedRequiredAgainstIndexedFields(t *testing.T) {
	cat := catalogtest.Shop()
	lib := shopLibrary(t, `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines.id_product]
        fields:
            lines.0.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
                checked_by: app_lookup
        status: draft
`)
	for _, row := range contract.Measure(lib, cat, "").RPCs {
		if row.RPC == shopCreateOrder && len(row.UnwiredIDs) > 0 {
			t.Fatalf("lines.0.id_product sources the required lines.id_product, got unwired %v", row.UnwiredIDs)
		}
	}
}

func anyNote(notes []string, needle string) bool {
	for _, n := range notes {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}
