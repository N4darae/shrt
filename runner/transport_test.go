package runner_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
	"github.com/N4darae/shrt/transport"
)

func fetchA(expect ...chain.Expectation) *chain.Chain {
	if expect == nil {
		expect = okExpect()
	}
	return flow(skipAuth(step("fetch", "ThingService/Fetch", byID("a"), expect...)))
}

func TestTheBuildIsStampedIntoTheRecord(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		builds      []string
		want        []string
		warn        [2]string
	}{
		{name: "a label", label: "rc-1", want: []string{"rc-1"}},
		{name: "the server's build header", builds: []string{"b-7"}, want: []string{"b-7"}},
		{name: "a build that changes mid-run", builds: []string{"b-7", "b-7", "b-8"}, want: []string{"b-7", "b-8"}, warn: [2]string{"fetch", "b-8"}},
		{name: "a label the server contradicts", label: "rc-1", builds: []string{"b-7"}, want: []string{"rc-1"}, warn: [2]string{"create", "b-7"}},
	} {
		srv := newFakeServer()
		srv.builds = tc.builds
		cfg := testConfig(srv.URL)
		if tc.builds != nil {
			cfg.Target.BuildHeader = "X-Build"
		}
		r, _, err := runner.NewFromConfig(t.Context(), cfg, catalogtest.New())
		if err != nil {
			t.Fatal(err)
		}
		rec := run(t, r, testChain(), runner.Options{Build: tc.label})
		srv.Close()
		for _, w := range tc.want {
			if !rec.Passed() || !strings.Contains(rec.Build, w) {
				t.Errorf("%s: record build = %q, want %q", tc.name, rec.Build, w)
			}
		}
		if tc.warn[0] != "" && !strings.Contains(stepByID(t, rec, tc.warn[0]).Warning, tc.warn[1]) {
			t.Errorf("%s: step %s must warn about %s", tc.name, tc.warn[0], tc.warn[1])
		}
	}
}

func TestACallWithNoAnswerIsSentAndMayHaveTakenEffect(t *testing.T) {
	dropped := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}, nil)
	msg := run(t, dropped, normalized(t, fetchA()), runner.Options{}).Steps[0].Error
	if !strings.Contains(msg, "sent, no answer") || strings.HasPrefix(msg, "not sent") || !strings.Contains(msg, "whether the call took effect is unknown") {
		t.Fatalf("the backend read the whole request before closing, so it was sent and got no answer, got %q", msg)
	}
	slow := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		answering(200, "application/json", `{"error":{"code":"OK"},"id":"x"}`)(w, r)
	}, func(c *config.Config) { c.Target.Timeout = "80ms" })
	st := run(t, slow, normalized(t, fetchA()), runner.Options{}).Steps[0]
	if !strings.Contains(st.Error, "sent, no answer before target.timeout (80ms)") || st.LatencyMS < 70 {
		t.Fatalf("a timed-out call says so with the timeout, and its latency is the time it waited: %q %dms", st.Error, st.LatencyMS)
	}
}

func TestAnUnavailableAnswerIsNotAVerdictAboutTheService(t *testing.T) {
	for _, tc := range []struct {
		name, ctype, body string
		status            int
	}{
		{"connect unavailable", "application/json", `{"code":"unavailable","message":"upstream connect error"}`, http.StatusServiceUnavailable},
		{"bare 502", "text/html", `<html>Bad Gateway</html>`, http.StatusBadGateway},
		{"bare 504", "text/plain", `gateway timeout`, http.StatusGatewayTimeout},
	} {
		rec := run(t, bareRunner(t, answering(tc.status, tc.ctype, tc.body), nil), normalized(t, fetchA()), runner.Options{})
		st := rec.Steps[0]
		if st.Status != runner.StatusError || rec.Status != runner.StatusError || !runner.NotAnsweredByService(st) || !strings.Contains(st.Error, "not answered by the service") {
			t.Fatalf("%s: a gateway's answer is not the service's verdict: step %s, run %s, %q", tc.name, st.Status, rec.Status, st.Error)
		}
	}
	r := bareRunner(t, answering(http.StatusServiceUnavailable, "application/json", `{"code":"unavailable","message":"draining"}`), nil)
	if st := run(t, r, normalized(t, fetchA(chain.Expectation{Path: "transport.code", Equals: "unavailable"})), runner.Options{}).Steps[0]; st.Status != runner.StatusPassed {
		t.Fatalf("a step asserting unavailable passes when it gets it: %s %s", st.Status, st.Error)
	}
}

