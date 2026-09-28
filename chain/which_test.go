package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

type whichCase struct {
	name   string
	chains []*chain.Chain
	q      chain.WhichQuery
	opts   chain.WhichOptions
	order  string
	cmds   []string
	check  func(*testing.T, []chain.WhichChain)
}

func okResponse() any { return map[string]any{"error": map[string]any{"code": "OK"}} }

func failureResponse(code any) any {
	return map[string]any{"error": map[string]any{"code": "failed_precondition",
		"details": []any{map[string]any{"app_code": code, "reason": "ObligationNotOpen"}}}}
}

func normalized(chains ...*chain.Chain) []*chain.Chain {
	for _, c := range chains {
		if err := c.Normalize(); err != nil {
			panic(err)
		}
	}
	return chains
}

func whichFixture() []*chain.Chain {
	ok := chain.Expectation{Path: "error.code", Equals: "OK"}
	return normalized(
		steps("long", st("a", "pkg.Svc/Create", nil, ok), st("b", "pkg.Svc/Create", nil, ok),
			st("boom_long", "pkg.Svc/Approve", map[string]any{"id": "${a.id}", "other": "${b.id}"}, chain.Expectation{Path: "error.details.0.app_code", Equals: 1218})),
		steps("short", st("seed", "pkg.Svc/Create", nil, ok),
			st("boom", "pkg.Svc/Approve", ref("${seed.id}"), chain.Expectation{Path: "error.code", Equals: "failed_precondition"}, chain.Expectation{Path: "error.details.0.app_code", Equals: 1218})))
}

func observed(by map[string][]chain.Observation) chain.WhichOptions {
	return chain.WhichOptions{Observations: func(name string) []chain.Observation {
		if all, ok := by["*"]; ok {
			return all
		}
		return by[name]
	}}
}

func obs(run, step, status string, resp any) chain.Observation {
	return chain.Observation{Run: run, Step: step, Status: status, Reached: status != "error" && status != "skipped", Response: resp}
}

func matchOf(t *testing.T, hits []chain.WhichChain, chainName, step string) (chain.WhichChain, chain.WhichStep) {
	t.Helper()
	for _, h := range hits {
		for _, m := range h.Matches {
			if h.Chain == chainName && m.Step == step {
				return h, m
			}
		}
	}
	t.Fatalf("no match %s/%s in %+v", chainName, step, hits)
	return chain.WhichChain{}, chain.WhichStep{}
}

func transportChains() []*chain.Chain {
	return normalized(steps("refusals", st("add_stock_batch_empty", "pkg.Stock/AddStockBatch", nil,
		chain.Expectation{Path: "transport.code", Equals: "invalid_argument"}, chain.Expectation{Path: "transport.http_status", Equals: 400})))
}

func flow() []*chain.Chain {
	ok := chain.Expectation{Path: "error.code", Equals: "OK"}
	unauth := chain.Expectation{Path: "transport.code", Equals: "unauthenticated"}
	return normalized(steps("flow", st("create", "pkg.Svc/Create", nil, ok), st("confirm", "pkg.Svc/Confirm", ref("${create.id}"), ok),
		st("fetch", "pkg.Svc/Fetch", ref("${create.id}"), ok),
		&chain.Step{ID: "cancel_without_token", Call: "pkg.Svc/Cancel", SkipAuth: true, Body: ref("${create.id}"), Expect: []chain.Expectation{unauth}},
		&chain.Step{ID: "cancel_bad_token", Call: "pkg.Svc/Cancel", Auth: chain.InvalidTokenAuth, Body: ref("${create.id}"), Expect: []chain.Expectation{unauth}}))
}

func sharedReasonFixture() []*chain.Chain {
	reason := chain.Expectation{Path: "status.details.0.reason", Equals: "CustomerNotFound"}
	return normalized(steps("customers",
		st("get_customer_missing", "shop.v1.CustomerService/GetCustomer", nil, chain.Expectation{Path: "status.details.0.app_code", Equals: 1102}, reason),
		st("create_order_unknown_customer", "shop.v1.OrderService/CreateOrder", nil, chain.Expectation{Path: "status.details.0.app_code", Equals: 1301}, reason),
		st("list_orders_unknown_customer", "shop.v1.OrderService/ListOrders", nil, reason)))
}

