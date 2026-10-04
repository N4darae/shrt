package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

const flakyChain = `apiVersion: shrt/v1
name: cli-flaky
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_again
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: fetch2
      call: ThingService/Fetch
      body: {id: thing-10}
      expect:
          - path: error.code
            equals: OK
    - id: fetch3
      call: ThingService/Fetch
      body: {id: thing-11}
      expect:
          - path: error.code
            equals: OK
`

type flakyServer struct {
	mu     sync.Mutex
	n      int
	failAt map[int]bool
	code   string
	status int
	delay  time.Duration
	name11 string
}

func (f *flakyServer) set(code string, status int, at ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n, f.code, f.status, f.failAt = 0, code, status, map[int]bool{}
	for _, i := range at {
		f.failAt[i] = true
	}
}

func (f *flakyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	fail := false
	if strings.HasSuffix(r.URL.Path, "/Fetch") {
		f.n++
		fail = f.failAt[f.n]
	}
	code, status, delay, name := f.code, f.status, f.delay, "widget"
	if body["id"] == "thing-11" && f.name11 != "" {
		name = f.name11
	}
	f.mu.Unlock()
	time.Sleep(delay)
	w.Header().Set("Content-Type", "application/json")
	if fail {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "pool exhausted"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": name})
}

func flakyWorkspace(t *testing.T) (*flakyServer, context.Context) {
	f := &flakyServer{}
	f.set("internal", http.StatusInternalServerError)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-flaky.yaml", flakyChain)
	ctx := context.Background()
	fixApprove(t, "cli-flaky")
	return f, ctx
}

func verifyOnce(t *testing.T, ctx context.Context) (string, error) {
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-flaky", "-quiet"}) })
	return out, err
}

func wantExit1(t *testing.T, what string, err error, out string) {
	t.Helper()
	var coded *exitError
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("%s: an intermittent failure is a defect, exit 1, never 0 or 3: %v\n%s", what, err, out)
	}
}

func TestAServerErrorOnARequestTheBackendAnsweredInTheSameRunIsAnIntermittentFinding(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"internal", 500}, {"unknown", 500}, {"resource_exhausted", 429}, {"data_loss", 500},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f, ctx := flakyWorkspace(t)
			f.set(tc.code, tc.status, 2, 3)
			out, err := verifyOnce(t, ctx)
			wantExit1(t, "fetch_again failed", err, out)
			msg := err.Error()
			if !strings.Contains(msg, "intermittent failure at ThingService/Fetch") || strings.HasPrefix(msg, "regression") {
				t.Fatalf("a %s at fetch_again, whose request fetch had answered in the same run, is an intermittent failure, not a regression: %v\n%s", tc.code, err, out)
			}
			if !strings.Contains(out, "same request at step 1 fetch in this run") || !strings.Contains(out, "FINDING: intermittent failure") {
				t.Fatalf("the finding names its evidence: %v\n%s", err, out)
			}
		})
	}
}

func TestAServerErrorBesideAnotherFailureOfItsRpcIsABackendChange(t *testing.T) {
	f := &flakyServer{}
	f.set("internal", http.StatusInternalServerError)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-flaky.yaml", strings.Replace(flakyChain, "body: {id: thing-11}\n      expect:\n", "body: {id: thing-11}\n      expect:\n          - path: name\n            equals: widget\n", 1))
	fixApprove(t, "cli-flaky")
	f.set("internal", http.StatusInternalServerError, 3, 4)
	f.name11 = "gadget"
	out, err := verifyOnce(t, context.Background())
	wantExit1(t, "fetch2 failed", err, out)
	if strings.Contains(out, "looks intermittent") || !strings.Contains(out, "ThingService/Fetch also failed in this run at fetch3, without a server error: a backend change at that rpc") {
		t.Fatalf("another call of the rpc answering otherwise in the same run points at a backend change, not chance: %v\n%s", err, out)
	}
}

