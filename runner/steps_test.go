package runner_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

func loginChain(expect ...chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "login-probe", Steps: []*chain.Step{{ID: "login", Call: "AuthService/Login", SkipAuth: true, AllowFail: true,
		Body: map[string]any{"username": "admin", "password": "wrong"}, Expect: expect}}}
}

func probeChain(probe *chain.Step) *chain.Chain {
	return flow(probe, step("after", "ThingService/Fetch", byID("thing-1"), okExpect()...))
}

func skipAuth(s *chain.Step) *chain.Step { s.SkipAuth = true; return s }

func refusedCreate(code string) func(*fakeServer) {
	return func(f *fakeServer) { f.createRefusal = code }
}

func TestEachStepIsRecordedAndJudged(t *testing.T) {
	mutated := func(mutate func(*chain.Chain)) func() *chain.Chain {
		return func() *chain.Chain { c := testChain(); mutate(c); return c }
	}
	allowFail := func(mutate func(*chain.Step)) func() *chain.Chain {
		return mutated(func(c *chain.Chain) { c.Steps[0].AllowFail = true; mutate(c.Steps[0]) })
	}
	partnerRefused := func(expect ...chain.Expectation) func() *chain.Chain {
		return mutated(func(c *chain.Chain) {
			c.Steps[0] = &chain.Step{ID: "create", Call: "PartnerService/FetchMine", Body: byID("thing-1"), AllowFail: true, Expect: expect}
			c.Steps[1].Body, c.Steps[1].Expect = byID("thing-1"), okExpect()
		})
	}
	oneCreate := func(expect ...chain.Expectation) func() *chain.Chain {
		return func() *chain.Chain { return flow(step("create", "ThingService/Create", thing("w"), expect...)) }
	}
	lenient := func(t *testing.T, f *fakeServer) *runner.Runner {
		r := newRunner(t, f)
		r.ValidateOutput = false
		return r
	}
	for _, tc := range []struct {
		name   string
		setup  func(*fakeServer)
		runner func(*testing.T, *fakeServer) *runner.Runner
		chain  func() *chain.Chain
		opts   runner.Options
		passed bool
		check  func(*testing.T, *runner.Record, *fakeServer)
	}{
		{name: "a run passes and exports", chain: testChain, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if rec.Exports["thing_id"] != "thing-1" || len(rec.Steps) != 2 || f.loginCount() != 1 {
				t.Fatalf("exports %v, %d steps, %d logins", rec.Exports, len(rec.Steps), f.loginCount())
			}
			if got := f.headerFor("/shrt.test.v1.ThingService/Create", "Authorization"); !strings.HasPrefix(got, "Bearer token-") {
				t.Fatalf("the token is found despite a camelCase login response, header %q", got)
			}
			for i, want := range []string{"shrt.test.v1.ThingService/Create", "shrt.test.v1.ThingService/Fetch"} {
				if rec.Steps[i].Call != want {
					t.Fatalf("step %d is recorded by its full name, got %q", i, rec.Steps[i].Call)
				}
			}
			if body := string(stepByID(t, rec, "fetch").Response); !strings.Contains(body, `"created_at"`) || strings.Contains(body, `"createdAt"`) {
				t.Fatalf("stored responses use proto names, got %s", body)
			}
		}},
		{name: "a short call name is recorded in full", chain: mutated(func(c *chain.Chain) { c.Steps[0].Call = "Create"; c.Normalize() }), passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if rec.Steps[0].Call != "shrt.test.v1.ThingService/Create" {
				t.Fatalf("got %q", rec.Steps[0].Call)
			}
		}},
		{name: "snake_case paths read a camelCase response", chain: mutated(func(c *chain.Chain) {
			c.Steps[1].Expect = append(c.Steps[1].Expect, chain.Expectation{Path: "created_at", NotEmpty: true}, chain.Expectation{Path: "createdAt", NotEmpty: true})
			c.Steps[1].Export = map[string]string{"stamp": "created_at"}
		}), passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if rec.Exports["stamp"] != "stamp-1" {
				t.Fatalf("export via snake_case path = %v", rec.Exports["stamp"])
			}
		}},
		{name: "step headers resolve references", chain: mutated(func(c *chain.Chain) { c.Steps[1].Headers = map[string]string{"X-Thing-Ref": "ref-${create.id}"} }), passed: true,
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if got := f.headerFor("/shrt.test.v1.ThingService/Fetch", "X-Thing-Ref"); got != "ref-thing-1" {
					t.Fatalf("header = %q", got)
				}
			}},
		{name: "the record carries the unordered lists verify reads", chain: mutated(func(c *chain.Chain) { c.Unordered, c.Steps[1].Unordered = []string{"items"}, []string{"lines"} }), passed: true,
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if strings.Join(rec.Steps[0].Unordered, ",") != "items" || strings.Join(rec.Steps[1].Unordered, ",") != "items,lines" {
					t.Fatalf("chain-level lists apply to every step and a step's are added: %v %v", rec.Steps[0].Unordered, rec.Steps[1].Unordered)
				}
			}},
		{name: "a failed step stops the run", chain: mutated(func(c *chain.Chain) { c.Steps[0].Expect = []chain.Expectation{{Path: "id", Equals: "wrong-id"}} }),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if len(rec.Steps) != 1 || rec.KeepGoing {
					t.Fatalf("want the chain to stop after step 1, got %d step records", len(rec.Steps))
				}
			}},
		{name: "the failure names the first failing expectation", setup: refusedCreate("out_of_stock"), chain: oneCreate(okExpect()...), check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			for _, want := range []string{`step "create": expectation failed`, "error.code", "want=OK", "got=out_of_stock", `message="refused"`} {
				if !strings.Contains(rec.Failure, want) {
					t.Fatalf("the failure lacks %q: %q", want, rec.Failure)
				}
			}
			if line := rec.Steps[0].Expect[0].String(); !strings.Contains(line, `message="refused"`) {
				t.Fatalf("the failure line carries the refusal's own message, got %q", line)
			}
			if !strings.Contains(rec.Warning, "conventions.envelope_ok") || !strings.Contains(rec.Warning, "out_of_stock (1)") {
				t.Fatalf("a run where no response carried envelope_ok must warn and name what was seen, got %q", rec.Warning)
			}
		}},
		{name: "a refusal as a whole does not blame envelope_ok", setup: refusedCreate("REJECTED"), chain: oneCreate(okExpect()...), check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if rec.Warning != "" {
				t.Fatalf("got %q", rec.Warning)
			}
		}},
		{name: "a step asserting a transport refusal does not blame envelope_ok", setup: refusedCreate("out_of_stock"), chain: oneCreate(chain.Expectation{Path: "transport.code", Equals: "invalid_argument"}),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if rec.Warning != "" {
					t.Fatalf("got %q", rec.Warning)
				}
				for _, want := range []string{"got=ok", "error.code = out_of_stock", `message="refused"`} {
					if line := rec.Steps[0].Expect[0].String(); !strings.Contains(line, want) {
						t.Fatalf("got=ok alone hides that the backend refused in-band; the line lacks %q: %q", want, line)
					}
				}
			}},
		{name: "an envelope path on a free-text field", chain: testChain, opts: runner.Options{KeepGoing: true}, runner: func(t *testing.T, f *fakeServer) *runner.Runner {
			chain.SetEnvelope("error.message", "OK")
			t.Cleanup(func() { chain.SetEnvelope("", "") })
			return newRunner(t, f)
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if !strings.Contains(rec.Warning, "conventions.envelope_path") || !strings.Contains(rec.Warning, `"" (2)`) {
				t.Fatalf("an empty message is not a verdict, so the path is wrong, printed visibly: %q", rec.Warning)
			}
		}},
		{name: "a step with no expect refused in-band warns", setup: refusedCreate("PERMISSION_DENIED"), chain: oneCreate(), passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if w := rec.Steps[0].Warning; !strings.Contains(w, "refused in-band") || !strings.Contains(w, "PERMISSION_DENIED") {
				t.Fatalf("warning = %q", w)
			}
		}},
		{name: "a step with no expect answered OK does not warn", chain: oneCreate(), passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if rec.Steps[0].Warning != "" || rec.Warning != "" {
				t.Fatalf("got %q %q", rec.Steps[0].Warning, rec.Warning)
			}
		}},
		{name: "a step asserting the in-band refusal does not warn", setup: refusedCreate("PERMISSION_DENIED"), chain: oneCreate(chain.Expectation{Path: "error.code", Equals: "PERMISSION_DENIED"}), passed: true,
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if rec.Steps[0].Warning != "" {
					t.Fatalf("got %q", rec.Steps[0].Warning)
				}
			}},
		{name: "exists fails on a scalar the server did not send", setup: func(f *fakeServer) { f.refuseLogin = true },
			chain: func() *chain.Chain {
				return loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"}, chain.Expectation{Path: "access_token", Exists: yes()})
			}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if expectResult(t, rec, "access_token").Passed {
					t.Fatal("every scalar is materialised at its zero value, so exists: true must read what was sent")
				}
				for _, want := range []string{`"access_token"`, `"expires_at"`} {
					if !strings.Contains(string(rec.Steps[0].Response), want) {
						t.Fatalf("the stored response keeps every declared field: %s", rec.Steps[0].Response)
					}
				}
			}},
		{name: "exists passes on a scalar the server did send", chain: func() *chain.Chain {
			return loginChain(chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "access_token", Exists: yes()})
		}, passed: true},
		{name: "exists false passes on a scalar the server withheld", setup: func(f *fakeServer) { f.refuseLogin = true }, chain: func() *chain.Chain {
			return loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"}, chain.Expectation{Path: "access_token", Exists: no()})
		}, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if note := rec.Steps[0].Note; !strings.Contains(note, "returned no token") || strings.Contains(note, "belongs to a different principal") {
				t.Fatalf("a refused login carries no token, so it cannot belong to anyone: %q", note)
			}
		}},
		{name: "a failed absence names the value present", chain: oneCreate(chain.Expectation{Path: "id", Exists: no()}), check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if got := chain.DescribeFailure(rec.Steps[0].Expect[0]); !strings.Contains(got, `want absent got present (holds "thing-`) {
				t.Fatalf("got %s", got)
			}
		}},
		{name: "an expectation resolves a reference to an earlier step", chain: func() *chain.Chain {
			return flow(step("first", "ThingService/Create", thing("widget"), okExpect()...),
				step("second", "ThingService/Create", thing("widget"), chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "id", NotEqual: "${steps.first.response.id}"}))
		}, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			var first map[string]any
			_ = json.Unmarshal(stepByID(t, rec, "first").Response, &first)
			if want := fmt.Sprint(stepByID(t, rec, "second").Expect[1].Want); want != fmt.Sprint(first["id"]) {
				t.Fatalf("the recorded want is the resolved id %v, not %q", first["id"], want)
			}
		}},
		{name: "an expectation reads its own step's request", chain: func() *chain.Chain {
			return flow(step("fetch", "ThingService/Fetch", byID("abc"), chain.Expectation{Path: "id", Equals: "${steps.fetch.request.id}"}),
				skipAuth(step("probe", "ThingService/Fetch", byID("xyz"), chain.Expectation{Path: "transport.code", Equals: "unauthenticated"},
					chain.Expectation{Path: "transport.message", NotEqual: "${steps.probe.request.id}"})))
		}, passed: true},
		{name: "an expectation reads its own step's request in a dry run", opts: runner.Options{DryRun: true}, chain: func() *chain.Chain {
			return flow(step("fetch", "ThingService/Fetch", byID("abc"), chain.Expectation{Path: "id", Equals: "${steps.fetch.request.id}"}))
		}, passed: true},
		{name: "a response reference to a step with no response", chain: func() *chain.Chain {
			probe := skipAuth(step("probe", "ThingService/Fetch", byID("x")))
			probe.AllowFail = true
			return flow(probe, step("next", "ThingService/Fetch", byID("${probe.id}"), okExpect()...))
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if next := stepByID(t, rec, "next"); next.Status != runner.StatusError || !strings.Contains(next.Error, "has no response") {
				t.Fatalf("got %s %q", next.Status, next.Error)
			}
		}},
		{name: "a transport refusal the step asserts", chain: func() *chain.Chain {
			return probeChain(skipAuth(step("no_token", "ThingService/Fetch", byID("thing-1"), unauthenticatedExpect()...)))
		}, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			sr := rec.Steps[0]
			if len(sr.Expect) != 2 || !sr.Expect[0].Passed || !sr.Expect[1].Passed || sr.Transport == nil || sr.Transport.Code != "unauthenticated" || len(rec.Steps) != 2 {
				t.Fatalf("both transport assertions held, the refusal is recorded and the chain continues: %+v", sr)
			}
			if got := f.headerFor("/shrt.test.v1.ThingService/Fetch", "Authorization"); got != "" {
				t.Fatalf("skip_auth sends no credential, sent %q", got)
			}
		}},
		{name: "a transport refusal with the wrong code", chain: func() *chain.Chain {
			s := skipAuth(step("no_token", "ThingService/Fetch", byID("thing-1"), chain.Expectation{Path: "transport.code", Equals: "permission_denied"}))
			s.AllowFail = true
			return probeChain(s)
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if got := rec.Steps[0].Expect[0]; got.Passed || got.Rule != "equals" || got.Got != "unauthenticated" {
				t.Fatalf("allow_fail must not tolerate the wrong refusal: %+v", got)
			}
		}},
		{name: "a refusal probe whose call succeeds", chain: func() *chain.Chain {
			s := step("should_refuse", "ThingService/Fetch", byID("thing-1"), unauthenticatedExpect()...)
			s.AllowFail = true
			return probeChain(s)
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if got := rec.Steps[0].Expect[0]; got.Got != chain.TransportOK {
				t.Fatalf("on success transport.code reads %q, got %+v", chain.TransportOK, got)
			}
		}},
		{name: "transport paths read the success outcome", chain: func() *chain.Chain {
			return flow(step("fetch", "ThingService/Fetch", byID("thing-1"), chain.Expectation{Path: "transport.code", Equals: "ok"},
				chain.Expectation{Path: "transport.http_status", Equals: 200}, chain.Expectation{Path: "transport.message", Exists: no()}))
		}, passed: true},
		{name: "a body assertion on a refused call", chain: func() *chain.Chain {
			return probeChain(skipAuth(step("no_token", "ThingService/Fetch", byID("thing-1"), chain.Expectation{Path: "transport.code", Equals: "unauthenticated"}, chain.Expectation{Path: "error.code", Equals: "OK"})))
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if body := rec.Steps[0].Expect[1]; body.Rule != "unevaluated" || body.Passed {
				t.Fatalf("got %+v", body)
			}
		}},
		{name: "a transport refused step shows each unevaluated want", chain: func() *chain.Chain {
			return probeChain(skipAuth(step("no_token", "ThingService/Fetch", byID("thing-1"), chain.Expectation{Path: "error.code", Equals: "REJECTED"}, chain.Expectation{Path: "name", Contains: "wid"})))
		}, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			sr := rec.Steps[0]
			if sr.Expect[0].Rule != "unevaluated" || !strings.Contains(sr.Expect[0].String(), "want=REJECTED") || !strings.Contains(sr.Expect[1].String(), "want=contains wid") {
				t.Fatalf("got %+v", sr.Expect)
			}
		}},
		{name: "auth: invalid sends a token never issued", chain: func() *chain.Chain {
			s := step("bad_token", "ThingService/Fetch", byID("thing-1"), unauthenticatedExpect()...)
			s.Auth = transport.InvalidTokenProfile
			return probeChain(s)
		}, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if sent := f.headerFor("/shrt.test.v1.ThingService/Fetch", "Authorization"); sent != "Bearer "+transport.InvalidToken || f.loginCount() != 1 {
				t.Fatalf("sent %q after %d logins", sent, f.loginCount())
			}
		}},
		{name: "auth: invalid uses the owning profile's header", runner: func(t *testing.T, f *fakeServer) *runner.Runner {
			cfg := partnerConfig(f.URL)
			p := cfg.Auth.Profiles["partner"]
			p.Calls, p.Header, p.Scheme = []string{"shrt.test.v1.PartnerService/*"}, "X-Partner-Token", "Token"
			return profileRunner(t, f, cfg)
		}, chain: func() *chain.Chain {
			s := step("bad", "PartnerService/FetchMine", byID("p-1"), unauthenticatedExpect()...)
			s.Auth = transport.InvalidTokenProfile
			return flow(s)
		}, passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if got := f.headerFor("/shrt.test.v1.PartnerService/FetchMine", "X-Partner-Token"); got != "Token "+transport.InvalidToken {
				t.Fatalf("got %q", got)
			}
		}},
		{name: "allow_fail tolerates the call being refused", chain: partnerRefused(), passed: true, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			if len(rec.Steps) != 2 || rec.Steps[0].Status != runner.StatusFailed {
				t.Fatalf("the chain continues past the tolerated step, recorded as failed: %+v", rec.Steps[0])
			}
		}},
		{name: "allow_fail does not swallow assertions that never ran", chain: partnerRefused(chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "name", NotEmpty: true}),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if sr := rec.Steps[0]; len(sr.Expect) != 2 || sr.Expect[0].Passed || !strings.Contains(sr.Expect[0].Detail, "never ran") || !strings.Contains(sr.Expect[1].Detail, "never ran") {
					t.Fatalf("both assertions are recorded unevaluated with why: %+v", sr.Expect)
				}
			}},
		{name: "allow_fail does not waive an expectation", chain: allowFail(func(s *chain.Step) { s.Expect = []chain.Expectation{{Path: "id", Equals: "deliberately-wrong"}} }),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if !strings.Contains(rec.Failure, "not an expectation you wrote being") {
					t.Fatalf("the failure explains what allow_fail covers: %q", rec.Failure)
				}
			}},
		{name: "allow_fail with assertions that held", chain: allowFail(func(*chain.Step) {}), passed: true},
		{name: "validate_output fails a descriptor mismatch", setup: func(f *fakeServer) { f.unknownField = true }, chain: testChain, check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
			fetch := stepByID(t, rec, "fetch")
			if rec.Status != runner.StatusFailed || !fetch.Drift || !strings.Contains(rec.Failure, "no_such_field_in_the_proto") || strings.Contains(string(fetch.Response), "createdAt") {
				t.Fatalf("the sent-and-answered step fails as drift naming the field: %s %v %q", rec.Status, fetch.Drift, rec.Failure)
			}
			for _, e := range fetch.Expect {
				if strings.Contains(e.Detail, "refused") {
					t.Errorf("the call was answered, not refused: %q", e.Detail)
				}
			}
		}},
		{name: "allow_fail does not swallow a descriptor mismatch", setup: func(f *fakeServer) { f.unknownField = true }, chain: mutated(func(c *chain.Chain) { c.Steps[1].AllowFail = true }),
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				if !strings.Contains(rec.Failure, "not a response the descriptor cannot read") {
					t.Fatalf("got %q", rec.Failure)
				}
			}},
		{name: "an unknown response field is discarded without validate_output", setup: func(f *fakeServer) { f.unknownField = true }, runner: lenient, chain: testChain, passed: true,
			check: func(t *testing.T, rec *runner.Record, f *fakeServer) {
				fetch := stepByID(t, rec, "fetch")
				if resp := string(fetch.Response); strings.Contains(resp, "createdAt") || strings.Contains(resp, "no_such_field_in_the_proto") {
					t.Errorf("the response is re-encoded with proto names and the unknown field dropped: %s", resp)
				}
				if strings.Contains(fetch.Warning, "kept as sent") || !strings.Contains(fetch.Warning, "no_such_field_in_the_proto") {
					t.Errorf("the warning names the discarded field: %q", fetch.Warning)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeServer()
			t.Cleanup(f.Close)
			if tc.setup != nil {
				tc.setup(f)
			}
			newR := tc.runner
			if newR == nil {
				newR = func(t *testing.T, f *fakeServer) *runner.Runner { return newRunner(t, f) }
			}
			rec := run(t, newR(t, f), normalized(t, tc.chain()), tc.opts)
			if rec.Passed() != tc.passed {
				t.Fatalf("passed=%v, want %v: %s %s", rec.Passed(), tc.passed, rec.Status, rec.Failure)
			}
			if tc.check != nil {
				tc.check(t, rec, f)
			}
		})
	}
}
