package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func restartRecord(id string, steps ...*runner.StepRecord) *runner.Record {
	for i, st := range steps {
		st.Index = i + 1
		if st.AuthProfile == "" {
			st.AuthProfile = "default"
		}
	}
	return &runner.Record{RunID: id, Chain: "cli-thing-flow", Status: runner.StatusError, StartedAt: time.Now(), Steps: steps}
}

func thingStep(id, call, status string, code int, request, response string) *runner.StepRecord {
	return &runner.StepRecord{ID: id, Call: "ThingService/" + call, Status: status, HTTPStatus: code, Request: json.RawMessage(request), Response: json.RawMessage(response)}
}

func createdThing() *runner.StepRecord {
	return thingStep("create", "Create", runner.StatusPassed, 200, `{"name":"widget"}`, `{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)
}

func refusedFetch() *runner.StepRecord {
	st := thingStep("fetch", "Fetch", runner.StatusError, 401, `{"id":"th-4f2a9c"}`, `{"code":"unauthenticated","message":"invalid or expired token"}`)
	st.AuthRetry, st.Transport = runner.AuthRetryNotResent, &runner.TransportError{Code: "unauthenticated", Message: "invalid or expired token"}
	return st
}

func TestASessionLossIsARestartOnlyWithEvidence(t *testing.T) {
	gone := thingStep("again", "Fetch", runner.StatusFailed, 404, `{"id":"th-4f2a9c"}`, `{"code":"not_found","message":"no thing th-4f2a9c"}`)
	gone.BodyRefs, gone.Transport = map[string]string{"id": "${create.id}"}, &runner.TransportError{Code: "not_found", Message: "no thing th-4f2a9c"}
	gateway := thingStep("peek", "Fetch", runner.StatusError, 502, `{"id":"th-4f2a9c"}`, ``)
	gateway.Transport = &runner.TransportError{Code: "http_502", Message: "bad gateway"}
	resent := thingStep("clerk_create", "Create", runner.StatusPassed, 200, `{"name":"clerk"}`, `{"error":{"code":"OK"},"id":"th-77","name":"clerk"}`)
	resent.AuthProfile, resent.AuthRetry = "clerk", runner.AuthRetryResent
	listBefore := thingStep("list", "List", runner.StatusPassed, 200, `{"prefix":"w"}`, `{"error":{"code":"OK"},"things":[{"id":"a"},{"id":"b"}]}`)
	listAfter := thingStep("list_again", "List", runner.StatusPassed, 200, `{"prefix":"w"}`, `{"error":{"code":"OK"},"things":[]}`)
	dup := thingStep("dup", "Create", runner.StatusFailed, 200, `{"name":"widget"}`, `{"error":{"code":"OK"},"id":"th-99","name":"widget"}`)
	dup.BodyRefs = map[string]string{"name": "${steps.create.request.name}"}
	dup.Expect = []chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "ALREADY_EXISTS", Got: "OK"}}
	later := thingStep("fetch_later", "Fetch", runner.StatusPassed, 200, `{"id":"th-4f2a9c"}`, `{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)
	missing := thingStep("get_missing", "Get", runner.StatusPassed, 200, `{"id":"th-4f2a9czz"}`, `{"error":{"code":"REJECTED","reason":"ThingNotFound"}}`)
	missing.BodyRefs = map[string]string{"id": "${create.id}zz"}
	missing.Expect = []chain.ExpectResult{{Path: "error.reason", Rule: "equals", Want: "ThingNotFound", Got: "ThingNotFound", Passed: true}}
	inBand := refusedFetch()
	inBand.HTTPStatus, inBand.Response = 200, json.RawMessage(`{"error":{"code":"UNAUTHENTICATED"}}`)
	for _, c := range []struct {
		name    string
		first   []*runner.StepRecord
		steps   []*runner.StepRecord
		finding bool
		names   []string
	}{
		{"data created before the refusal gone after the re-login proves a restart", nil, []*runner.StepRecord{createdThing(), refusedFetch(), gone}, false, []string{"restarted mid-run", "no thing th-4f2a9c"}},
		{"a gateway answer before a repeated refusal keeps it a restart", nil, []*runner.StepRecord{createdThing(), gateway, refusedFetch()}, false, nil},
		{"a call re-sent and accepted after the re-login is restart evidence", nil, []*runner.StepRecord{createdThing(), resent, refusedFetch()}, false, []string{"clerk_create"}},
		{"a list that shrank after the re-login is restart evidence", nil, []*runner.StepRecord{createdThing(), listBefore, refusedFetch(), listAfter}, false, []string{"list_again"}},
		{"a conflict that vanished after the re-login is restart evidence", nil, []*runner.StepRecord{createdThing(), refusedFetch(), dup}, false, []string{"dup"}},
		{"the refused rpc accepting the fresh login's token is restart evidence", nil, []*runner.StepRecord{createdThing(), refusedFetch(), later}, false, nil},
		{"an expected not-found is no evidence, so a repeated refusal is a finding", nil, []*runner.StepRecord{createdThing(), refusedFetch(), missing}, true, nil},
		{"an in-band refusal and a 401 are not refused the same way", []*runner.StepRecord{createdThing(), inBand}, []*runner.StepRecord{createdThing(), refusedFetch()}, false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := newEchoNameBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			e, err := loadEnv(false)
			if err != nil {
				t.Fatal(err)
			}
			first := c.first
			if first == nil {
				first = c.steps
			}
			saveRun(t, e, restartRecord("20990101T000000Z-ev1", first...))
			saveRun(t, e, restartRecord("20990101T000001Z-ev2", c.steps...))
			rec, err := e.store.LoadRun("cli-thing-flow", "20990101T000001Z-ev2")
			if err != nil {
				t.Fatal(err)
			}
			loss := examineSessionLoss(e, rec)
			if loss == nil || loss.finding() != c.finding {
				t.Fatalf("got %v, want a session loss with finding %v", loss, c.finding)
			}
			for _, want := range c.names {
				if !strings.Contains(loss.line(), want) {
					t.Errorf("the line names %q: %s", want, loss.line())
				}
			}
		})
	}
	read := thingStep("fetch", "Fetch", runner.StatusPassed, 200, `{"id":"th-4f2a9c"}`, `{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)
	read.AuthRetry = runner.AuthRetryResent
	rec := restartRecord("20990101T000000Z-pass1", createdThing(), read)
	rec.Status = runner.StatusPassed
	if loss := detectSessionLoss(rec); loss == nil || strings.Contains(loss.line(), "restarted mid-run") || !strings.Contains(loss.line(), "re-sent") {
		t.Errorf("a passed run whose read was re-sent and accepted says only that: %v", loss)
	}
	for _, c := range []struct {
		status, response, want string
	}{
		{runner.StatusFailed, `{"error":{"code":"OK"},"id":"","name":""}`, ""},
		{runner.StatusPassed, `{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`, "list"},
	} {
		back := thingStep("list", "Fetch", c.status, 200, `{"id":"th-4f2a9c"}`, c.response)
		back.BodyRefs = map[string]string{"id": "${create.id}"}
		if got := readBackAfter(restartRecord("20990101T000000Z-back", createdThing(), refusedFetch(), back), 1); got != c.want {
			t.Errorf("a read answering %s reads the created data back as %q, want %q", c.response, got, c.want)
		}
	}
}

func approvedThingFlowRun(t *testing.T) (context.Context, *env, *runner.Record) {
	t.Helper()
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	base, err := e.store.LoadRun("cli-thing-flow", spot.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, e, base
}

func copyRun(t *testing.T, base *runner.Record, id string) *runner.Record {
	t.Helper()
	raw, _ := json.Marshal(base)
	var rec runner.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.RunID = id
	rec.Seal = ""
	for _, st := range rec.Steps {
		st.AuthProfile = "default"
	}
	return &rec
}

func refuseStep(st *runner.StepRecord, retry, why string) {
	st.AuthRetry, st.Status, st.HTTPStatus, st.Error = retry, runner.StatusError, 401, why
	st.Transport = &runner.TransportError{Code: "unauthenticated", Message: "token rejected"}
	st.Response = json.RawMessage(`{"code":"unauthenticated","message":"token rejected"}`)
}

func saveRun(t *testing.T, e *env, rec *runner.Record) {
	t.Helper()
	if _, err := e.store.SaveRun(rec); err != nil {
		t.Fatal(err)
	}
}

func saveRefusedAtFetch(t *testing.T, e *env, base *runner.Record, id string) {
	t.Helper()
	rec := copyRun(t, base, id)
	rec.Status = runner.StatusError
	refuseStep(rec.Steps[1], runner.AuthRetryNotResent, "")
	saveRun(t, e, rec)
}

const resentRefusedWhy = "unauthenticated: token rejected\n       the backend refused this call at authentication, then a fresh login in this run " +
	"succeeded and the call was re-sent with the new token, and the backend refused that too: the credentials work and the token " +
	"is current, so this may be an auth regression in the backend (this rpc refusing valid tokens)"

func saveResentAndRefusedAtFetch(t *testing.T, e *env, base *runner.Record, id string) {
	t.Helper()
	rec := copyRun(t, base, id)
	rec.Status = runner.StatusError
	refuseStep(rec.Steps[1], runner.AuthRetryResent, resentRefusedWhy)
	saveRun(t, e, rec)
}

type verifyOutcome struct {
	code      int
	has, lack []string
}

func (w verifyOutcome) check(t *testing.T, what string, err error, out string) {
	t.Helper()
	code := 0
	var coded *exitError
	switch {
	case errors.As(err, &coded):
		code = coded.code
	case err != nil:
		code = 1
	}
	text := out
	if err != nil {
		text += "\nerror: " + err.Error()
	}
	if code != w.code {
		t.Errorf("%s: exit %d, want %d:\n%s", what, code, w.code, text)
	}
	for _, s := range w.has {
		if !strings.Contains(text, s) {
			t.Errorf("%s: want %q in:\n%s", what, s, text)
		}
	}
	for _, s := range w.lack {
		if strings.Contains(text, s) {
			t.Errorf("%s: want no %q in:\n%s", what, s, text)
		}
	}
}

func TestVerifyOfATokenRefusedMidRunIsARestartUntilItRepeats(t *testing.T) {
	for _, c := range []struct {
		name          string
		save          func(t *testing.T, e *env, base *runner.Record, id string)
		first, second *verifyOutcome
	}{
		{"a first refusal of a token accepted earlier reads as a likely restart; a repeat at the same rpc is a finding", saveRefusedAtFetch,
			&verifyOutcome{code: 3, has: []string{"restarted mid-run", "WARNING: ", "step 2 fetch"}},
			&verifyOutcome{code: 1, has: []string{"auth refused at", "20990101T000000Z-first"}, lack: []string{"restarted mid-run"}}},
		{"a read refused again after a fresh login may be an auth regression; a repeat is a finding naming the fresh token", saveResentAndRefusedAtFetch,
			&verifyOutcome{code: 3, has: []string{"may be an auth regression"}, lack: []string{"restarted mid-run"}},
			&verifyOutcome{code: 1, has: []string{"FINDING: ", "just issued", "20990101T000000Z-first"}}},
		{"a write refused with a just-issued token and never re-sent stays could-not-verify on a repeat", func(t *testing.T, e *env, base *runner.Record, id string) {
			rec := copyRun(t, base, id)
			rec.Status = runner.StatusError
			refuseStep(rec.Steps[0], runner.AuthRetryNotResent, "unauthenticated: token rejected\n       the backend refused a token that a login in this run had just issued: "+
				"the credentials work and the token is current, so this may be an auth regression in the backend")
			rec.Steps[1].Status, rec.Steps[1].HTTPStatus, rec.Steps[1].Response, rec.Steps[1].Expect = runner.StatusSkipped, 0, nil, nil
			saveRun(t, e, rec)
		},
			&verifyOutcome{code: 3, has: []string{"may be an auth regression"}},
			&verifyOutcome{code: 3, has: []string{"not re-sent"}, lack: []string{"FINDING"}}},
		{"a read the backend lost its session for is could-not-verify, not a regression", func(t *testing.T, e *env, base *runner.Record, id string) {
			rec := copyRun(t, base, id)
			rec.Status = runner.StatusFailed
			fetch := rec.Steps[1]
			fetch.AuthRetry, fetch.Status = runner.AuthRetryResent, runner.StatusFailed
			fetch.Response = json.RawMessage(`{"error":{"code":"NOT_FOUND","message":"no thing"},"id":"","name":"","created_at":"","total":0}`)
			saveRun(t, e, rec)
		},
			&verifyOutcome{code: 3, has: []string{"the backend refused a token it had accepted earlier in this run at step 2", "likely restarted mid-run"}, lack: []string{"regression"}}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, e, base := approvedThingFlowRun(t)
			c.save(t, e, base, "20990101T000000Z-first")
			c.save(t, e, base, "20990101T000001Z-again")
			for i, want := range []*verifyOutcome{c.first, c.second} {
				if want == nil {
					continue
				}
				id := []string{"20990101T000000Z-first", "20990101T000001Z-again"}[i]
				var err error
				out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", id}) })
				want.check(t, id, err, out)
			}
		})
	}
}

type shortSessionBackend struct {
	mu     sync.Mutex
	uses   int
	logins int
	left   map[string]int
	short  bool
	life   time.Duration
	born   map[string]time.Time
}

func (b *shortSessionBackend) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/Login") {
			b.logins++
			token := fmt.Sprintf("tok-%d", b.logins)
			b.left[token] = b.uses
			if b.born == nil {
				b.born = map[string]time.Time{}
			}
			b.born[token] = time.Now()
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "access_token": token,
				"expires_at": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		n, known := b.left[token]
		if !known || b.short && n <= 0 || b.life > 0 && time.Since(b.born[token]) > b.life {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "unauthenticated", "message": "invalid or expired token"})
			return
		}
		b.left[token] = n - 1
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": "widget"})
	}))
}

func shortSessionWorkspace(t *testing.T, b *shortSessionBackend, steps string) {
	t.Helper()
	b.left = map[string]int{}
	srv := b.server()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	t.Setenv("LIFE_USER", "staff")
	t.Setenv("LIFE_PASSWORD", "secret")
	writeFile(t, ".shrt/config.yaml", `target:
    base_url: `+srv.URL+`
descriptor:
    file: .shrt/descriptor.binpb
auth:
    call: shrt.test.v1.AuthService/Login
    body:
        username: ${env.LIFE_USER}
        password: ${env.LIFE_PASSWORD}
    token_path: access_token
    expires_path: expires_at
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
`)
	writeFile(t, filepath.Join(".shrt", "chains", "cli-thing-flow.yaml"), "apiVersion: shrt/v1\nname: cli-thing-flow\nsteps:\n"+steps)
}

const lifetimeWrites = `    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect: [{path: error.code, equals: OK}, {path: id, equals: thing-1}]
    - id: create_again
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect: [{path: error.code, equals: OK}, {path: id, equals: thing-1}]
`

func TestATokenRefusedLongBeforeItsStatedExpiryInTwoRunsIsAFindingNotARestart(t *testing.T) {
	b := &shortSessionBackend{uses: 1, short: true}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("one early refusal is no verdict: a restart since the login explains it too, want exit 3: %v\n%s", err, out)
	}
	if strings.Contains(out, "likely restarted") || strings.Contains(out, "FINDING") {
		t.Fatalf("nothing shows a restart and nothing repeats yet:\n%s", out)
	}
	if !strings.Contains(out, "WARNING: token refused 0s after issue although the login said it expires in 3600s") {
		t.Fatalf("the run must say the token died long before the expiry its login stated:\n%s", out)
	}
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("a token this run's login issued refused early again, as in the previous run, is a finding, exit 1: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FINDING: token refused 0s after issue although the login said it expires in 3600s") ||
		strings.Contains(out, "specific to that rpc") || strings.Contains(out, "likely restarted") {
		t.Fatalf("the finding is about the token's lifetime, not the rpc and not a restart:\n%s", out)
	}
}

func TestTwoTokensOfOneRunRefusedEarlyAreAFindingEvenWhenEveryReadWasResent(t *testing.T) {
	b := &shortSessionBackend{uses: 1, short: true}
	shortSessionWorkspace(t, b, `    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_again
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
    - id: fetch_third
      call: ThingService/Fetch
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
`)
	var err error
	out := captureStdout(t, func() { err = runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if err == nil || errors.As(err, &coded) {
		t.Fatalf("the re-login's own token was refused early too: a finding, exit 1, though every step passed: %v\n%s", err, out)
	}
	if strings.Contains(out, "PASSED") || !strings.Contains(out, "cli-thing-flow: FINDING (every step passed) in ") || !strings.Contains(out, "FINDING: token refused") ||
		!strings.Contains(out, "the fresh token the re-login issued was refused") {
		t.Fatalf("the run passed step by step and the finding names both refusals:\n%s", out)
	}
}

func TestVerifyReportsATokenLifetimeFindingAndNeverCallsItARestart(t *testing.T) {
	b := &shortSessionBackend{uses: 1}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	b.mu.Lock()
	b.short = true
	b.mu.Unlock()
	removeFile(t, ".shrt/tokens.json")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 || strings.Contains(out, "likely restarted") {
		t.Fatalf("one early refusal is could-not-verify, and not called a restart: %v\n%s", err, out)
	}
	if !strings.Contains(out, "token refused 0s after issue although the login said it expires in 3600s") {
		t.Fatalf("verify must name the token's age and stated lifetime:\n%s", out)
	}
	out = captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || errors.As(err, &coded) || !strings.Contains(out, "FINDING: token refused 0s after issue") {
		t.Fatalf("refused early again in the next replay: a finding, exit 1: %v\n%s", err, out)
	}
}

func TestACachedTokenRefusedAfterARestartIsReMintedQuietly(t *testing.T) {
	b := &shortSessionBackend{uses: 100}
	shortSessionWorkspace(t, b, lifetimeWrites)
	ctx := context.Background()
	restart := func() {
		b.mu.Lock()
		b.left = map[string]int{}
		b.mu.Unlock()
	}
	quiet := func(what string, oneLine bool) {
		t.Helper()
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-thing-flow", "-quiet"}) })
		if err != nil || strings.Contains(out, "FINDING") || strings.Contains(out, "WARNING") || oneLine && len(nonEmptyLines(out)) != 1 {
			t.Fatalf("%s: a cached token refused on its first use is re-minted as a restart's note, never a finding: %v\n%s", what, err, out)
		}
	}
	quiet("first run", false)
	for i := 1; i <= 3; i++ {
		restart()
		quiet(fmt.Sprintf("after restart %d", i), i == 1)
	}
	time.Sleep(200 * time.Millisecond)
	quiet("the re-login's token accepted from the cache", false)
	restart()
	quiet("a token accepted in a later run and then refused", false)
}

func bigReadRecord(n, refusedAt int) *runner.Record {
	rec := &runner.Record{RunID: "20990101T000000Z-big", Chain: "big"}
	rec.Steps = append(rec.Steps, &runner.StepRecord{ID: "create", Index: 1, Call: "ThingService/Create", Status: runner.StatusPassed,
		HTTPStatus: 200, AuthProfile: "default", Request: json.RawMessage(`{"name":"widget"}`),
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-1","tags":["a","b"]}`)})
	for i := 0; i < n; i++ {
		st := &runner.StepRecord{ID: fmt.Sprintf("get_%d", i), Index: i + 2, Call: "ThingService/Fetch", Status: runner.StatusPassed,
			HTTPStatus: 200, AuthProfile: "default", Request: json.RawMessage(`{"id":"th-1"}`),
			BodyRefs: map[string]string{"id": "${create.id}"},
			Response: json.RawMessage(`{"error":{"code":"OK"},"thing":{"id":"th-1","tags":["a","b"]}}`)}
		if i == refusedAt {
			st.AuthRetry = runner.AuthRetryResent
		}
		rec.Steps = append(rec.Steps, st)
	}
	return rec
}