func TestAServerErrorThatMovesBetweenRunsIsIntermittentAndOneThatStaysIsARegression(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("internal", 500, 3, 4)
	out, err := verifyOnce(t, ctx)
	wantExit1(t, "verify 1", err, out)
	if !strings.HasPrefix(err.Error(), "regression") || !strings.Contains(out, "this looks intermittent") ||
		!strings.Contains(out, "the previous run that sent step fetch2, had it answered as expected") {
		t.Fatalf("verify 1: one failure on a request nothing else in the run sent stays a regression, with a note that the previous run answered it: %v\n%s", err, out)
	}
	f.set("internal", 500, 4, 5)
	out, err = verifyOnce(t, ctx)
	wantExit1(t, "verify 2", err, out)
	if !strings.Contains(err.Error(), "intermittent failure at ThingService/Fetch") ||
		!strings.Contains(out, "failed at step 3 fetch2 instead with the same error, and answered step fetch3 as expected") {
		t.Fatalf("verify 2: the previous verify failed at another step with the same error, so this is intermittent: %v\n%s", err, out)
	}
	f.set("internal", 500, 4, 5)
	out, err = verifyOnce(t, ctx)
	wantExit1(t, "verify 3", err, out)
	if !strings.HasPrefix(err.Error(), "regression") || strings.Contains(out, "intermittent") {
		t.Fatalf("verify 3: the same failure at the same step as the previous run is a regression, not intermittent: %v\n%s", err, out)
	}
}

func TestRunSummarySaysIntermittent(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("internal", 500, 2)
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-flaky", "-quiet"}) })
	wantExit1(t, "run", err, out)
	if !strings.Contains(out, "FINDING: intermittent failure at ThingService/Fetch") || strings.Count(out, "a re-run may pass and does not clear it") != 1 {
		t.Fatalf("run's summary says the failure looks intermittent, with evidence, and once what that means: %v\n%s", err, out)
	}
}

func TestAServerErrorAtTheSameStepAsThePreviousRunIsARepeatedFailureARerunDoesNotClear(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("internal", 500, 2, 3)
	if out, err := verifyOnce(t, ctx); err == nil || !strings.Contains(err.Error(), "intermittent failure") || strings.Contains(out, "a re-run may pass") {
		t.Fatalf("verify 1: a first failure is intermittent, and what that means is said by run and the gate, or verify -v: %v\n%s", err, out)
	}
	f.set("internal", 500, 2, 3)
	out, err := verifyOnce(t, ctx)
	wantExit1(t, "verify 2", err, out)
	msg := err.Error()
	if !strings.Contains(msg, "repeated failure at ThingService/Fetch") || strings.Contains(out, "a re-run may pass") ||
		!strings.Contains(out, "failed at the same step(s) the same way") || strings.Contains(out, "a re-run fails the same way") ||
		!strings.Contains(out, "the errors hid the checks of fetch_again") {
		t.Fatalf("verify 2: the same failure at the same step as the previous run is said so, with the checks it hid: %v\n%s", err, out)
	}
	f.set("internal", 500, 2, 3)
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-flaky", "-quiet", "-v"}) })
	if !strings.Contains(out, "a re-run fails the same way") {
		t.Fatalf("verify -v says what a repeated failure means: %v\n%s", err, out)
	}
}

func TestAReadThatGetsAServerErrorIsResentOnceAndJudgedOnTheAnswerWhileTheFindingStays(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	f.set("internal", 500, 2, 5)
	out, err := verifyOnce(t, ctx)
	wantExit1(t, "verify", err, out)
	msg := err.Error()
	if !strings.Contains(msg, "intermittent failure at ThingService/Fetch (failed 2 of 6 calls)") ||
		!strings.Contains(out, "intermittent failure at ThingService/Fetch: it failed 2 of 6 calls in this run, and answered") ||
		!strings.Contains(out, "step 2 fetch_again, step 4 fetch3 got internal: pool exhausted, each re-send answered and judged") ||
		strings.Contains(out, "hid the checks") || strings.HasPrefix(msg, "regression") {
		t.Fatalf("each failed read is re-sent once, judged on the answer, and the failure is still a finding with its rate: %v\n%s", err, out)
	}
	f.set("internal", 500, 2, 5)
	out, err = verifyOnce(t, ctx)
	wantExit1(t, "verify 2", err, out)
	if !strings.Contains(err.Error(), "repeated failure at ThingService/Fetch") {
		t.Fatalf("the same first-attempt failure at the same steps as the previous run is a repeated failure: %v\n%s", err, out)
	}
}

