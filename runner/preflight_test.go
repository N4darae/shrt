package runner_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func TestARunIsRefusedBeforeAnythingIsSent(t *testing.T) {
	t.Setenv("SHRT_TEST_UNSET_7F", "x")
	os.Unsetenv("SHRT_TEST_UNSET_7F")
	const sent = "nothing was sent"
	withAllowFail := func(mutate func(*chain.Step)) func() *chain.Chain {
		return func() *chain.Chain {
			c := testChain()
			c.Steps[0].AllowFail = true
			mutate(c.Steps[0])
			return c
		}
	}
	twoCreates := func(expect ...chain.Expectation) func() *chain.Chain {
		return func() *chain.Chain {
			return flow(
				&chain.Step{ID: "first", Call: "ThingService/Create", Body: thing("widget"), Export: map[string]string{"alias_only": "id"}, Expect: okExpect()},
				step("second", "ThingService/Create", thing("widget"), expect...))
		}
	}
	createThen := func(later *chain.Step) func() *chain.Chain {
		return func() *chain.Chain {
			return flow(step("create", "ThingService/Create", thing("widget"), okExpect()...), later)
		}
	}
	handAuth := func(skip bool, header string) func() *chain.Chain {
		return func() *chain.Chain {
			s := step("create", "ThingService/Create", thing("widget"), okExpect()...)
			s.SkipAuth, s.Headers = skip, map[string]string{header: "Bearer clerk-token"}
			return flow(step("first", "ThingService/Create", thing("widget"), okExpect()...), s)
		}
	}
	keptRedGot := func(got string) func() *chain.Chain {
		return func() *chain.Chain {
			return keptRedChain(5, 7, chain.Pin{Step: "second", Path: "qty_on_hand", Got: strp(got)})
		}
	}
	for _, tc := range []struct {
		name  string
		cfg   func(*config.Config)
		shop  bool
		chain func() *chain.Chain
		opts  runner.Options
		dry   bool
		want  []string
		not   string
		once  string
	}{
		{name: "unknown request field", chain: func() *chain.Chain {
			c := testChain()
			c.Steps[0].Body["not_a_field"] = "x"
			return c
		}, want: []string{sent, "not_a_field"}},
		{name: "allow_fail with an rpc that does not exist", chain: withAllowFail(func(s *chain.Step) { s.Call = "ThingService/NoSuchRpc" }), want: []string{sent, "NoSuchRpc"}},
		{name: "allow_fail with an unset env var", chain: withAllowFail(func(s *chain.Step) { s.Body["name"] = "${env.SHRT_TEST_UNSET_7F}" }), want: []string{sent}},
		{name: "allow_fail with a field the proto rejects", chain: withAllowFail(func(s *chain.Step) { s.Body["not_a_real_field"] = 1 }), want: []string{sent}},
		{name: "a later login reads an unset env var", cfg: func(c *config.Config) {
			c.Auth.Body = map[string]any{"username": "staff", "password": "${env.SHRT_TEST_UNSET_7F}"}
		}, chain: func() *chain.Chain {
			probe := step("probe", "ThingService/Create", thing("widget"), okExpect()...)
			probe.SkipAuth = true
			return flow(probe, step("create", "ThingService/Create", thing("widget"), okExpect()...))
		}, want: []string{sent, "SHRT_TEST_UNSET_7F"}},
		{name: "unknown rpc", chain: createThen(step("typo", "ThingService/Craete", map[string]any{"name": "widget"}, okExpect()...)), want: []string{"typo", "Craete"}},
		{name: "a field the response lacks", chain: createThen(step("fetch", "ThingService/Fetch", byID("${create.idd}"), okExpect()...)), want: []string{sent, "idd"}},
		{name: "a request path the earlier request lacks", chain: createThen(step("fetch", "ThingService/Fetch", byID("${steps.create.request.nmae}"), okExpect()...)), want: []string{sent, "nmae"}},
		{name: "a reference to no step", chain: createThen(step("fetch", "ThingService/Fetch", byID("${craete.id}"), okExpect()...)), want: []string{sent, "craete", `did you mean "create"`}},
		{name: "an unset env var in a body", chain: createThen(step("fetch", "ThingService/Fetch", byID("${env.SHRT_TEST_UNSET_7F}"), okExpect()...)), want: []string{sent, "SHRT_TEST_UNSET_7F"}},
		{name: "a step named by one word of the reference", chain: func() *chain.Chain {
			return flow(step("customer", "ThingService/Create", thing("widget"), okExpect()...),
				step("fetch", "ThingService/Create", map[string]any{"name": "${create_customer.id}", "kind": "KIND_A", "meta": map[string]any{"trace_id": "${create_customer.id}"}}, okExpect()...))
		}, want: []string{`did you mean "customer"?`}, once: "[fetch]"},
		{name: "an unknown field in a later request", chain: createThen(step("create_bad", "ThingService/Create", map[string]any{"name": "widget", "kind": "KIND_A", "bogus": 1}, okExpect()...)), want: []string{sent, "[create_bad]"}},
		{name: "an invalid enum in a later request", chain: createThen(step("create_bad", "ThingService/Create", map[string]any{"name": "widget", "kind": "KIND_NOPE"}, okExpect()...)), want: []string{sent, "[create_bad]"}},
		{name: "every chain error at once", chain: func() *chain.Chain {
			return flow(step("create", "ThingService/Create", map[string]any{"name": "${vars.batch}", "kind": "KIND_A"}, okExpect()...),
				step("typo", "ThingService/Craete", map[string]any{"name": "widget"}, okExpect()...),
				step("fetch", "ThingService/Fetch", byID("${craete.id}"), okExpect()...),
				step("bad", "ThingService/Create", map[string]any{"name": "widget", "kind": "KIND_A", "bogus": 1}, okExpect()...))
		}, dry: true, want: []string{sent, "4 chain errors", "-var batch=...", "[typo] unknown rpc", `[fetch] ${craete.id} names no step of this chain (did you mean "create"?)`, `[bad] request: "bogus"`}},
		{name: "a hand-written authorization header", chain: handAuth(false, "authorization"), want: []string{sent, "uthorization"}},
		{name: "a hand-written authorization header with skip_auth", chain: handAuth(true, "Authorization"), want: []string{sent, "uthorization"}},
		{name: "an unresolvable header", chain: func() *chain.Chain {
			c := testChain()
			c.Steps[0].Headers = map[string]string{"X-Bad": "${nope.id}"}
			return c
		}, want: []string{"X-Bad"}},
		{name: "an export read before it is produced", chain: func() *chain.Chain {
			create := step("create", "ThingService/Create", thing("w"), okExpect()...)
			create.Export = map[string]string{"later_id": "id"}
			return flow(step("fetch", "ThingService/Fetch", byID("${exports.later_id}"), okExpect()...), create)
		}, want: []string{sent, "${exports.later_id}", "exported by create at step 2", "runs later"}},
		{name: "a step read before it runs", chain: func() *chain.Chain {
			return flow(step("fetch", "ThingService/Fetch", byID("${create.id}"), okExpect()...), step("create", "ThingService/Create", thing("w"), okExpect()...))
		}, want: []string{sent, `"create"`, "does not run before this step"}},
		{name: "an unsupplied var", chain: func() *chain.Chain { return varChain("batch") }, dry: true, want: []string{"${vars.batch}", "-var batch=..."}},
		{name: "an expectation naming an undeclared var", chain: func() *chain.Chain {
			return flow(step("create", "ThingService/Create", thing("w"), chain.Expectation{Path: "id", Equals: "${vars.never_set}"}))
		}, want: []string{sent, "${vars.never_set}"}},
		{name: "a var reference with arithmetic", chain: func() *chain.Chain {
			return flow(step("create", "ThingService/Create", map[string]any{"name": "${vars.base + 3}", "kind": "KIND_A"}))
		}, opts: runner.Options{Vars: map[string]any{"base": 1}}, want: []string{chain.NoArithmetic}, not: "-var base + 3"},
		{name: "a dry run expectation reading a field no response carries", chain: twoCreates(chain.Expectation{Path: "id", Equals: "${first.field_that_cannot_exist}"}),
			opts: runner.Options{DryRun: true}, want: []string{sent, "field_that_cannot_exist", "[second]"}},
		{name: "a dry run expectation reading an export alias as a field", chain: twoCreates(chain.Expectation{Path: "id", Equals: "${first.alias_only}"}),
			opts: runner.Options{DryRun: true}, want: []string{"alias_only"}},
		{name: "a dry run names each bad reference once", chain: func() *chain.Chain {
			return flow(step("first", "ThingService/Create", thing("widget"), okExpect()...),
				step("second", "ThingService/Fetch", byID("${first.idd}"), okExpect()...),
				step("third", "ThingService/Fetch", byID("${first.idd}"), okExpect()...))
		}, opts: runner.Options{DryRun: true}, want: []string{"[second, third] ${first.idd}", `did you mean "id"?`}, once: "${first.idd}"},
		{name: "auth: invalid with no auth configured", cfg: func(c *config.Config) { c.Auth = nil }, chain: func() *chain.Chain {
			s := step("bad_token", "ThingService/Fetch", byID("thing-1"), unauthenticatedExpect()...)
			s.Auth = transport.InvalidTokenProfile
			return flow(s)
		}, want: []string{"invalid"}},
		{name: "an unknown auth profile", cfg: func(c *config.Config) { c.Auth.Profiles = partnerConfig("").Auth.Profiles }, chain: func() *chain.Chain {
			s := step("mine", "PartnerService/FetchMine", byID("p-1"), okExpect()...)
			s.Auth = "prtner"
			return flow(s)
		}, want: []string{"prtner", "partner"}},
		{name: "an export named like a step", shop: true, dry: true, chain: func() *chain.Chain {
			return flow(&chain.Step{ID: "made", Call: "shop.catalog.v1.ProductService/CreateProduct", Body: map[string]any{"sku": "a"}, Export: map[string]string{"made": "product.id_product"}},
				step("read", "shop.catalog.v1.ProductService/GetProduct", map[string]any{"id_product": "${made}"}))
		}, want: []string{sent, `export "made" has the name of step "made"`}},
		{name: "a var carrying a reference", shop: true, dry: true, chain: func() *chain.Chain {
			c := flow(step("add", "shop.catalog.v1.StockService/AddStock", map[string]any{"id_product": "${vars.idem}", "qty": "1"}, chain.Expectation{Path: "status.code", Equals: "SUCCESS"}))
			c.Vars = map[string]any{"idem": "idem-${vars.tag}"}
			return c
		}, want: []string{sent, `var "idem" holds ${vars.tag}`}},
		{name: "a kept_red got on a redacted path", shop: true, chain: keptRedGot("5"), opts: runner.Options{Redact: []string{"**.qty_on_hand"}}, want: []string{sent, "redact", "qty_on_hand"}},
		{name: "a kept_red got of <redacted>", shop: true, chain: keptRedGot("<redacted>"), opts: runner.Options{Redact: []string{"**.qty_on_hand"}}, want: []string{sent, "redact", "qty_on_hand"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r *runner.Runner
			var count func() int
			if tc.shop {
				srv := newShopServer(t, map[string]any{"AddStock": map[string]any{"status": map[string]any{"code": "SUCCESS"}, "qtyOnHand": "5"}})
				r, count = srv.runner(), func() int { return len(srv.sent()) }
			} else {
				srv := newFakeServer()
				t.Cleanup(srv.Close)
				cfg := testConfig(srv.URL)
				if tc.cfg != nil {
					tc.cfg(cfg)
				}
				deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
				if err != nil {
					t.Fatal(err)
				}
				r = &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, Auth: deps.Bindings, AuthRoute: deps.Route}
				count = srv.sentCount
			}
			dries := []bool{tc.opts.DryRun}
			if tc.dry {
				dries = []bool{false, true}
			}
			for _, dry := range dries {
				opts := tc.opts
				opts.DryRun = dry
				_, err := r.Run(context.Background(), normalized(t, tc.chain()), opts)
				if err == nil {
					t.Fatalf("dry=%v: want a refusal", dry)
				}
				for _, w := range tc.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("dry=%v: the refusal lacks %q: %v", dry, w, err)
					}
				}
				if (tc.not != "" && strings.Contains(err.Error(), tc.not)) || (tc.once != "" && strings.Count(err.Error(), tc.once) != 1) {
					t.Errorf("dry=%v: %v", dry, err)
				}
			}
			if n := count(); n != 0 {
				t.Fatalf("%d request(s) reached the backend before the refusal", n)
			}
		})
	}
}

