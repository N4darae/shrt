package runner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func TestAStepIsJudgedOnTheVerdictItsResponseCarries(t *testing.T) {
	refused := map[string]any{"status": map[string]any{"code": "REJECTED", "message": "qty must be positive"}, "qtyOnHand": "0"}
	stock := func(qty any) map[string]any {
		return map[string]any{"status": map[string]any{"code": "SUCCESS"}, "qtyOnHand": qty}
	}
	e := func(path string, rule func(*chain.Expectation)) chain.Expectation {
		x := chain.Expectation{Path: path}
		rule(&x)
		return x
	}
	eq := func(v any) func(*chain.Expectation) { return func(x *chain.Expectation) { x.Equals = v } }
	ne := func(v any) func(*chain.Expectation) { return func(x *chain.Expectation) { x.NotEqual = v } }
	has := func(v string) func(*chain.Expectation) { return func(x *chain.Expectation) { x.Contains = v } }
	notEmpty := func(x *chain.Expectation) { x.NotEmpty = true }
	absent := func(x *chain.Expectation) { x.Exists = no() }
	qty0 := e("qty_on_hand", eq(0))
	type row struct {
		name       string
		answer     any
		expect     []chain.Expectation
		vars       map[string]any
		codeFields bool
		passed     bool
		says       string
		stepSays   string
	}
	rows := []row{
		{name: "no envelope", answer: map[string]any{}, expect: []chain.Expectation{qty0}, says: "no verdict"},
		{name: "empty envelope", answer: map[string]any{"status": map[string]any{}}, expect: []chain.Expectation{qty0}, says: "no verdict"},
		{name: "empty code", answer: map[string]any{"status": map[string]any{"code": ""}}, expect: []chain.Expectation{qty0}, says: "no verdict"},
		{name: "a status that is a string", answer: `{"status":"SUCCESS","qtyOnHand":"5"}`, expect: []chain.Expectation{e("qty_on_hand", eq(5))}, says: "no verdict"},
		{name: "a missing verdict that is pinned", answer: map[string]any{}, expect: []chain.Expectation{e("status.code", absent), qty0}, passed: true},
		{name: "a present ok verdict", answer: stock("0"), expect: []chain.Expectation{qty0}, passed: true},
		{name: "an unpinned in-band refusal", answer: refused, expect: []chain.Expectation{qty0}, says: "refused in-band"},
		{name: "a refusal pinned by equals", answer: refused, expect: []chain.Expectation{e("status.code", eq("REJECTED")), qty0}, passed: true},
		{name: "a refusal pinned by not_equal ok", answer: refused, expect: []chain.Expectation{e("status.code", ne("SUCCESS")), qty0}, passed: true},
		{name: "a refusal pinned by not_equal a var holding ok", answer: refused, vars: map[string]any{"ok": "SUCCESS"}, expect: []chain.Expectation{e("status.code", ne("${vars.ok}")), qty0}, passed: true},
		{name: "a refusal pinned by contains", answer: refused, expect: []chain.Expectation{e("status.code", has("REJ")), qty0}, passed: true},
		{name: "not_equal empty says nothing", answer: refused, expect: []chain.Expectation{e("status.code", ne("")), qty0}, says: "refused in-band"},
		{name: "not_equal a typo says nothing", answer: refused, expect: []chain.Expectation{e("status.code", ne("REJECTD")), qty0}, says: "refused in-band"},
		{name: "not_equal a var holding a typo says nothing", answer: refused, vars: map[string]any{"typo": "REJECTD"}, expect: []chain.Expectation{e("status.code", ne("${vars.typo}")), qty0}, says: "refused in-band"},
		{name: "transport ok says nothing", answer: refused, expect: []chain.Expectation{e("transport.code", eq("ok")), qty0}, says: "refused in-band"},
		{name: "a sibling not_equal", answer: refused, expect: []chain.Expectation{e("status.message", ne("boom"))}},
		{name: "a sibling equals", answer: refused, expect: []chain.Expectation{e("status.message", eq("qty must be positive"))}},
		{name: "a sibling contains", answer: refused, expect: []chain.Expectation{e("status.message", has("positive"))}},
		{name: "an empty message rule", answer: map[string]any{"status": map[string]any{"code": "REJECTED"}, "qtyOnHand": "0"}, expect: []chain.Expectation{e("status.message", eq(""))}},
		{name: "not_equal ok on an empty verdict", answer: map[string]any{"status": map[string]any{"code": ""}, "qtyOnHand": "5"}, expect: []chain.Expectation{e("status.code", ne("SUCCESS"))}},
		{name: "an empty verdict pinned as empty", answer: map[string]any{"status": map[string]any{}, "qtyOnHand": "5"}, expect: []chain.Expectation{e("status.code", eq(""))}, passed: true},
		{name: "a casefolded verdict path not_equal", answer: refused, expect: []chain.Expectation{e("Status.Code", ne("SUCCESS"))}, passed: true},
		{name: "a casefolded verdict path equals", answer: refused, expect: []chain.Expectation{e("Status.Code", eq("REJECTED"))}, passed: true},
		{name: "an upper-case verdict path", answer: refused, expect: []chain.Expectation{e("STATUS.code", eq("REJECTED"))}, passed: true},
		{name: "a code field pinned by equals", answer: refused, codeFields: true, expect: []chain.Expectation{e("status.message", eq("qty must be positive"))}, passed: true},
		{name: "a code field pinned by contains", answer: refused, codeFields: true, expect: []chain.Expectation{e("status.message", has("positive"))}, passed: true},
		{name: "a code field not_empty", answer: refused, codeFields: true, expect: []chain.Expectation{e("status.message", notEmpty)}, says: "no expectation on this step pins the verdict"},
		{name: "a code field not_equal", answer: refused, codeFields: true, expect: []chain.Expectation{e("status.message", ne("boom"))}, says: "no expectation on this step pins the verdict"},
		{name: "a code field equals empty", answer: refused, codeFields: true, expect: []chain.Expectation{e("status.message", eq(""))}, says: "no expectation on this step pins the verdict"},
		{name: "a data field beside code fields", answer: refused, codeFields: true, expect: []chain.Expectation{qty0}, says: "no expectation on this step pins the verdict"},
		{name: "an int64 is not empty", answer: stock("7"), expect: []chain.Expectation{e("qty_on_hand", notEmpty), e("qty_on_hand", ne(""))}, passed: true},
		{name: "a repeated top-level key", answer: `{"status":{"code":"REJECTED","message":"denied"},"qtyOnHand":"5","status":{"code":"SUCCESS"}}`,
			expect: []chain.Expectation{e("status.code", eq("SUCCESS")), e("qty_on_hand", eq(5))}, stepSays: "status"},
		{name: "a repeated nested key", answer: `{"status":{"code":"SUCCESS","code":"REJECTED"},"qtyOnHand":"5"}`,
			expect: []chain.Expectation{e("status.code", eq("SUCCESS")), e("qty_on_hand", eq(5))}, stepSays: "status"},
		{name: "a field spelt twice, json name first", answer: `{"status":{"code":"SUCCESS"},"qtyOnHand":"999","qty_on_hand":"5"}`,
			expect: []chain.Expectation{e("status.code", eq("SUCCESS")), e("qty_on_hand", eq(5))}, stepSays: "qty"},
		{name: "a field spelt twice, proto name first", answer: `{"status":{"code":"SUCCESS"},"qty_on_hand":"5","qtyOnHand":"999"}`,
			expect: []chain.Expectation{e("status.code", eq("SUCCESS")), e("qty_on_hand", eq(5))}, stepSays: "qty"},
		{name: "one spelling of each field", answer: stock5, expect: []chain.Expectation{e("qty_on_hand", eq(5))}, passed: true},
	}
	for _, wire := range []any{"0", 0, nil} {
		for _, x := range []chain.Expectation{e("qty_on_hand", notEmpty), e("qty_on_hand", ne(""))} {
			rows = append(rows, row{name: fmt.Sprintf("an int64 zero sent as %#v", wire), answer: stock(wire), expect: []chain.Expectation{x}})
		}
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			if tc.codeFields {
				chain.ApplyCodeFields([]string{"message"})
				t.Cleanup(func() { chain.ApplyCodeFields(nil) })
			}
			body, validate := "", false
			if s, raw := tc.answer.(string); raw {
				body = s
			} else {
				b, _ := json.Marshal(tc.answer)
				body, validate = string(b), true
			}
			c := addStockChain(tc.expect...)
			c.Vars = tc.vars
			rec := run(t, stockRunner(t, body, validate), normalized(t, c), runner.Options{})
			if rec.Passed() != tc.passed {
				t.Fatalf("passed=%v, want %v: %s %+v", rec.Passed(), tc.passed, rec.Failure, rec.Steps[0].Expect)
			}
			if tc.says != "" && !strings.Contains(recordText(t, rec), tc.says) {
				t.Errorf("the record must say %q: %s %+v", tc.says, rec.Failure, rec.Steps[0].Expect)
			}
			if tc.stepSays != "" && (!strings.Contains(rec.Steps[0].Error, "repeats") || !strings.Contains(rec.Steps[0].Error, tc.stepSays)) {
				t.Errorf("the error must name the repeated field, got %q", rec.Steps[0].Error)
			}
		})
	}
}

