package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func firmGate(t *testing.T, shop *fakeShop, steps string, bug func()) string {
	t.Helper()
	chdirToFakeShop(t, shop)
	inProcessGate(t)
	writeFile(t, ".shrt/chains/firm.yaml", "apiVersion: shrt/v1\nname: firm\nsteps:\n"+steps)
	bug()
	out, _ := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
	return out
}

func reproBlock(out string) string {
	_, block, _ := strings.Cut(out, "failures by suspect rpc:\n")
	return block
}

func TestGateReproSendsAnEchoedFieldAtTheBoundaryItsCutImplies(t *testing.T) {
	product := func(id, sku string) string {
		return fmt.Sprintf("    - id: %s\n      call: ProductService/CreateProduct\n      body:\n        sku: %s\n        price_minor: \"250\"\n"+
			"      expect:\n        - path: product.sku\n          equals: ${steps.%s.request.sku}\n", id, sku, id)
	}
	steps := product("short", "s-${vars.tag}") + product("mid", "sku-${vars.tag}-ab") + product("long", "sku-${vars.tag}-abcdefghijklmnopqrst") +
		product("longer", "sku-${vars.tag}-abcdefghijklmnopqrstuvwxyz")
	cut := func(over int) func(string) string {
		return func(s string) string {
			if len(s) > over {
				return s[:20]
			}
			return s
		}
	}
	for _, c := range []struct {
		name  string
		steps string
		over  int
		want  string
	}{
		{"a cut past 20 bytes: 20 bytes pass and 21 fail", steps, 20,
			"    trigger: fails with sku longer than 20 bytes (3 calls); passes with sku of up to 20 bytes (3 calls); got keeps the first 20 bytes of the sku sent (3 calls)\n    repro: "},
		{"a cut past 25 bytes: 21 bytes pass, against the split", steps, 25, "    trigger: none: sent again with sku of 21 bytes, the call passed\n    repro: "},
		{"the calls already pin 20 and 21 bytes, so nothing is sent", steps + product("twenty", "sku-${vars.tag}-abcdef") + product("twenty_one", "sku-${vars.tag}-abcdefg"), 20,
			"    trigger: fails with sku longer than 20 bytes (3 calls); passes with sku of up to 20 bytes (3 calls); got keeps the first 20 bytes of the sku sent (3 calls)\n    repro: "},
	} {
		shop := newFakeShop()
		out := firmGate(t, shop, c.steps, func() { shop.cutSku = cut(c.over) })
		block := reproBlock(out)
		if !strings.Contains(block, c.want) || strings.Count(block, "trigger") > 1 {
			t.Errorf("%s:\n%s", c.name, out)
		}
	}
}

func TestGateReproSendsAFieldEmptyTheOtherWayAndAWidePrefix(t *testing.T) {
	list := func(id, prefix, expect string) string {
		return fmt.Sprintf("    - id: %s\n      call: ProductService/ListProducts\n      body:\n        sku_prefix: %s\n      expect:\n%s", id, prefix, expect)
	}
	includes := "        - path: products\n          includes:\n            id_product: ${create_product.product.id_product}\n"
	first := func(of string) string {
		return "        - path: products.0.id_product\n          equals: ${" + of + ".product.id_product}\n"
	}
	steps := "    - id: create_product\n      call: ProductService/CreateProduct\n      body:\n        sku: sku-${vars.tag}-a\n        price_minor: \"250\"\n" +
		"    - id: create_product_2\n      call: ProductService/CreateProduct\n      body:\n        sku: sku-${vars.tag}-b\n        price_minor: \"250\"\n" +
		list("list_all", `""`, includes) + list("list_a", "${steps.create_product.request.sku}", first("create_product")) +
		list("list_b", "${steps.create_product_2.request.sku}", first("create_product_2"))
	empty := func(p any, _ int) bool { s, _ := p.(string); return s == "" }
	for _, c := range []struct {
		name    string
		steps   string
		nothing func(any, int) bool
		want    string
		probes  int
	}{
		{"an empty or absent prefix lists nothing, a one-byte prefix lists every product", steps, empty,
			"    trigger: fails with sku_prefix empty or absent (2 calls); passes with sku_prefix set (3 calls)\n    repro: ", 2},
		{"more than one match lists nothing, so the wide prefix fails too", steps, func(_ any, n int) bool { return n > 1 },
			"    trigger: none: sent again with sku_prefix \"s\", the call failed\n    repro: ", 2},
		{"two calls on each side already, so nothing is sent", steps + list("list_none", `""`, includes), empty, "    repro: ", 0},
	} {
		shop := newFakeShop()
		out := firmGate(t, shop, c.steps, func() { shop.listNothing = c.nothing })
		probes := 0
		for _, p := range shop.listed {
			if p == nil || p == "s" {
				probes++
			}
		}
		if !strings.Contains(reproBlock(out), c.want) || probes != c.probes {
			t.Errorf("%s: %d probe(s):\n%s", c.name, probes, out)
		}
	}
	product := func(id, name, price string) string {
		return fmt.Sprintf("    - id: %s\n      call: ProductService/CreateProduct\n      body:\n        sku: %s-${vars.tag}\n        name: %s\n        price_minor: %q\n"+
			"      expect:\n        - path: status.code\n          equals: SUCCESS\n", id, id, name, price)
	}
	shop := newFakeShop()
	out := firmGate(t, shop, product("unnamed", `""`, "250")+product("named_a", "A", "100")+product("named_b", "B", "200"), func() {
		shop.refuseProduct = func(body map[string]any) bool { return body["name"] == nil || body["name"] == "" }
	})
	if strings.Contains(out, "    trigger: fails with name empty or absent (1 call); passes with name set (2 calls)\n") ||
		!strings.Contains(reproBlock(out), "\n    trigger: fails with name empty or absent (2 calls); passes with name set (2 calls)\n") {
		t.Errorf("the call sent again without its name copies its price, so the price does not split the calls too:\n%s", out)
	}
}