func TestWhich(t *testing.T) {
	ok := okResponse()
	refused := map[string]any{"transport": map[string]any{"code": "unauthenticated", "http_status": 401}}
	flowRun := observed(map[string][]chain.Observation{"*": {obs("r1", "create", "passed", ok), obs("r1", "confirm", "passed", ok), obs("r1", "fetch", "passed", ok),
		obs("r1", "cancel_without_token", "passed", refused), obs("r1", "cancel_bad_token", "passed", refused)}})
	denied := map[string]any{"status": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1603), "reason": "PermissionDenied"}}}}
	roles := normalized(steps("auth-roles",
		st("clerk_create_denied", "shop.v1.ProductService/CreateProduct", nil, chain.Expectation{Path: "status.details.0.app_code", Equals: 1603}),
		st("clerk_stock_denied", "shop.v1.StockService/AddStock", nil, chain.Expectation{Path: "status.details.0.reason", Equals: "PermissionDenied"})))
	clerk := normalized(steps("catalog-refusals",
		st("create_product_as_clerk", "pkg.Catalog/CreateProduct", nil, chain.Expectation{Path: "status.details.0.app_code", Equals: 1603}),
		st("add_stock_as_clerk", "pkg.Catalog/AddStock", nil, chain.Expectation{Path: "status.details.0.app_code", Equals: 1603})))
	clerkRun := obs("r1", "add_stock_as_clerk", "failed", map[string]any{"status": map[string]any{"code": "SUCCESS"}})
	clerkRun.Failures = []chain.ExpectResult{{Path: "status.details.0.app_code", Rule: "equals", Want: 1603}}
	failedB := obs("r2", "b", "failed", ok)
	failedB.Failures = []chain.ExpectResult{{Path: "total", Rule: "equals", Want: 4548, Got: 6250}}
	contradicted := obs("new", "boom", "failed", ok)
	contradicted.Failures = []chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "failed_precondition", Got: "OK"},
		{Path: "error.details.0.app_code", Rule: "equals", Want: float64(1218), Detail: "path not present in response"}}
	login := normalized(steps("auth", &chain.Step{ID: "login_admin", Call: "pkg.AuthService/Login", SkipAuth: true, Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}}))
	loginRun := observed(map[string][]chain.Observation{"*": {obs("r1", "login_admin", "passed", ok)}})
	loginAsRead := loginRun
	loginAsRead.ReadsOnly = func(s *chain.Step) bool { return s.Call == "pkg.AuthService/Login" }
	sized := chain.WhichOptions{SliceOf: func(c *chain.Chain, step string) (int, bool) {
		res, err := chain.Slice(c, step, chain.SliceOptions{})
		if err != nil {
			return 0, false
		}
		return len(res.Kept), true
	}}
	getProduct := normalized(steps("stock",
		st("stock_after_cancel", "pkg.Svc/GetProduct", nil, chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "qty", Equals: 10}),
		st("stock_unrun", "pkg.Svc/GetProduct", nil, chain.Expectation{Path: "error.code", Equals: "OK"}),
		st("stock_ok", "pkg.Svc/GetProduct", nil, chain.Expectation{Path: "error.code", Equals: "OK"})))
	fetchers := normalized(steps("a-small", st("fetch", "shop.v1.S/Fetch", nil, chain.Expectation{Path: "error.code", Equals: "OK"})),
		steps("b-large", st("make", "shop.v1.S/Make", nil), st("other", "shop.v1.S/Make", nil), st("fetch", "shop.v1.S/Fetch", nil, chain.Expectation{Path: "error.code", Equals: "OK"})))
	cases := []whichCase{
		{name: "a transport code assertion", chains: transportChains(), q: chain.WhichQuery{Code: "invalid_argument"},
			opts:  observed(map[string][]chain.Observation{"*": {obs("r1", "add_stock_batch_empty", "passed", chain.TransportOutcome(400, "invalid_argument", "lines must not be empty"))}}),
			order: "refusals", check: func(t *testing.T, hits []chain.WhichChain) {
				if ev := hits[0].Matches[0].Observed; ev == nil || ev.Code != "invalid_argument" || ev.Path != "transport.code" || !ev.Holds {
					t.Errorf("the transport code is read from the recorded outcome: %+v", ev)
				}
			}},
		{name: "transport ok is no fallback code", chains: normalized(steps("a", st("s", "pkg.Svc/Do", nil, chain.Expectation{Path: "error.details.0.app_code", Equals: 1218},
			chain.Expectation{Path: "transport.code", Equals: "ok"}))), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "s", "failed", chain.TransportOutcome(200, "", ""))}}), check: func(t *testing.T, hits []chain.WhichChain) {
				if ev := hits[0].Matches[0].Observed; ev == nil || ev.Code != "" {
					t.Errorf("a call answered 200 has no failure code: %+v", ev)
				}
			}},
		{name: "fresh vars in the reproduce command", chains: transportChains(), q: chain.WhichQuery{Code: "invalid_argument"},
			opts: chain.WhichOptions{FreshVars: func(*chain.Chain, string) []string { return []string{"tag", "email"} }},
			cmds: []string{"shrt chain slice refusals -step add_stock_batch_empty -var email=<fresh> -var tag=<fresh>"}},
		{name: "a failed step before a passing one", chains: getProduct, q: chain.WhichQuery{RPC: "pkg.Svc/GetProduct"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "stock_after_cancel", "failed", ok), obs("r1", "stock_ok", "passed", ok)}}),
			check: func(t *testing.T, hits []chain.WhichChain) {
				got := []string{}
				for _, m := range hits[0].Matches {
					got = append(got, m.Step)
				}
				if strings.Join(got, ",") != "stock_after_cancel,stock_ok,stock_unrun" {
					t.Errorf("got %v", got)
				}
			}},
		{name: "an observed verdict outranks an assertion", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts:  observed(map[string][]chain.Observation{"long": {obs("r1", "boom_long", "failed", failureResponse(float64(1204))), obs("r2", "boom_long", "failed", failureResponse(float64(1218)))}}),
			order: "long,short", cmds: []string{"shrt chain slice long -step boom_long -keep writes", "shrt chain slice short -step boom"},
			check: func(t *testing.T, hits []chain.WhichChain) {
				ev := hits[0].Matches[0].Observed
				if !hits[0].Observed || hits[1].Observed || ev == nil || ev.Run != "r2" || ev.Code != "1218" || ev.Path != "error.details.0.app_code" {
					t.Errorf("the newest run answering 1218 is the evidence: %+v", ev)
				}
				if hits[1].Runs != 0 || hits[1].Matches[0].Observed != nil {
					t.Errorf("no local runs, no evidence: %+v", hits[1])
				}
			}},
		{name: "a run that never reached the backend", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "boom", "error", failureResponse(float64(1218))), obs("r1", "boom_long", "error", failureResponse(float64(1218)))}}),
			check: func(t *testing.T, hits []chain.WhichChain) {
				for _, h := range hits {
					if h.Observed || !strings.HasSuffix(h.Command, h.Best) {
						t.Errorf("%s: no evidence and no pinned run: %+v", h.Chain, h)
					}
				}
			}},
		{name: "the cheaper slice first", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"}, opts: sized, order: "short,long",
			check: func(t *testing.T, hits []chain.WhichChain) {
				if hits[0].Matches[0].SliceSteps != 2 || hits[1].Matches[0].SliceSteps != 3 {
					t.Errorf("slice sizes: %d %d", hits[0].Matches[0].SliceSteps, hits[1].Matches[0].SliceSteps)
				}
			}},
		{name: "a code no chain asserts", chains: whichFixture(), q: chain.WhichQuery{Code: "9999"}, order: "-"},
		{name: "an rpc no chain calls", chains: whichFixture(), q: chain.WhichQuery{RPC: "pkg.Svc/Missing"}, order: "-"},
		{name: "both selectors intersect", chains: whichFixture(), q: chain.WhichQuery{RPC: "pkg.Svc/Create", Code: "1218"}, order: "-"},
		{name: "a passing step outranks a failed one", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts:  observed(map[string][]chain.Observation{"long": {obs("r1", "boom_long", "failed", failureResponse(float64(1218)))}, "short": {obs("r2", "boom", "passed", failureResponse(float64(1218)))}}),
			order: "short,long"},
		{name: "a code alias", chains: roles, q: chain.WhichQuery{Code: "1603", Aliases: chain.CodeAliases("1603", []any{denied})}, check: matchCount(2)},
		{name: "a reason alias", chains: roles, q: chain.WhichQuery{Code: "PermissionDenied", Aliases: chain.CodeAliases("PermissionDenied", []any{denied})}, check: matchCount(2)},
		{name: "no alias", chains: roles, q: chain.WhichQuery{Code: "1603"}, check: matchCount(1)},
		{name: "a shared reason with another code", chains: sharedReasonFixture(), q: chain.WhichQuery{Code: "1102", Aliases: []string{"CustomerNotFound"}},
			check: func(t *testing.T, hits []chain.WhichChain) {
				got := map[string]string{}
				for _, m := range hits[0].Matches {
					got[m.Step] = m.ByReason
				}
				if len(hits) != 1 || len(got) != 2 || got["get_customer_missing"] != "" || got["list_orders_unknown_customer"] != "CustomerNotFound" {
					t.Errorf("the other code's step does not match, the reason-only step says so: %+v", hits[0].Matches)
				}
			}},
		{name: "every code of a reason", chains: sharedReasonFixture(), q: chain.WhichQuery{Code: "CustomerNotFound", Aliases: []string{"1102", "1301"}}, check: matchCount(3)},
		{name: "an auth probe keeps no writes", chains: flow(), q: chain.WhichQuery{RPC: "pkg.Svc/Cancel"}, opts: flowRun, check: func(t *testing.T, hits []chain.WhichChain) {
			if len(hits) != 1 || hits[0].Command != "shrt chain slice flow -step "+hits[0].Best {
				t.Errorf("a plain closure slice: %+v", hits)
			}
			for _, m := range hits[0].Matches {
				if m.Kind != chain.WhichKindAuthProbe {
					t.Errorf("%s is an auth probe", m.Step)
				}
			}
		}},
		{name: "a write keeps writes", chains: flow(), q: chain.WhichQuery{RPC: "pkg.Svc/Confirm"}, opts: flowRun, cmds: []string{"shrt chain slice flow -step confirm -keep writes"}},
		{name: "a read is sliced plainly", chains: flow(), q: chain.WhichQuery{RPC: "pkg.Svc/Fetch"}, opts: flowRun, cmds: []string{"shrt chain slice flow -step fetch"}},
		{name: "a login is a write by its name", chains: login, q: chain.WhichQuery{RPC: "pkg.AuthService/Login"}, opts: loginRun, cmds: []string{"shrt chain slice auth -step login_admin -keep writes"}},
		{name: "the auth login is a read", chains: login, q: chain.WhichQuery{RPC: "pkg.AuthService/Login"}, opts: loginAsRead, cmds: []string{"shrt chain slice auth -step login_admin"}},
		{name: "under -rpc the newest failure first", chains: whichFixture(), q: chain.WhichQuery{RPC: "pkg.Svc/Create"},
			opts:  observed(map[string][]chain.Observation{"short": {obs("r1", "seed", "passed", ok)}, "long": {obs("r2", "a", "passed", ok), failedB}}),
			order: "long/b,short/seed", cmds: []string{"shrt chain slice long -step b -keep writes"}},
		{name: "under -rpc a failing chain above a passing one", chains: fetchers, q: chain.WhichQuery{RPC: "shop.v1.S/Fetch"},
			opts: observed(map[string][]chain.Observation{"a-small": {obs("r-a", "fetch", "failed", ok)}, "b-large": {obs("r-b", "fetch", "passed", ok)}}), order: "a-small,b-large"},
		{name: "the failing step reproduced, not a passing one", chains: clerk, q: chain.WhichQuery{Code: "1603"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "create_product_as_clerk", "passed", denied), clerkRun}}), order: "catalog-refusals/add_stock_as_clerk",
			cmds: []string{"shrt chain slice catalog-refusals -step add_stock_as_clerk -keep writes"}},
		{name: "the newest reaching run is cited even when it contradicts", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{"short": {obs("old", "boom", "passed", failureResponse(float64(1218))), contradicted}}),
			check: func(t *testing.T, hits []chain.WhichChain) {
				h, m := matchOf(t, hits, "short", "boom")
				ev := m.Observed
				if !h.Observed || ev == nil || ev.Run != "new" || ev.Code != "OK" || ev.Status != "failed" || ev.Holds || ev.Asserted != "error.details.0.app_code" ||
					ev.Path != "error.code" || len(ev.Failures) != 2 || ev.Failures[0].Path != "error.code" {
					t.Errorf("evidence: %+v", ev)
				}
			}},
		{name: "a contradicted step ranks below an asserted one", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{"short": {obs("new", "boom", "failed", ok)}}), order: "long,short",
			check: func(t *testing.T, hits []chain.WhichChain) {
				if hits[1].Matches[0].Observed == nil {
					t.Error("ranking it last must not hide its evidence")
				}
			}},
		{name: "the newest run did not reach the step", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{
				"short": {obs("r1", "seed", "passed", ok), obs("r1", "boom", "passed", failureResponse(float64(1218))), obs("r2", "seed", "failed", ok), {Run: "r2", Step: "boom", Status: "skipped"}},
				"long":  {obs("r3", "a", "failed", ok)}}),
			check: func(t *testing.T, hits []chain.WhichChain) {
				_, m := matchOf(t, hits, "short", "boom")
				if m.Observed == nil || m.Observed.Run != "r1" || m.Observed.NewerRuns != 1 || m.Newest == nil || m.Newest.Run != "r2" || m.Newest.Status != "skipped" {
					t.Errorf("older evidence, newest skipped: %+v %+v", m.Observed, m.Newest)
				}
				_, m = matchOf(t, hits, "long", "boom_long")
				if m.Observed != nil || m.Newest == nil || m.Newest.Run != "r3" || m.Newest.Status != "not in run" || m.Newest.StoppedAt != "a" || m.Newest.StoppedStatus != "failed" {
					t.Errorf("not in the newest run, which stopped at a: %+v %+v", m.Observed, m.Newest)
				}
			}},
		{name: "the newest run reached the step", chains: whichFixture(), q: chain.WhichQuery{Code: "1218"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "boom", "passed", failureResponse(float64(1218)))}}), check: func(t *testing.T, hits []chain.WhichChain) {
				_, m := matchOf(t, hits, "short", "boom")
				if m.Newest != nil || m.Observed == nil || m.Observed.NewerRuns != 0 || !m.Observed.Holds {
					t.Errorf("nothing stale: %+v %+v", m.Observed, m.Newest)
				}
			}},
		{name: "-rpc reads got from the asserted path", chains: whichFixture(), q: chain.WhichQuery{RPC: "pkg.Svc/Approve"},
			opts: observed(map[string][]chain.Observation{"*": {obs("r1", "boom", "passed", failureResponse(float64(1218)))}}), check: func(t *testing.T, hits []chain.WhichChain) {
				_, m := matchOf(t, hits, "short", "boom")
				a, ok := chain.PrimaryAssertion(m.Asserts, "")
				if m.Observed == nil || m.Observed.Code != "1218" || m.Observed.Path != "error.details.0.app_code" || !ok || a.Path != m.Observed.Path {
					t.Errorf("got from app_code: %+v %+v", m.Observed, a)
				}
			}},
	}
	for _, tc := range cases {
		hits := chain.Which(tc.chains, tc.q, tc.opts)
		if tc.order != "" {
			got := []string{}
			for _, h := range hits {
				id := h.Chain
				if strings.Contains(tc.order, "/") {
					id += "/" + h.Best
				}
				got = append(got, id)
			}
			if want := strings.TrimPrefix(tc.order, "-"); strings.Join(got, ",") != want {
				t.Errorf("%s: order %v, want %q", tc.name, got, want)
			}
		}
		for i, want := range tc.cmds {
			if i >= len(hits) || hits[i].Command != want {
				t.Errorf("%s: command %d, want %q in %+v", tc.name, i, want, hits)
			}
		}
		if tc.check != nil && len(hits) > 0 {
			tc.check(t, hits)
		} else if tc.check != nil {
			t.Errorf("%s: no hits", tc.name)
		}
	}
}

