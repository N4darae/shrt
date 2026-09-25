package runner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func TestAClientStreamingRPCIsRefusedBeforeSendingInRunAndDryRun(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnsupportedMediaType)
	}))
	defer srv.Close()
	cat := catalogtest.Rich()
	method, err := cat.Lookup("OrderService/UploadOrders")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Target.BaseURL = srv.URL
	deps, err := runner.Build(context.Background(), cfg, cat, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client}
	for _, dry := range []bool{false, true} {
		c := normalized(t, &chain.Chain{Name: "watch", Steps: []*chain.Step{{
			ID: "upload", Call: "OrderService/UploadOrders", SkipAuth: true,
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "http_415"}},
		}}})
		rec, err := r.Run(context.Background(), c, runner.Options{DryRun: dry})
		if err == nil {
			t.Fatalf("dry=%v: a streaming rpc is a static refusal; want the chain refused before sending, got a record with status %s", dry, rec.Status)
		}
		if !strings.Contains(err.Error(), method.StreamRefusal()) || !strings.Contains(err.Error(), "nothing was sent") {
			t.Errorf("dry=%v: want lint's StreamRefusal sentence and that nothing was sent, got %q", dry, err)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("the streaming step reached the server %d time(s)", n)
	}
}

func TestAnExpectationReadsItsOwnStepsRequest(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "echo", Steps: []*chain.Step{
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "abc"},
			Expect: []chain.Expectation{{Path: "id", Equals: "${steps.fetch.request.id}"}}},
		{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "xyz"},
			Expect: []chain.Expectation{
				{Path: "transport.code", Equals: "unauthenticated"},
				{Path: "transport.message", NotEqual: "${steps.probe.request.id}"},
			}},
	}})
	for _, dry := range []bool{false, true} {
		rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{DryRun: dry})
		if err != nil {
			t.Fatal(err)
		}
		if !rec.Passed() {
			t.Fatalf("dry=%v: an expectation reading this step's own request must resolve, on an answered "+
				"call and on a refused one: %s", dry, rec.Failure)
		}
	}
}

func TestAResponseReferenceToAStepWithNoResponseSaysSo(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "no-response", Steps: []*chain.Step{
		{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, AllowFail: true, Body: map[string]any{"id": "x"}},
		{ID: "next", Call: "ThingService/Fetch", Body: map[string]any{"id": "${probe.id}"}, Expect: okExpect()},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	next := stepByID(t, rec, "next")
	if next.Status != runner.StatusError || !strings.Contains(next.Error, "has no response") {
		t.Fatalf("a refused step has only a request; reading its response must fail loudly, got %s %q", next.Status, next.Error)
	}
}

func successEnvelopeBatchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ok := map[string]any{"code": "SUCCESS", "message": ""}
		switch r.URL.Path {
		case "/shrt.test.v1.AuthService/Login":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "accessToken": "t", "expiresAt": "0"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "results": []any{
				map[string]any{"error": ok, "amount": "1"},
				map[string]any{"error": map[string]any{"code": "invalid_argument", "message": "bad"}, "amount": ""},
			}})
		}
	}))
}