func TestSessionRestartEvidenceOnALongReadChainIsNotQuadratic(t *testing.T) {
	rec := bigReadRecord(20000, 10000)
	start := time.Now()
	if got := sessionRestartEvidence(rec, 10001); got != "" {
		t.Fatalf("identical reads after the refusal are no restart evidence, got %q", got)
	}
	if got := restartEvidence(rec, 10001); got == "" {
		t.Fatal("the resent read accepted after a fresh login is restart evidence")
	}
	if got := readBackAfter(rec, 10001); got != "get_10001" {
		t.Fatalf("the first clean read after the refusal reads the created id back, got %q", got)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("scanning a 20000-step record for restart evidence took %s; it must stay linear", took)
	}
}

func bruteShrunkList(rec *runner.Record, index int, st *runner.StepRecord) string {
	if !answeredCleanly(st) {
		return ""
	}
	var after any
	if json.Unmarshal(st.Response, &after) != nil {
		return ""
	}
	key, ok := requestKey(st)
	if !ok {
		return ""
	}
	for _, prior := range rec.Steps[:index] {
		if !answeredCleanly(prior) || prior.Call != st.Call {
			continue
		}
		if pk, ok := requestKey(prior); !ok || pk != key {
			continue
		}
		var before any
		if json.Unmarshal(prior.Response, &before) == nil && listShrank(before, after) {
			return prior.ID
		}
	}
	return ""
}