func matchCount(n int) func(*testing.T, []chain.WhichChain) {
	return func(t *testing.T, hits []chain.WhichChain) {
		if len(hits) != 1 || len(hits[0].Matches) != n {
			t.Errorf("want %d match(es) in one chain, got %+v", n, hits)
		}
	}
}

func TestCodeAliasesAndUnassertedCodes(t *testing.T) {
	denied := map[string]any{"status": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1603), "reason": "PermissionDenied"}}}}
	if got := strings.Join(chain.CodeAliases("1603", []any{denied}), ","); got != "PermissionDenied" {
		t.Errorf("1603's alias is its reason, got %q", got)
	}
	if got := strings.Join(chain.CodeAliases("permissiondenied", []any{denied}), ","); got != "1603" {
		t.Errorf("the alias works both ways, got %q", got)
	}
	confirm := normalized(steps("confirm", st("confirm_short", "shop.v1.OrderService/ConfirmOrder", nil,
		chain.Expectation{Path: "error.code", Equals: "REJECTED"}, chain.Expectation{Path: "error.details.0.reason", Equals: "InsufficientStock"})))
	opts := observed(map[string][]chain.Observation{"*": {obs("r1", "confirm_short", "passed", map[string]any{
		"error": map[string]any{"code": "REJECTED", "details": []any{map[string]any{"app_code": float64(1305), "reason": "InsufficientStock"}}}})}})
	q := chain.WhichQuery{Code: "1305"}
	if hits := chain.Which(confirm, q, opts); len(hits) != 0 {
		t.Errorf("nothing asserts 1305: %+v", hits)
	}
	seen := chain.WhichObservedUnasserted(confirm, q, opts)
	if len(seen) != 1 || seen[0].Step != "confirm_short" || seen[0].Path != "error.details.0.app_code" || seen[0].Run != "r1" || !strings.Contains(seen[0].Command, "-step confirm_short -keep writes") {
		t.Errorf("the run observed 1305 on confirm_short: %+v", seen)
	}
	if again := chain.WhichObservedUnasserted(confirm, chain.WhichQuery{Code: "1306"}, opts); len(again) != 0 {
		t.Errorf("a code no run carried is not observed: %+v", again)
	}
}