func TestTheCallRateNamesAPeriodOnlyFromThreeFailures(t *testing.T) {
	rec := &runner.Record{}
	for i := 1; i <= 9; i++ {
		st := &runner.StepRecord{Index: i, ID: "s", Call: "A/Get", Status: runner.StatusPassed, HTTPStatus: 200}
		if i%3 == 0 {
			st.FirstAttempt = &runner.Attempt{HTTPStatus: 500, Code: "internal", Message: "pool exhausted"}
		}
		rec.Steps = append(rec.Steps, st)
	}
	if got := callRate(rec, "A/Get"); got != "it failed 3 of 12 calls in this run, every 4th, and answered the others" {
		t.Fatalf("each re-send is a call too, so a failure at every 3rd step is every 4th call: %q", got)
	}
	rec.Steps = rec.Steps[:6]
	if got := callRate(rec, "A/Get"); got != "it failed 2 of 8 calls in this run, and answered the others" {
		t.Fatalf("two failures name no period: %q", got)
	}
	rec.Steps = rec.Steps[:3]
	if got := callRate(rec, "A/Get"); got != "" {
		t.Fatalf("one failure is not a rate: %q", got)
	}
}

func TestAConfirmedLatencyRegressionLeadsVerifyOverAnIntermittentFinding(t *testing.T) {
	f, ctx := flakyWorkspace(t)
	cfg, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(cfg)+"latency:\n    floor_ms: 100\n    fail: true\n")
	f.set("internal", http.StatusInternalServerError, 2, 3)
	f.mu.Lock()
	f.delay = 150 * time.Millisecond
	f.mu.Unlock()
	out, err := verifyOnce(t, ctx)
	if err == nil || !strings.Contains(err.Error(), "latency regression") || !strings.Contains(out, "FINDING: intermittent failure at ThingService/Fetch") {
		t.Fatalf("the latency regression leads, the finding is still printed: %v\n%s", err, out)
	}
}

func TestAReadFailingBehindAnIntermittentWriteIsNoOtherFailure(t *testing.T) {
	busy := func(st *runner.StepRecord) {
		st.Status, st.Response, st.HTTPStatus = runner.StatusError, nil, 503
		st.Transport = &runner.TransportError{Code: "unavailable", Message: "stock store busy"}
	}
	rec := shopRecord(
		shopStep("create_product", shopCreate, `{"product":{"id_product":"p1","qty_on_hand":"0"}}`),
		shopStep("add_stock_1", shopAdd, `{"qty_on_hand":"1"}`, "create_product"),
		shopStep("add_stock_2", shopAdd, ``, "create_product").with(busy),
		shopStep("add_stock_3", shopAdd, `{"qty_on_hand":"2"}`, "create_product"),
		shopStep("get_product", shopGet, `{"product":{"id_product":"p1","qty_on_hand":"2"}}`, "create_product").failing("product.qty_on_hand", "3", "2"),
		shopStep("create_other", shopCreate, `{"product":{"id_product":"p2","sku":"B"}}`).failing("product.sku", "A", "B"),
	)
	flaky := &intermittentFailure{rec: rec, steps: []flakyStep{{step: rec.Steps[2]}}}
	if got := flaky.otherFailures(&env{cat: catalogtest.Shop()}, rec); strings.Join(got, ",") != "create_other" {
		t.Errorf("get_product failed because add_stock_2 was refused, so only create_other is another failure, got %q", got)
	}
}
