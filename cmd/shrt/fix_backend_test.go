package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fixThing struct {
	mu          sync.Mutex
	unique      string
	dupCode     string
	dupMsg      string
	refuse      bool
	refuseAfter int
	refuseEven  bool
	refuseMsg   string
	drop        bool
	dropFetch   bool
	idem        bool
	fetchStored bool
	fetchName   string
	createCode  string
	fetchCode   string
	total       int
	tier        string
	creates     int
	next        int
	seen        map[string]bool
	byKey       map[string]map[string]any
	things      map[string]map[string]any
}

func (f *fixThing) set(change func(*fixThing)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func fixErr(code, msg string) map[string]any {
	e := map[string]any{"code": code}
	if msg != "" {
		e["message"] = msg
	}
	return map[string]any{"error": e}
}

func hangUp(w http.ResponseWriter) {
	if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
		conn.Close()
	}
}

func (f *fixThing) create(w http.ResponseWriter, body map[string]any) map[string]any {
	f.creates++
	name, _ := body["name"].(string)
	if f.refuse {
		return fixErr("ALREADY_EXISTS", "name "+name+" already exists")
	}
	if f.refuseAfter > 0 && f.creates > f.refuseAfter || f.refuseEven && f.creates > 2 && f.creates%2 == 0 {
		return fixErr("REJECTED", f.refuseMsg)
	}
	key, _ := body["idempotency_key"].(string)
	if prior, ok := f.byKey[key]; f.idem && ok && key != "" {
		return prior
	}
	if f.unique != "" {
		value := name
		if f.unique == "source" {
			meta, _ := body["meta"].(map[string]any)
			value, _ = meta["source"].(string)
		}
		if f.seen[value] {
			code, msg := f.dupCode, f.dupMsg
			if code == "" {
				code, msg = "ALREADY_EXISTS", "name %s already exists"
			}
			if strings.Contains(msg, "%s") {
				msg = fmt.Sprintf(msg, value)
			}
			return fixErr(code, msg)
		}
		f.seen[value] = true
		if f.drop {
			hangUp(w)
			return nil
		}
	}
	if f.createCode != "" {
		return fixErr(f.createCode, "")
	}
	f.next++
	thing := map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(f.next), "name": firstNonEmptyString(name, "widget")}
	f.things[thing["id"].(string)] = thing
	if f.idem && key != "" {
		f.byKey[key] = thing
	}
	return thing
}

func (f *fixThing) fetch(w http.ResponseWriter, body map[string]any) map[string]any {
	if f.dropFetch {
		hangUp(w)
		return nil
	}
	id, _ := body["id"].(string)
	if thing, ok := f.things[id]; f.idem && ok {
		return thing
	}
	code := firstNonEmptyString(f.fetchCode, "OK")
	out := map[string]any{"error": map[string]any{"code": code}, "id": body["id"], "name": firstNonEmptyString(f.fetchName, "widget")}
	if thing, ok := f.things[id]; f.fetchStored && ok {
		out["name"] = thing["name"]
	}
	if f.total != 0 {
		out["total"] = f.total
	}
	return out
}

func (f *fixThing) server(t *testing.T) *httptest.Server {
	t.Helper()
	f.seen, f.byKey, f.things = map[string]bool{}, map[string]map[string]any{}, map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		var out map[string]any
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			out = f.create(w, body)
		case "/shrt.test.v1.ThingService/Fetch":
			out = f.fetch(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if out == nil {
			return
		}
		if f.tier != "" {
			out["tier"] = f.tier
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fixWorkspace(t *testing.T, f *fixThing, chains ...string) *httptest.Server {
	t.Helper()
	srv := f.server(t)
	chdirToFreshCLIWorkspace(t, srv.URL)
	for _, c := range chains {
		name := strings.TrimSpace(strings.SplitN(strings.SplitN(c, "name: ", 2)[1], "\n", 2)[0])
		writeFile(t, ".shrt/chains/"+name+".yaml", c)
	}
	return srv
}

func fixApprove(t *testing.T, chainName string, args ...string) {
	t.Helper()
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, append([]string{chainName, "-quiet"}, args...)); err != nil {
			t.Fatalf("shrt run %s: %v", chainName, err)
		}
		if err := runConfirm(ctx, []string{chainName, "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{chainName, "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
}

func fixCmd(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		switch name {
		case "run":
			err = runRun(context.Background(), args)
		case "verify":
			err = runVerify(context.Background(), args)
		case "diff":
			err = runDiff(context.Background(), args)
		default:
			t.Fatalf("unknown command %s", name)
		}
	})
	return out, err
}

func approveUniqueChain(t *testing.T, ctx context.Context) {
	t.Helper()
	fixApprove(t, "cli-unique")
}

func createForeignThing(t *testing.T, base, name string) {
	t.Helper()
	resp, err := http.Post(base+"/shrt.test.v1.ThingService/Create", "application/json", strings.NewReader(`{"name":"`+name+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

const uniqueNameChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
`

func newUniqueNameBackend() *httptest.Server {
	seen := map[string]bool{}
	next := 0
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			name, _ := body["name"].(string)
			if seen[name] {
				_ = json.NewEncoder(w).Encode(fixErr("ALREADY_EXISTS", "name "+name+" already exists"))
				return
			}
			seen[name] = true
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": name})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newNamingBackend(name *string) *httptest.Server {
	next := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "OK"}, "id": body["id"], "name": *name,
				"created_at": "2026-09-0" + itoa(next) + "T10:00:00Z",
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

type resettableUniqueBackend struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (b *resettableUniqueBackend) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = map[string]bool{}
}

func (b *resettableUniqueBackend) server() *httptest.Server {
	b.reset()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		name, _ := body["name"].(string)
		if b.seen[name] {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "REJECTED", "message": "name " + name + " is already registered"}})
			return
		}
		b.seen[name] = true
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
	}))
}
