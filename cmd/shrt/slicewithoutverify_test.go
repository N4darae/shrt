package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func stockBackend(fetchBias int) *httptest.Server {
	var mu sync.Mutex
	totals := map[string]int{}
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		n := 0
		switch q := body["qty"].(type) {
		case string:
			for _, ch := range q {
				n = n*10 + int(ch-'0')
			}
		case float64:
			n = int(q)
		}
		w.Header().Set("Content-Type", "application/json")
		ok := map[string]any{"code": "OK"}
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			id := ""
			if meta, _ := body["meta"].(map[string]any); meta != nil {
				id, _ = meta["trace_id"].(string)
			}
			if id == "" {
				next++
				id = "thing-" + itoa(next)
			}
			totals[id] += n
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "id": id, "total": totals[id]})
		case "/shrt.test.v1.ThingService/Fetch":
			id, _ := body["id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": ok, "id": id, "name": "widget", "total": totals[id] + fetchBias})
		default:
			w.WriteHeader(404)
		}
	}))
}

const stockChain = `apiVersion: shrt/v1
name: stock
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget
          qty: 6
      expect:
          - path: error.code
            equals: OK
    - id: stray_add
      call: ThingService/Create
      body:
          qty: 9
          meta:
              trace_id: ${make.id}
      expect:
          - path: error.code
            equals: DENIED
    - id: fetch_total
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: total
            equals: 6
    - id: fetch_name
      call: ThingService/Fetch
      body:
          id: ${make.id}
      expect:
          - path: name
            equals: gadget
`

func stockWorkspace(t *testing.T, fetchBias int) {
	t.Helper()
	srv := stockBackend(fetchBias)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/stock.yaml", stockChain)
	if err := runRun(context.Background(), []string{"stock", "-quiet", "-keep-going"}); err == nil {
		t.Fatalf("the chain must fail")
	}
}

func stockWithout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"stock"}, args...))
	})
	return out, err
}

func TestSliceWithoutVerifyNamesTheFailuresTheLeftOutWriteCaused(t *testing.T) {
	stockWorkspace(t, 0)
	out, err := stockWithout(t, "-without", "stray_add", "-verify")
	if exitCodeOf(err) != 1 {
		t.Fatalf("fetch_name still fails, so exit 1, got %d: %v\n%s", exitCodeOf(err), err, out)
	}
	if !strings.Contains(out, "cause confirmed for 1 of 2 step(s)") || !strings.Contains(out, "fetch_total") {
		t.Fatalf("fetch_total passes without stray_add:\n%s", out)
	}
	if !strings.Contains(out, "still fail, so another cause: fetch_name") {
		t.Fatalf("fetch_name fails the same way without it:\n%s", out)
	}
}

func TestSliceWithoutVerifyDoesNotCountAFailureWhoseExpectationCountsOnTheLeftOutWrite(t *testing.T) {
	srv := stockBackend(1)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/stock.yaml", strings.NewReplacer("equals: DENIED", "equals: OK", "equals: 6", "equals: 15").Replace(stockChain))
	if err := runRun(context.Background(), []string{"stock", "-quiet", "-keep-going"}); err == nil {
		t.Fatalf("the chain must fail")
	}
	out, err := stockWithout(t, "-without", "stray_add", "-verify", "-json")
	var payload struct {
		Verify withoutVerdict `json:"verify"`
	}
	if jerr := json.Unmarshal([]byte(out), &payload); jerr != nil {
		t.Fatalf("json: %v\n%s", jerr, out)
	}
	v := payload.Verify
	if exitCodeOf(err) != 1 || len(v.NotCounted) != 1 || v.NotCounted[0] != "fetch_total" || len(v.StillFail) != 1 || v.StillFail[0] != "fetch_name" {
		t.Fatalf("fetch_total expects what stray_add added, so without it it fails with another value and is not counted: %d %v %+v\n%s", exitCodeOf(err), err, v, out)
	}
}