func runSuccessBatch(t *testing.T, expect ...chain.Expectation) *runner.Record {
	t.Helper()
	srv := successEnvelopeBatchServer()
	defer srv.Close()
	deps, err := runner.Build(context.Background(), testConfig(srv.URL), catalogtest.Batch(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
	rec, err := r.Run(context.Background(), batchChain("BatchService/Preview", []any{"a", "b"}, expect...), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestItemEnvelopeFailureNamesTheConfiguredOKValue(t *testing.T) {
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	chain.SetEnvelope("error.code", "SUCCESS")
	chain.SetItemEnvelope("results[].error.code")

	rec := runSuccessBatch(t, chain.Expectation{Path: "error.code", Equals: "SUCCESS"})
	res, found := itemEnvelopeResult(t, rec)
	if !found {
		t.Fatalf("line 1 was refused and undeclared: %+v", rec.Steps[0].Expect)
	}
	if !strings.Contains(res.Detail, "top-level envelope said SUCCESS") || strings.Contains(res.Detail, "said OK") {
		t.Errorf("the detail must quote the envelope this backend uses, got %q", res.Detail)
	}
	if res.Want != "SUCCESS" {
		t.Errorf("want = %v, want SUCCESS", res.Want)
	}
}

func TestAPinOnARefusedLinesCodeFieldDeclaresIt(t *testing.T) {
	defer chain.SetEnvelope("", "")
	defer chain.SetItemEnvelope("")
	defer chain.ApplyCodeFields(nil)
	chain.SetEnvelope("error.code", "SUCCESS")
	chain.SetItemEnvelope("results[].error.code")
	chain.ApplyCodeFields([]string{"message"})

	rec := runSuccessBatch(t,
		chain.Expectation{Path: "error.code", Equals: "SUCCESS"},
		chain.Expectation{Path: "results.1.error.message", Equals: "bad"},
	)
	if !rec.Passed() {
		t.Fatalf("pinning the refused line's code field is a stronger declaration than pinning its verdict: %s", rec.Failure)
	}

	rec = runSuccessBatch(t,
		chain.Expectation{Path: "error.code", Equals: "SUCCESS"},
		chain.Expectation{Path: "results.1.error.message", NotEmpty: true},
	)
	if _, found := itemEnvelopeResult(t, rec); !found {
		t.Fatal("not_empty on a code field says nothing about which code, so it must not declare the refusal")
	}
}

func TestKeepGoingSendsAStepThatReadsOnlyAFailedStepsRequest(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "idempotent", Steps: []*chain.Step{
		{ID: "create_order", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "k-1"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "id", Equals: "deliberately-wrong"}}},
		{ID: "create_order_again", Call: "ThingService/Create",
			Body:   map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${steps.create_order.request.idempotency_key}"},
			Expect: okExpect()},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create_order.id}"}, Expect: okExpect()},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := stepByID(t, rec, "create_order_again").Status; got != runner.StatusPassed {
		t.Fatalf("a request value is what was sent, so reading it is safe; want create_order_again passed, got %s: %s",
			got, stepByID(t, rec, "create_order_again").Error)
	}
	fetch := stepByID(t, rec, "fetch")
	if fetch.Status != runner.StatusSkipped {
		t.Fatalf("fetch reads the failed step's response and must be withheld, got %s", fetch.Status)
	}
	if !strings.Contains(fetch.Error, "failed its assertion on id") {
		t.Errorf("the message must say what happened to create_order, got %q", fetch.Error)
	}
	if strings.Contains(fetch.Error, "refused call") {
		t.Errorf("create_order was answered, not refused: %q", fetch.Error)
	}
}

func TestKeepGoingNamesARefusalAsARefusal(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	c := normalized(t, &chain.Chain{Name: "refused", Steps: []*chain.Step{
		{ID: "probe", Call: "ThingService/Fetch", SkipAuth: true, Body: map[string]any{"id": "x"}, Expect: okExpect()},
		{ID: "next", Call: "ThingService/Fetch", Body: map[string]any{"id": "${probe.id}"}, Expect: okExpect()},
	}})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	if msg := stepByID(t, rec, "next").Error; !strings.Contains(msg, "was refused before a response body existed (transport unauthenticated)") {
		t.Fatalf("got %q", msg)
	}
}

func TestAnEmptySecretIsNotMasked(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true
	redact := []string{"**.access_token", "**.password"}
	c := loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"},
		chain.Expectation{Path: "access_token", Equals: ""})
	rec, err := newRunner(t, srv).Run(context.Background(), c, runner.Options{Redact: redact})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Steps[0].Response, &body); err != nil {
		t.Fatal(err)
	}
	if body["access_token"] != "" {
		t.Fatalf("a refused login sent no token, and masking \"\" hides exactly that: got %v", body["access_token"])
	}
	if got := expectResult(t, rec, "access_token"); got.Got == pathmask.MaskRedacted {
		t.Errorf("the expectation result masked an empty value: %+v", got)
	}
	var req map[string]any
	if err := json.Unmarshal(rec.Steps[0].Request, &req); err != nil {
		t.Fatal(err)
	}
	if req["password"] != pathmask.MaskRedacted {
		t.Errorf("a non-empty secret must still be masked, got %v", req["password"])
	}

	srv.refuseLogin = false
	rec, err = newRunner(t, srv).Run(context.Background(), loginChain(chain.Expectation{Path: "error.code", Equals: "OK"}), runner.Options{Redact: redact})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rec.Steps[0].Response), `"access_token":"`+pathmask.MaskRedacted+`"`) {
		t.Fatalf("an issued token must still be masked: %s", rec.Steps[0].Response)
	}
}

func TestARefusedLoginNoteDoesNotInventAToken(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.refuseLogin = true
	rec, err := newRunner(t, srv).Run(context.Background(),
		loginChain(chain.Expectation{Path: "error.code", Equals: "unauthenticated"}), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	note := rec.Steps[0].Note
	if !strings.Contains(note, "returned no token") || strings.Contains(note, "belongs to a different principal") {
		t.Fatalf("a refused login carries no token, so it cannot belong to anyone: %q", note)
	}
}