func listShrank(before, after any) bool {
	switch b := before.(type) {
	case map[string]any:
		a, ok := after.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range b {
			if list, isList := v.([]any); isList && len(list) > 0 {
				other, _ := a[k].([]any)
				if len(other) < len(list) {
					return true
				}
				continue
			}
			if listShrank(v, a[k]) {
				return true
			}
		}
	}
	return false
}

func TestShrunkListIndexAgreesWithAPairwiseScan(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	bodies := func() string {
		list := func() string {
			n := r.Intn(4)
			out := "["
			for i := 0; i < n; i++ {
				if i > 0 {
					out += ","
				}
				out += fmt.Sprint(i)
			}
			return out + "]"
		}
		switch r.Intn(5) {
		case 0:
			return fmt.Sprintf(`{"items":%s}`, list())
		case 1:
			return fmt.Sprintf(`{"page":{"items":%s,"other":%s}}`, list(), list())
		case 2:
			return fmt.Sprintf(`{"items":%s,"page":{"items":%s}}`, list(), list())
		case 3:
			return `{"page":"none"}`
		}
		return `{}`
	}
	for round := 0; round < 200; round++ {
		rec := &runner.Record{}
		for i := 0; i < 30; i++ {
			rec.Steps = append(rec.Steps, &runner.StepRecord{ID: fmt.Sprintf("s%d", i), Call: fmt.Sprintf("S/List%d", r.Intn(2)),
				Status: runner.StatusPassed, HTTPStatus: 200, Request: json.RawMessage(fmt.Sprintf(`{"q":%d}`, r.Intn(2))),
				Response: json.RawMessage(bodies())})
		}
		index := r.Intn(len(rec.Steps))
		scan := newPriorScan(rec, index)
		for _, st := range rec.Steps[index:] {
			if got, want := scan.shrunkList(st), bruteShrunkList(rec, index, st); got != want {
				t.Fatalf("round %d step %s: index says %q, pairwise scan %q", round, st.ID, got, want)
			}
		}
	}
}