func TestAReadWithAServerErrorIsResentOnceAndAWriteNever(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	r := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		n := calls[r.URL.Path]
		mu.Unlock()
		if n == 1 {
			answering(500, "application/json", `{"code":"internal","message":"pool exhausted"}`)(w, r)
			return
		}
		answering(200, "application/json", `{"error":{"code":"OK"},"id":"a","name":"widget"}`)(w, r)
	}, nil)
	create := skipAuth(step("create", "ThingService/Create", map[string]any{"name": "widget"}))
	create.AllowFail = true
	fetch := fetchA().Steps[0]
	fetch.Export = map[string]string{"got": "name"}
	rec := run(t, r, normalized(t, flow(create, fetch, skipAuth(step("echo", "ThingService/Fetch", byID("${got}"), okExpect()...)))), runner.Options{})
	c, f, e := rec.Steps[0], rec.Steps[1], rec.Steps[2]
	if c.FirstAttempt != nil || c.Transport == nil || calls["/shrt.test.v1.ThingService/Create"] != 1 {
		t.Fatalf("a write is never re-sent: %+v", c)
	}
	if f.Status != runner.StatusPassed || f.FirstAttempt == nil || f.FirstAttempt.Code != "internal" || f.FirstAttempt.HTTPStatus != 500 || !strings.Contains(f.Warning, "re-sent once") {
		t.Fatalf("a read's server error is re-sent once, both attempts recorded and the step judged on the answer: %+v", f)
	}
	if e.Status != runner.StatusPassed || calls["/shrt.test.v1.ThingService/Fetch"] != 3 {
		t.Fatalf("a step reading the re-sent answer runs on it: %s", e.Status)
	}
	n := 0
	asserted := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		answering(500, "application/json", `{"code":"internal","message":"boom"}`)(w, r)
	}, nil)
	if st := run(t, asserted, normalized(t, fetchA(chain.Expectation{Path: "transport.code", Equals: "internal"})), runner.Options{}).Steps[0]; n != 1 || st.FirstAttempt != nil || st.Status != runner.StatusPassed {
		t.Fatalf("an asserted server error is the answer, not re-sent: %d calls, %+v", n, st)
	}
}

func TestASlowReadIsReMeasuredAndAWriteNever(t *testing.T) {
	for _, tc := range []struct {
		name          string
		slow          func(int) bool
		c             *chain.Chain
		resent, calls int
	}{
		{"a read stops at the first fast answer", func(n int) bool { return n == 1 }, fetchA(), 1, 2},
		{"a steadily slow read is re-measured as often as asked", func(int) bool { return true }, fetchA(), 2, 3},
		{"a slow write is never re-sent", func(int) bool { return true }, flow(skipAuth(step("create", "ThingService/Create", thing("w"), okExpect()...))), 0, 1},
	} {
		var mu sync.Mutex
		calls := 0
		r := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if tc.slow(n) {
				time.Sleep(60 * time.Millisecond)
			}
			answering(200, "application/json", `{"error":{"code":"OK"}}`)(w, r)
		}, nil)
		st := run(t, r, normalized(t, tc.c), runner.Options{LatencySuspect: func(_ string, got int64) bool { return got >= 40 }, Remeasure: 2}).Steps[0]
		if len(st.LatencyResent) != tc.resent || calls != tc.calls || st.LatencyMS < 50 {
			t.Fatalf("%s: resent %v after %d calls, first latency %dms", tc.name, st.LatencyResent, calls, st.LatencyMS)
		}
		if tc.resent == 1 && st.LatencyResent[0] >= 40 {
			t.Fatalf("%s: the re-measurement was fast: %v", tc.name, st.LatencyResent)
		}
	}
}

func TestAServerStreamingStepAssertsItsFirstMessageAndItsRefusal(t *testing.T) {
	frame := func(flags byte, payload string) []byte {
		out := make([]byte, 5, 5+len(payload))
		out[0] = flags
		binary.BigEndian.PutUint32(out[1:], uint32(len(payload)))
		return append(out, payload...)
	}
	r := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/connect+json")
		if r.Header.Get("X-Deny") != "" {
			_, _ = w.Write(frame(2, `{"error":{"code":"unauthenticated","message":"no token"}}`))
			return
		}
		_, _ = w.Write(frame(0, `{"idOrder":"o-1","state":"OPEN"}`))
		_, _ = w.Write(frame(2, `{}`))
	}, nil)
	r.Catalog = catalogtest.Rich()
	c := normalized(t, flow(
		step("watch", "OrderService/WatchOrder", map[string]any{"id_order": "o-1"}, chain.Expectation{Path: "messages.0.state", Equals: "OPEN"}, chain.Expectation{Path: "messages.1", Exists: no()}),
		&chain.Step{ID: "denied", Call: "OrderService/WatchOrder", Body: map[string]any{"id_order": "o-1"}, Headers: map[string]string{"X-Deny": "1"},
			Expect: []chain.Expectation{{Path: "transport.code", Equals: "unauthenticated"}}}))
	if rec := run(t, r, c, runner.Options{}); rec.Status != runner.StatusPassed {
		t.Fatalf("both steps must pass: %s\n%s", rec.Status, rec.Failure)
	}
}