func TestAnInt64ZeroAtARedactedPathIsNotRedacted(t *testing.T) {
	rec := run(t, stockRunner(t, `{"status":{"code":"SUCCESS"},"qtyOnHand":"0"}`, true),
		normalized(t, addStockChain(chain.Expectation{Path: "status.code", Equals: "SUCCESS"})), runner.Options{Redact: []string{"**.qty_on_hand", "**.qty"}})
	var resp, req map[string]any
	_ = json.Unmarshal(rec.Steps[0].Response, &resp)
	_ = json.Unmarshal(rec.Steps[0].Request, &req)
	if resp["qty_on_hand"] != "0" || req["qty"] != "0" || strings.Contains(string(rec.Steps[0].Response), "<redacted>") {
		t.Fatalf("an empty value, 0 included, is never redacted, whatever its proto type; got response %s request %s", rec.Steps[0].Response, rec.Steps[0].Request)
	}
}

func successEnvelopeBatch(w http.ResponseWriter, r *http.Request) {
	ok := map[string]any{"code": "SUCCESS", "message": ""}
	if r.URL.Path == "/shrt.test.v1.AuthService/Login" {
		writeJSON(w, 200, map[string]any{"error": ok, "accessToken": "t", "expiresAt": "0"})
		return
	}
	writeJSON(w, 200, map[string]any{"error": ok, "results": []any{
		map[string]any{"error": ok, "amount": "1"},
		map[string]any{"error": map[string]any{"code": "invalid_argument", "message": "bad"}, "amount": ""},
	}})
}