func TestAClientStreamingRPCIsRefusedBeforeSendingInRunAndDryRun(t *testing.T) {
	cat := catalogtest.Rich()
	method, err := cat.Lookup("OrderService/UploadOrders")
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	r := bareRunner(t, func(w http.ResponseWriter, r *http.Request) { hits++ }, nil)
	r.Catalog = cat
	for _, dry := range []bool{false, true} {
		c := normalized(t, flow(&chain.Step{ID: "upload", Call: "OrderService/UploadOrders", SkipAuth: true,
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "http_415"}}}))
		_, err := r.Run(context.Background(), c, runner.Options{DryRun: dry})
		if err == nil || !strings.Contains(err.Error(), method.StreamRefusal()) || !strings.Contains(err.Error(), "nothing was sent") {
			t.Errorf("dry=%v: want lint's StreamRefusal sentence and that nothing was sent, got %v", dry, err)
		}
	}
	if hits != 0 {
		t.Fatalf("the streaming step reached the server %d time(s)", hits)
	}
}

func TestARunThatLooksUnresolvedButIsNotStillRuns(t *testing.T) {
	t.Setenv("SHRT_TEST_UNSET_8A", "x")
	os.Unsetenv("SHRT_TEST_UNSET_8A")
	srv := newFakeServer()
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Auth.Body = map[string]any{"username": "staff", "password": "${env.SHRT_TEST_UNSET_8A}"}
	probe := step("probe", "ThingService/Create", thing("widget"), chain.Expectation{Path: "transport.code", Equals: "unauthenticated"})
	probe.SkipAuth = true
	if rec := run(t, buildRunner(t, cfg, catalogtest.New()), normalized(t, flow(probe)), runner.Options{}); !rec.Passed() {
		t.Fatalf("no step of this chain logs in, so the login's env is not needed: %s", rec.Failure)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		rec := run(t, newRunner(t, srv), varChain("tag"), runner.Options{})
		if tag, _ := rec.Vars["tag"].(string); !rec.Passed() || tag == "" || seen[tag] {
			t.Fatalf("run %d: an undeclared ${vars.tag} gets a fresh value recorded in the run, got %v %s", i, rec.Vars, rec.Failure)
		} else {
			seen[tag] = true
		}
	}
	if rec := run(t, newRunner(t, srv), varChain("tag"), runner.Options{Vars: map[string]any{"tag": "given"}}); !rec.Passed() || rec.Vars["tag"] != "given" {
		t.Fatalf("-var tag wins over the fresh one: %v", rec.Vars)
	}
	declared := varChain("batch")
	declared.Vars = map[string]any{"batch": "default"}
	if rec := run(t, newRunner(t, srv), declared, runner.Options{}); !rec.Passed() {
		t.Fatalf("declared var: %s", rec.Failure)
	}
	late := normalized(t, flow(
		&chain.Step{ID: "create", Call: "ThingService/Create", Body: thing("widget"), Export: map[string]string{"thing_id": "id"}, Expect: okExpect()},
		step("fetch", "ThingService/Fetch", byID("${thing_id}"), okExpect()...)))
	if rec := run(t, newRunner(t, srv), late, runner.Options{}); !rec.Passed() {
		t.Fatalf("a request built from an earlier response is validated with synthetic values: %s", rec.Failure)
	}
	shop := newShopServer(t, nil)
	idem := flow(step("add", "shop.catalog.v1.StockService/AddStock", map[string]any{"id_product": "${vars.idem}", "qty": "1"}, chain.Expectation{Path: "status.code", Equals: "SUCCESS"}))
	idem.Vars = map[string]any{"idem": "idem-${vars.tag}"}
	if rec := run(t, shop.runner(), normalized(t, idem), runner.Options{Vars: map[string]any{"idem": "idem-1"}}); rec.Status != runner.StatusPassed {
		t.Fatalf("a -var that replaces the value leaves nothing unresolved: %+v", rec)
	}
}