func TestRecordVarsRoundTripLargeIntegersExactly(t *testing.T) {
	var out runner.Record
	in := &runner.Record{RunID: "r", Vars: map[string]any{"tag": int64(1790233617019699993), "small": int64(12345678), "ratio": 0.5, "name": "x"}}
	if err := json.Unmarshal([]byte(recordText(t, in)), &out); err != nil {
		t.Fatal(err)
	}
	if out.RunID != "r" || out.Vars["tag"] != int64(1790233617019699993) || out.Vars["small"] != int64(12345678) || out.Vars["ratio"] != 0.5 || out.Vars["name"] != "x" {
		t.Fatalf("integers come back exact, fractions as floats, text as text: %#v", out.Vars)
	}
}

func TestTheReservedProfileNameIsOneName(t *testing.T) {
	if config.InvalidTokenProfile != transport.InvalidTokenProfile || chain.InvalidTokenAuth != transport.InvalidTokenProfile {
		t.Fatalf("config reserves %q, lint checks %q and the transport honours %q", config.InvalidTokenProfile, chain.InvalidTokenAuth, transport.InvalidTokenProfile)
	}
}

func TestRecordConfirmReplayDetectsRegression(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	root := t.TempDir()
	st := store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
	r := newRunner(t, srv)
	first := run(t, r, testChain(), runner.Options{})
	if _, err := st.SaveRun(first); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Promote(first, store.Confirmation{By: "agent"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatal("an agent must not be able to create a safe spot")
	}
	spot, _, err := st.Promote(first, store.Confirmation{By: "reviewer", Acknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep := diff.Compare(spot, run(t, r, testChain(), runner.Options{})); !rep.Clean() {
		t.Fatalf("a faithful replay must not drift:\n%s", rep.Text())
	}
	srv.setDrift("gadget")
	rep := diff.Compare(spot, run(t, r, testChain(), runner.Options{}))
	for _, c := range rep.Changes {
		if c.Step == "fetch" && c.Path == "name" && c.Want == "widget" && c.Got == "gadget" {
			return
		}
	}
	t.Fatalf("the diff must name the step and field that changed:\n%s", rep.Text())
}

func TestASliceReproducesTheTargetStepVerdict(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	r := newRunner(t, srv)
	c := normalized(t, flow(
		step("create_subject", "ThingService/Create", map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"}, chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "id", NotEmpty: true}),
		step("create_noise", "ThingService/Create", map[string]any{"name": "unrelated", "kind": "KIND_A", "idempotency_key": "${uuid}"}, okExpect()...),
		step("fetch_subject", "ThingService/Fetch", byID("${create_subject.id}"), chain.Expectation{Path: "error.code", Equals: "OK"}, chain.Expectation{Path: "name", Equals: "widget"})))
	verdict := func(rec *runner.Record) chain.Verdict {
		sr := stepByID(t, rec, "fetch_subject")
		var response any
		_ = json.Unmarshal(sr.Response, &response)
		code, _ := chain.Get(response, "error.code")
		return chain.Verdict{Step: sr.ID, Status: sr.Status, ErrorCode: fmt.Sprint(code), Expect: sr.Expect}
	}
	source := run(t, r, c, runner.Options{})
	res, err := chain.Slice(c, "fetch_subject", chain.SliceOptions{})
	if err != nil || !source.Passed() || len(res.Kept) != 2 {
		t.Fatalf("the slice keeps create_subject and fetch_subject of a passing run: %v %s %d", err, source.Failure, len(res.Kept))
	}
	replay := run(t, r, res.Chain, runner.Options{})
	if diffs := chain.CompareVerdicts(verdict(source), verdict(replay)); len(diffs) != 0 || len(replay.Steps) >= len(source.Steps) {
		t.Fatalf("the slice reproduces the target's verdict in fewer calls: %v, %d vs %d steps", diffs, len(replay.Steps), len(source.Steps))
	}
	srv.setDrift("something-else")
	if diffs := chain.CompareVerdicts(verdict(source), verdict(run(t, r, res.Chain, runner.Options{}))); len(diffs) == 0 {
		t.Fatal("the backend answered differently and the comparison called it reproduced")
	}
}
