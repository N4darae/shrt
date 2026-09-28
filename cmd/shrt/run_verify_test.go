package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/store"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdirToFreshCLIWorkspace(t *testing.T, baseURL string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".shrt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".shrt", "descriptor.binpb"), catalogtest.Descriptor(), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), `target:
    base_url: `+baseURL+`
descriptor:
    file: .shrt/descriptor.binpb
paths:
    chains: .shrt/chains
    runs: .shrt/runs
    safespots: .shrt/safespots
`)
	writeFile(t, filepath.Join(dir, ".shrt", "chains", "cli-thing-flow.yaml"), `apiVersion: shrt/v1
name: cli-thing-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
          - path: id
            not_empty: true
      export:
          thing_id: id
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

func newFakeCLIBackend() *httptest.Server {
	nextID := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := verTBody(r)
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			if meta, _ := body["meta"].(map[string]any); meta != nil && meta["trace_id"] != nil && meta["trace_id"] != "" {
				verTWrite(w, verTOK("id", meta["trace_id"], "name", body["name"]))
				return
			}
			nextID++
			verTWrite(w, verTOK("id", "thing-"+itoa(nextID)))
		case "/shrt.test.v1.ThingService/Fetch":
			verTWrite(w, verTOK("id", body["id"], "name", "widget"))
		default:
			w.WriteHeader(404)
			verTWrite(w, map[string]any{"code": "unimplemented", "message": r.URL.Path})
		}
	}))
}

func itoa(n int) string { return fmt.Sprint(n) }

func newEchoNameBackend() *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := verTBody(r)
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			verTWrite(w, verTOK("id", "thing-"+itoa(next), "name", body["name"]))
		case "/shrt.test.v1.ThingService/Fetch":
			verTWrite(w, verTOK("id", body["id"], "name", "widget"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func approvedThingFlow(t *testing.T) *httptest.Server {
	t.Helper()
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	verTApprove(t, "cli-thing-flow")
	return srv
}

func newTotalBackend(regressed *bool) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := verTBody(r)
		total := 300
		if *regressed {
			total = 999
		}
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			if body["kind"] == "KIND_B" && !*regressed {
				total = 500
			}
			verTWrite(w, verTOK("id", "thing-"+itoa(next), "name", body["name"], "total", total))
		case "/shrt.test.v1.ThingService/Fetch":
			verTWrite(w, verTOK("id", body["id"], "total", total))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func driftWorkspace(t *testing.T, name *string, extra *bool) {
	t.Helper()
	nextID := 0
	verTServe(t, func(w http.ResponseWriter, r *http.Request) {
		body := verTBody(r)
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			nextID++
			verTWrite(w, verTOK("id", "thing-"+itoa(nextID)))
		case "/shrt.test.v1.ThingService/Fetch":
			out := verTOK("id", body["id"], "name", *name)
			if *extra {
				out["traceHint"] = "t-1"
			}
			verTWrite(w, out)
		default:
			w.WriteHeader(404)
		}
	}, true)
	verTApprove(t, "cli-thing-flow")
}

func newSlowFetchBackend(delay *atomic.Int64) *httptest.Server {
	var next atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := verTBody(r)
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			verTWrite(w, verTOK("id", "thing-"+itoa(int(next.Add(1)))))
		case "/shrt.test.v1.ThingService/Fetch":
			time.Sleep(time.Duration(delay.Load()) * time.Millisecond)
			verTWrite(w, verTOK("id", body["id"], "name", "widget", "created_at", "2026-09-01T10:00:00Z"))
		default:
			w.WriteHeader(404)
		}
	}))
}

func resealSafeSpot(t *testing.T, path string) {
	t.Helper()
	spot := &store.SafeSpot{}
	if err := json.Unmarshal(verTRead(t, path), spot); err != nil {
		t.Fatal(err)
	}
	spot.Digest = spot.ComputeDigest()
	raw, err := json.MarshalIndent(spot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw))
}

const dropChain = `apiVersion: shrt/v1
name: cli-drop
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: again
      call: ThingService/Create
      body: {name: other, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
`

func verTBody(r *http.Request) map[string]any {
	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body
}

func verTWrite(w http.ResponseWriter, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func verTOK(kv ...any) map[string]any {
	m := map[string]any{"error": map[string]any{"code": "OK"}}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func verTHangUp(w http.ResponseWriter) {
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		conn.Close()
	}
}

func verTRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verTServe(t *testing.T, h http.HandlerFunc, validate bool, chains ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	if validate {
		writeFile(t, ".shrt/config.yaml", string(verTRead(t, ".shrt/config.yaml"))+"conventions:\n    validate_output: true\n")
	}
	for _, c := range chains {
		writeFile(t, ".shrt/chains/"+verTName(c)+".yaml", c)
	}
	return srv
}

func verTName(chainYAML string) string {
	_, rest, _ := strings.Cut(chainYAML, "\nname: ")
	name, _, _ := strings.Cut(rest, "\n")
	return name
}

func verTApprove(t *testing.T, name string, runArgs ...string) {
	t.Helper()
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, append([]string{name, "-quiet"}, runArgs...)); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{name, "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{name, "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
}

type verTCheck struct {
	args   []string
	argsFn func() []string
	set    func()
	code   int
	has    []string
	not    []string
	then   func(t *testing.T, out string)
}

func verTShrt(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		errOut := captureStderr(t, func() { err = commands[args[0]].run(context.Background(), args[1:]) })
		defer fmt.Print(errOut)
	})
	if err != nil {
		out += "\nERR: " + err.Error()
	}
	return out, exitCodeOf(err)
}

func verTRun(t *testing.T, checks []verTCheck) {
	t.Helper()
	for i, c := range checks {
		if c.set != nil {
			c.set()
		}
		args := c.args
		if c.argsFn != nil {
			args = c.argsFn()
		}
		out, code := verTShrt(t, args...)
		if code != c.code {
			t.Errorf("check %d %v: exit %d, want %d\n%s", i, args, code, c.code, out)
		}
		for _, w := range c.has {
			if !strings.Contains(out, w) {
				t.Errorf("check %d %v: want %q in:\n%s", i, args, w, out)
			}
		}
		for _, w := range c.not {
			if strings.Contains(out, w) {
				t.Errorf("check %d %v: unwanted %q in:\n%s", i, args, w, out)
			}
		}
		if c.then != nil {
			c.then(t, out)
		}
	}
}

func verTRebuild(t *testing.T, matches bool) func() {
	return func() {
		was := descriptorMatchesRebuild
		descriptorMatchesRebuild = func(context.Context, *config.Config) (bool, error) { return matches, nil }
		t.Cleanup(func() { descriptorMatchesRebuild = was })
	}
}

func verTEdit(t *testing.T, path, from, to string) func() {
	return func() {
		raw := string(verTRead(t, path))
		edited := strings.Replace(raw, from, to, 1)
		if edited == raw {
			t.Fatalf("edit of %s did not apply: %q", path, from)
		}
		writeFile(t, path, edited)
	}
}