func TestADryRunSendsNoTrafficAndStillResolvesReferences(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	for _, c := range []*chain.Chain{testChain(), normalized(t, flow(
		&chain.Step{ID: "first", Call: "ThingService/Create", Body: thing("widget"), Export: map[string]string{"alias_only": "id"}, Expect: okExpect()},
		step("second", "ThingService/Create", thing("widget"),
			chain.Expectation{Path: "error.code", Equals: "OK"},
			chain.Expectation{Path: "id", NotEqual: "${steps.first.response.id}"},
			chain.Expectation{Path: "error.code", Equals: "${first.error.code}"})))} {
		rec := run(t, newRunner(t, srv), c, runner.Options{DryRun: true})
		if !rec.Passed() || len(rec.Steps) != 2 {
			t.Fatalf("a dry run visits every step and a reference the synthesized response satisfies stays clean: %s", rec.Failure)
		}
	}
	for _, path := range srv.calls {
		if path != "/shrt.test.v1.AuthService/Login" {
			t.Fatalf("dry run sent traffic to %s", path)
		}
	}
}

func varChain(name string) *chain.Chain {
	login := step("login", "AuthService/Login", map[string]any{"username": "staff", "password": "secret"}, okExpect()...)
	login.SkipAuth = true
	c := &chain.Chain{Name: "tagged", Steps: []*chain.Step{login,
		step("create", "ThingService/Create", map[string]any{"name": "w-${vars." + name + "}", "kind": "KIND_A"}, okExpect()...)}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func unauthenticatedExpect() []chain.Expectation {
	return []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}, {Path: "transport.http_status", Equals: 401}}
}