func TestItemEnvelopeFailsARefusedBatchLineNoExpectationDeclares(t *testing.T) {
	envOK := chain.Expectation{Path: "error.code", Equals: "OK"}
	envSuccess := chain.Expectation{Path: "error.code", Equals: "SUCCESS"}
	for _, tc := range []struct {
		name       string
		setup      func(*fakeServer)
		serve      http.HandlerFunc
		lenient    bool
		success    bool
		codeFields bool
		call       string
		lines      []any
		expect     []chain.Expectation
		passed     bool
		fired      bool
		got        []string
		gotNot     string
		detail     string
		warning    string
	}{
		{name: "a refusal behind an OK envelope", lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK}, fired: true, got: []string{"results.1.error.code = invalid_argument", `message="bad was refused"`}},
		{name: "a refusal the step asserts", lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK,
			{Path: "results.0.error.code", Equals: "OK"}, {Path: "results.1.error.code", Equals: "invalid_argument"}}, passed: true},
		{name: "only the undeclared refusal is reported", lines: []any{"bad-one", "bad-two"}, expect: []chain.Expectation{envOK, {Path: "results[0].error.code", NotEqual: "OK"}},
			fired: true, got: []string{"results.1.error.code = invalid_argument"}, gotNot: "results.0."},
		{name: "exists on the verdict path declares nothing", lines: []any{"bad"}, expect: []chain.Expectation{envOK, {Path: "results.0.error.code", Exists: yes()}}, fired: true},
		{name: "not_equal empty declares nothing", lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK, {Path: "results.1.error.code", NotEqual: ""}}, fired: true},
		{name: "not_equal a typo declares nothing", lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK, {Path: "results.1.error.code", NotEqual: "INVALID_ARGUMNT"}}, fired: true},
		{name: "not_equal ok declares the refusal", lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK, {Path: "results.1.error.code", NotEqual: "OK"}}, passed: true},
		{name: "a receipt list carries no verdict field", call: "BatchService/Receipt", lines: []any{"a", "b"}, expect: []chain.Expectation{envOK}, passed: true},
		{name: "an unset per-item envelope is success", setup: func(f *fakeServer) { f.batchUnset = true }, lines: []any{"ok", "fine"}, expect: []chain.Expectation{envOK}, passed: true},
		{name: "a stale descriptor does not silence the check", setup: func(f *fakeServer) { f.receiptDrift = true }, lenient: true, call: "BatchService/Receipt",
			lines: []any{"ok", "bad"}, expect: []chain.Expectation{envOK}, fired: true, got: []string{"results.1.error.code = invalid_argument"}, warning: "descriptor"},
		{name: "a non-batch list under a lenient runner", lenient: true, call: "BatchService/Receipt", lines: []any{"a"}, expect: []chain.Expectation{envOK}, passed: true},
		{name: "a verdict under a case-variant key", setup: func(f *fakeServer) { f.itemKey = "Error" }, lenient: true, lines: []any{"ok", "ok"}, expect: []chain.Expectation{envOK},
			fired: true, got: []string{"results.0.error.code = " + chain.NoItemVerdict, "results.1.error.code"}, warning: "results.0 carries its verdict under Error"},
		{name: "the detail names the configured OK value", serve: successEnvelopeBatch, success: true, lines: []any{"a", "b"}, expect: []chain.Expectation{envSuccess},
			fired: true, detail: "top-level envelope said SUCCESS"},
		{name: "a pin on a refused line's code field declares it", serve: successEnvelopeBatch, success: true, codeFields: true, lines: []any{"a", "b"},
			expect: []chain.Expectation{envSuccess, {Path: "results.1.error.message", Equals: "bad"}}, passed: true},
		{name: "not_empty on a code field declares nothing", serve: successEnvelopeBatch, success: true, codeFields: true, lines: []any{"a", "b"},
			expect: []chain.Expectation{envSuccess, {Path: "results.1.error.message", NotEmpty: true}}, fired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain.SetItemEnvelope("results[].error.code")
			t.Cleanup(func() { chain.SetItemEnvelope(""); chain.SetEnvelope("", ""); chain.ApplyCodeFields(nil) })
			if tc.success {
				chain.SetEnvelope("error.code", "SUCCESS")
			}
			if tc.codeFields {
				chain.ApplyCodeFields([]string{"message"})
			}
			url := ""
			if tc.serve != nil {
				srv := httptest.NewServer(tc.serve)
				t.Cleanup(srv.Close)
				url = srv.URL
			} else {
				f := newFakeServer()
				t.Cleanup(f.Close)
				if tc.setup != nil {
					tc.setup(f)
				}
				url = f.URL
			}
			r := buildRunner(t, testConfig(url), catalogtest.Batch())
			r.ValidateOutput = !tc.lenient
			call := tc.call
			if call == "" {
				call = "BatchService/Preview"
			}
			c := batchChain(call, tc.lines, tc.expect...)
			c.Vars = map[string]any{"typo": "INVALID_ARGUMNT", "ok": "OK"}
			rec := run(t, r, c, runner.Options{})
			res, fired := itemEnvelopeResult(t, rec)
			if rec.Passed() != tc.passed || fired != tc.fired {
				t.Fatalf("passed=%v item_envelope=%v, want %v %v: %s %+v", rec.Passed(), fired, tc.passed, tc.fired, rec.Failure, rec.Steps[0].Expect)
			}
			got := fmt.Sprint(res.Got)
			for _, w := range tc.got {
				if !strings.Contains(got, w) {
					t.Errorf("got = %q, want it to name %q", got, w)
				}
			}
			if (tc.gotNot != "" && strings.Contains(got, tc.gotNot)) || !strings.Contains(res.Detail, tc.detail) || !strings.Contains(rec.Steps[0].Warning, tc.warning) {
				t.Errorf("got %q detail %q warning %q", got, res.Detail, rec.Steps[0].Warning)
			}
			if fired && rec.Steps[0].Error != "" && tc.warning == "" {
				t.Errorf("an assertion failure is not a step error, got %q", rec.Steps[0].Error)
			}
		})
	}
}