func TestGateReproSendsAnUnknownIDReadWithAnotherIDOfItsShape(t *testing.T) {
	steps := "    - id: create_product\n      call: ProductService/CreateProduct\n      body:\n        sku: sku-${vars.tag}\n        price_minor: \"250\"\n" +
		"    - id: get_unknown\n      call: ProductService/GetProduct\n      body:\n        id_product: ${create_product.product.id_product}-unknown\n" +
		"      expect:\n        - path: status.code\n          not_equal: SUCCESS\n"
	for _, c := range []struct {
		name string
		ok   func(*fakeShop) func(string) bool
		want string
	}{
		{"every unknown id answers SUCCESS", func(*fakeShop) func(string) bool { return func(string) bool { return true } },
			`    trigger: fails for every unknown id_product sent \(2 calls: prd-\d{4}-unknown, prd-\d{4}-unknown\)\n    repro: `},
		{"only a real id with -unknown appended answers SUCCESS", func(s *fakeShop) func(string) bool {
			return func(id string) bool { return s.products[strings.TrimSuffix(id, "-unknown")] != nil }
		}, `    trigger: fails for the unknown id_product sent \(1 call: prd-\d{4}-unknown\), not for another of its shape no record has \(1 call: prd-\d{4}-unknown\)\n    repro: `},
	} {
		shop := newFakeShop()
		out := firmGate(t, shop, steps, func() { shop.unknownOK = c.ok(shop) })
		if !regexp.MustCompile(c.want).MatchString(reproBlock(out)) {
			t.Errorf("%s:\n%s", c.name, out)
		}
	}
}

type thingShop struct {
	mu     sync.Mutex
	owner  map[string]string
	refuse func(caller, owner string) bool
}

func (s *thingShop) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		caller, out := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer tok-"), map[string]any{"error": map[string]any{"code": "OK"}}
		switch id := fmt.Sprint(body["id"]); {
		case strings.HasSuffix(r.URL.Path, "/Login"):
			out["access_token"], out["expires_at"] = "tok-"+fmt.Sprint(body["username"]), "4102444800"
		case strings.HasSuffix(r.URL.Path, "/Create"):
			id = fmt.Sprintf("thing-%d", len(s.owner)+1)
			s.owner[id], out["id"] = caller, id
		case s.owner[id] == "":
			out["error"] = map[string]any{"code": "NOT_FOUND"}
		case s.refuse(caller, s.owner[id]):
			out["error"] = map[string]any{"code": "DENIED"}
		default:
			out["id"] = id
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestGateReproSendsAProfilesCallOnARecordItCreatedItself(t *testing.T) {
	fetch := func(id, as, of string) string {
		return fmt.Sprintf("    - id: %s\n      call: ThingService/Fetch\n      auth: %s\n      body:\n        id: ${%s.id}\n      expect:\n        - path: error.code\n          equals: OK\n", id, as, of)
	}
	create := func(id string) string {
		return fmt.Sprintf("    - id: %s\n      call: ThingService/Create\n      body:\n        name: widget\n        kind: KIND_A\n", id)
	}
	for _, c := range []struct {
		name   string
		refuse func(caller, owner string) bool
		want   string
	}{
		{"a clerk is refused every thing", func(caller, _ string) bool { return caller == "clerk" },
			"    trigger: fails when Fetch is sent as clerk (2 calls; 1 on records created as default); passes as default (2 calls)\n    repro: "},
		{"a clerk is refused only what another profile created", func(caller, owner string) bool { return caller == "clerk" && owner != "clerk" },
			"    trigger: none: sent again as clerk on what clerk created, the call passed\n    repro: "},
	} {
		shop := &thingShop{owner: map[string]string{}, refuse: c.refuse}
		srv := shop.server()
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		t.Setenv("FIRM_ADMIN", "staff")
		t.Setenv("FIRM_CLERK", "clerk")
		login := func(env string) string {
			return "call: shrt.test.v1.AuthService/Login\n    body:\n        username: ${env." + env + "}\n        password: x\n    token_path: access_token\n    expires_path: expires_at\n"
		}
		writeFile(t, ".shrt/config.yaml", "target:\n    base_url: "+srv.URL+"\ndescriptor:\n    file: .shrt/descriptor.binpb\nauth:\n    "+login("FIRM_ADMIN")+
			"    profiles:\n        clerk:\n            "+strings.ReplaceAll(login("FIRM_CLERK"), "\n    ", "\n            ")+
			"paths:\n    chains: .shrt/chains\n    runs: .shrt/runs\n    safespots: .shrt/safespots\nconventions:\n    envelope_path: error.code\n    envelope_ok: OK\n")
		removeFile(t, ".shrt/chains/cli-thing-flow.yaml")
		inProcessGate(t)
		writeFile(t, ".shrt/chains/things.yaml", "apiVersion: shrt/v1\nname: things\nsteps:\n"+create("create")+fetch("fetch_clerk", "clerk", "create")+
			create("create_2")+fetch("fetch_2", "default", "create_2")+create("create_3")+fetch("fetch_3", "default", "create_3"))
		out, _ := shrtOut(t, "gate", "-repro", "-no-session-check", "-hollow-baseline", "")
		if !strings.Contains(reproBlock(out), c.want) {
			t.Errorf("%s:\n%s", c.name, out)
		}
	}
}
