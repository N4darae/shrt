package contract_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const shortRPCProducer = `
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        exports: {product.id_product: the new product id}
        fields: {sku: {value: SKU-1}, price_minor: {value: "100"}}
        aliases:
            first:
                fields: {sku: {value: SKU-FIRST}}
        status: draft`

func TestPlanShortRPCNamesMatchFullNames(t *testing.T) {
	for _, tc := range []struct {
		name, target, rpc, rpcs, field, value string
	}{
		{"from", shopAddStock, shopCreateProduct, shortRPCProducer + `
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        fields: {id_product: {from: "RPC->product.id_product"}, qty: {value: "5"}}
        status: draft`, "id_product", "${create_product.product.id_product}"},
		{"same_as", "shop.catalog.v1.ProductService/ListProducts", shopCreateProduct, shortRPCProducer + `
    shop.catalog.v1.ProductService/ListProducts:
        summary: lists products
        fields: {sku_prefix: {same_as: "RPC->sku"}}
        status: draft`, "sku_prefix", "${vars.create_product_sku}"},
		{"before", shopCreateProduct, shopCreateProduct, shortRPCProducer + `
    shop.customers.v1.CustomerService/CreateCustomer:
        summary: adds a customer
        fields: {email: {value: a@example.com}}
        before: [RPC]
        status: draft`, "sku", "SKU-1"},
		{"key", shopAddStock, shopAddStock, `
    RPC:
        summary: adds stock
        fields: {id_product: {value: P-1}, qty: {value: "5"}}
        status: draft`, "id_product", "P-1"},
		{"needs", shopAddStock, shopCreateProduct, shortRPCProducer + `
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        needs: [RPC@first]
        fields: {id_product: {value: P-1}, qty: {value: "5"}}
        status: draft`, "id_product", "P-1"},
		{"alias_from", shopAddStock + "@first", shopCreateProduct, shortRPCProducer + `
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        fields: {qty: {value: "5"}}
        aliases:
            first:
                fields: {id_product: {from: "RPC@first->product.id_product"}}
        status: draft`, "id_product", "${create_product_first.product.id_product}"},
		{"alias_same_as_legacy", "shop.catalog.v1.ProductService/ListProducts", shopCreateProduct, shortRPCProducer + `
    shop.catalog.v1.ProductService/ListProducts:
        summary: lists products
        fields: {sku_prefix: {same_as: "RPC@first#sku"}}
        status: draft`, "sku_prefix", "${vars.create_product_first_sku}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cat := catalogtest.Shop()
			build := func(rpc string) *contract.Plan {
				t.Helper()
				lib := shopLibrary(t, "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:"+
					strings.ReplaceAll(tc.rpcs, "RPC", rpc)+"\n")
				p, err := contract.BuildPlan(tc.target, lib, cat, "short-names")
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			full := build(tc.rpc)
			found := false
			for _, st := range full.Chain.Steps {
				if st.Call == strings.Split(tc.target, "@")[0] {
					found = true
					if got := st.Body[tc.field]; got != tc.value {
						t.Fatalf("full-name control: %s %s = %v, want %s", st.ID, tc.field, got, tc.value)
					}
					break
				}
			}
			if !found {
				t.Fatalf("full-name control is missing the target %s", tc.target)
			}
			if tc.name == "before" && (len(full.Order) != 2 || full.Order[0] != shopCreateCustomer) {
				t.Fatalf("before must pull the customer producer in first: %v", full.Order)
			}
			if tc.name == "needs" && full.Order[0] != shopCreateProduct+"@first" {
				t.Fatalf("needs must pull in the named alias: %v", full.Order)
			}
			service, method, _ := strings.Cut(tc.rpc, "/")
			service = service[strings.LastIndex(service, ".")+1:]
			for _, rpc := range []string{service + "/" + method, method, "/" + tc.rpc} {
				t.Run(rpc, func(t *testing.T) {
					short := build(rpc)
					if !reflect.DeepEqual(full.Order, short.Order) || !reflect.DeepEqual(full.Chain, short.Chain) ||
						!reflect.DeepEqual(full.Notes, short.Notes) {
						t.Errorf("short name changes the plan:\nfull: %v\nshort: %v\nfull chain: %+v\nshort chain: %+v\nnotes: %v / %v",
							full.Order, short.Order, full.Chain, short.Chain, full.Notes, short.Notes)
					}
				})
			}
		})
	}
}

func TestPlanShortRPCKeysKeepAliasesAndOriginalLibrary(t *testing.T) {
	cat := catalogtest.Shop()
	doc := "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:" + strings.ReplaceAll(shortRPCProducer, shopCreateProduct, "ProductService/CreateProduct") + `
    StockService/AddStock:
        summary: adds stock
        needs: [ProductService/CreateProduct@first]
        fields: {qty: {value: "5"}}
        aliases:
            first:
                fields: {id_product: {from: "ProductService/CreateProduct@first->product.id_product"}}
        status: draft
`
	lib := shopLibrary(t, doc)
	o := lib.Overlays[0]
	before, err := o.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	lint := contract.LintLibrary(lib, cat)
	if node, _, err := contract.ResolveTarget("StockService/AddStock@first", lib, cat); err != nil || node != shopAddStock+"@first" {
		t.Fatalf("the CLI's target resolver must find aliases under a short key: %s, %v", node, err)
	}
	for i := 0; i < 2; i++ {
		p, err := contract.BuildPlan("StockService/AddStock@first", lib, cat, "aliases")
		if err != nil {
			t.Fatal(err)
		}
		st, ok := p.Chain.Step("add_stock_first")
		if !ok || st.Body["id_product"] != "${create_product_first.product.id_product}" {
			t.Fatalf("short keys and aliases must wire the selected producer: %+v", st)
		}
	}
	after, err := o.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !reflect.DeepEqual(lint, contract.LintLibrary(lib, cat)) {
		t.Fatal("planning must not rewrite source overlays or suppress lint's short-key warning")
	}
	if _, ok := lib.Get("StockService/AddStock"); !ok {
		t.Fatal("planning changed the caller's library index")
	}
	if _, ok := lib.Get(shopAddStock); ok {
		t.Fatal("the normalized index must be private to the plan")
	}
}