func TestAnItemRefusalMatchingTheTopLevelValuePointsAtEnvelopeOK(t *testing.T) {
	chain.SetItemEnvelope("results[].error.code")
	chain.ApplyConventions(nil, "error.code", "SUCCESS")
	t.Cleanup(func() { chain.SetItemEnvelope(""); chain.ApplyConventions(nil, "", "") })
	srv := newFakeServer()
	defer srv.Close()
	rec := run(t, newBatchRunner(t, srv), batchChain("BatchService/Preview", []any{"ok"}, chain.Expectation{Path: "id", Exists: new(bool)}), runner.Options{})
	got, ok := itemEnvelopeResult(t, rec)
	if !ok || !strings.Contains(got.Detail, "conventions.envelope_ok: OK") || strings.Contains(got.Detail, "were refused while") {
		t.Fatalf("an item 'refusal' equal to the top-level value points at envelope_ok, got %+v", got)
	}
}

func TestConventionsNoResponseDeclaresAreRefusedUpFront(t *testing.T) {
	defer chain.SetItemEnvelope("")
	for _, tc := range []struct {
		name     string
		envelope string
		item     string
		batch    bool
		want     []string
	}{
		{name: "a bad envelope_path", envelope: "status.cod", want: []string{"conventions.envelope_path", `"error.code"`}},
		{name: "a declared envelope_path", envelope: "error.code"},
		{name: "an item envelope no response declares", item: "results[].error.code", want: []string{"results[].error.code"}},
		{name: "an item envelope the batch descriptor declares", item: "results[].error.code", batch: true},
		{name: "an item envelope without []", item: "results.error.code", batch: true, want: []string{"<list>[].<path>"}},
	} {
		cfg := testConfig("http://127.0.0.1:1")
		cfg.Conventions.EnvelopePath, cfg.Conventions.ItemEnvelopePath = tc.envelope, tc.item
		cat := catalogtest.New()
		if tc.batch {
			cat = catalogtest.Batch()
		}
		_, _, err := runner.NewFromConfig(context.Background(), cfg, cat)
		if (err == nil) != (tc.want == nil) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the error lacks %q: %v", tc.name, w, err)
			}
		}
	}
	cfg := config.Default()
	cfg.Root, cfg.Target.BaseURL = t.TempDir(), "http://127.0.0.1:1"
	if r, _, err := runner.NewFromConfig(context.Background(), cfg, nil); err != nil || r.ValidateOutput {
		t.Fatalf("validate_output is off by default: %v", err)
	}
	cfg.Conventions.ValidateOutput = true
	if r, _, err := runner.NewFromConfig(context.Background(), cfg, nil); err != nil || !r.ValidateOutput {
		t.Fatalf("conventions.validate_output reaches the Runner: %v", err)
	}
}