func TestPlanShortRPCNamesStillRejectInvalidReferences(t *testing.T) {
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(catalogtest.ShopDescriptor(), fds); err != nil {
		t.Fatal(err)
	}
	product := fds.File[1]
	fds.File = append(fds.File, &descriptorpb.FileDescriptorProto{
		Name:       proto.String("other.proto"),
		Package:    proto.String("other.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{product.GetName()},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name:   proto.String("ProductService"),
			Method: []*descriptorpb.MethodDescriptorProto{proto.Clone(product.Service[0].Method[0]).(*descriptorpb.MethodDescriptorProto)},
		}},
	})
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, rpc := range []string{"CreateProduct", "ProductService/CreateProduct", "MissingService/CreateProduct"} {
		for _, edge := range []string{"needs: [RPC]", `fields: {id_product: {from: "RPC->product.id_product"}}`, `fields: {qty: {same_as: "RPC->price_minor"}}`} {
			t.Run(rpc+"/"+edge, func(t *testing.T) {
				lib := shopLibrary(t, "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:"+shortRPCProducer+`
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        `+strings.ReplaceAll(edge, "RPC", rpc)+`
        status: draft
`)
				_, err := contract.BuildPlan(shopAddStock, lib, cat, "invalid")
				if err == nil || !strings.Contains(err.Error(), rpc) {
					t.Fatalf("do not silently bind unknown or ambiguous references: %v", err)
				}
				if !strings.HasPrefix(rpc, "Missing") && (!strings.Contains(err.Error(), "ambiguous") ||
					!strings.Contains(err.Error(), shopCreateProduct) || !strings.Contains(err.Error(), "other.v1.ProductService/CreateProduct")) {
					t.Fatalf("ambiguity must retain both catalog candidates: %v", err)
				}
			})
		}
	}
}

func TestScaffoldShortRPCNamesMatchFullNames(t *testing.T) {
	cat := catalogtest.Shop()
	doc := "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:" + shortRPCProducer + `
    shop.catalog.v1.StockService/AddStock:
        summary: adds stock
        fields: {id_product: {from: "shop.catalog.v1.ProductService/CreateProduct->product.id_product"}, qty: {value: "5"}}
        status: draft
`
	full := shopLibrary(t, doc)
	short := shopLibrary(t, strings.ReplaceAll(strings.ReplaceAll(doc, shopCreateProduct, "CreateProduct"), shopAddStock, "AddStock"))
	refs := []string{shopCreateProduct, shopAddStock}
	ids := []string{"product", "stock"}
	fullNodes, fullNotes, err := contract.ScaffoldSteps(refs, ids, full, cat)
	if err != nil {
		t.Fatal(err)
	}
	shortNodes, shortNotes, err := contract.ScaffoldSteps(refs, ids, short, cat)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fullNodes, shortNodes) || !reflect.DeepEqual(fullNotes, shortNotes) {
		t.Error("step scaffolding must use the same contracts and field wiring for short names")
	}
	fullChain, fullNotes, err := contract.ScaffoldChain("flow", "flow", refs, ids, full, cat)
	if err != nil {
		t.Fatal(err)
	}
	shortChain, shortNotes, err := contract.ScaffoldChain("flow", "flow", refs, ids, short, cat)
	if err != nil {
		t.Fatal(err)
	}
	if string(fullChain) != string(shortChain) || !reflect.DeepEqual(fullNotes, shortNotes) {
		t.Fatalf("chain scaffolding differs:\n%s\n%s\nnotes: %v / %v", fullChain, shortChain, fullNotes, shortNotes)
	}
}

func TestPlanShortRPCKeyCollisionIsAnError(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(fmt.Sprint("separate_overlays=", separate), func(t *testing.T) {
			base := "apiVersion: shrt/contract/v1\ndomain: catalog\nrpcs:"
			other := strings.ReplaceAll(shortRPCProducer, shopCreateProduct, "ProductService/CreateProduct")
			docs := []string{base + shortRPCProducer + other}
			if separate {
				docs = []string{base + shortRPCProducer, base + other}
			}
			_, err := contract.BuildPlan(shopCreateProduct, shopLibrary(t, docs...), catalogtest.Shop(), "collision")
			if err == nil || !strings.Contains(err.Error(), shopCreateProduct) || !strings.Contains(err.Error(), `"ProductService/CreateProduct"`) {
				t.Fatalf("a short/full-key clash must not silently select either contract: %v", err)
			}
		})
	}
}
