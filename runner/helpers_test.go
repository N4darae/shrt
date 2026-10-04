package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

type fakeServer struct {
	*httptest.Server

	mu            sync.Mutex
	headers       []http.Header
	bodies        []map[string]any
	logins        int
	partnerLogins int
	creates       int
	calls         []string
	tokens        map[string]bool
	partnerTokens map[string]bool
	nextID        int
	rejectAll     bool
	drift         string
	unknownField  bool
	tokenKey      bool
	batchUnset    bool
	refuseLogin   bool
	inBand        bool
	inBandPath    string
	batchDrift    bool
	itemKey       string
	receiptDrift  bool
	createRefusal string
	echoHeader    string
	issued        map[string]string
	partnerIssued map[string]string
	builds        []string
}

func newFakeServer() *fakeServer {
	f := &fakeServer{tokens: map[string]bool{}, partnerTokens: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.URL.Path)
	f.headers = append(f.headers, r.Header.Clone())
	if len(f.builds) > 0 {
		w.Header().Set("X-Build", f.builds[0])
		if len(f.builds) > 1 {
			f.builds = f.builds[1:]
		}
	}

	body := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.bodies = append(f.bodies, body)

	switch r.URL.Path {
	case "/shrt.test.v1.AuthService/Login":
		if f.refuseLogin {
			writeJSON(w, 200, map[string]any{
				"error": map[string]any{"code": "unauthenticated", "message": "username or password is incorrect"},
			})
			return
		}
		f.logins++
		token := fmt.Sprintf("token-%d", f.logins)
		f.tokens[token] = true
		if user, ok := body["username"].(string); ok {
			if f.issued == nil {
				f.issued = map[string]string{}
			}
			f.issued[user] = token
		}
		writeJSON(w, 200, map[string]any{
			"error":       okError(),
			"accessToken": token,
			"expiresAt":   "0",
		})
		return
	case "/shrt.test.v1.PartnerAuthService/Login":
		f.partnerLogins++
		token := fmt.Sprintf("partner-token-%d", f.partnerLogins)
		f.partnerTokens[token] = true
		if user, ok := body["username"].(string); ok {
			if f.partnerIssued == nil {
				f.partnerIssued = map[string]string{}
			}
			f.partnerIssued[user] = token
		}
		writeJSON(w, 200, map[string]any{
			"error":       okError(),
			"accessToken": token,
			"expiresAt":   "0",
		})
		return
	case "/shrt.test.v1.PartnerService/FetchMine":
		if !f.partnerAuthorized(r) {
			writeJSON(w, 401, map[string]any{"code": "unauthenticated", "message": "a staff token has the wrong scope"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"error":     okError(),
			"id":        body["id"],
			"name":      "mine",
			"createdAt": "stamp-partner",
		})
		return
	}

	if !f.authorized(r) {
		if f.inBand {
			path := f.inBandPath
			if path == "" {
				path = "error.code"
			}
			head, _, _ := strings.Cut(path, ".")
			writeJSON(w, 200, map[string]any{
				head: map[string]any{"code": "unauthenticated", "message": "token rejected"},
			})
			return
		}
		writeJSON(w, 401, map[string]any{"code": "unauthenticated", "message": "token rejected"})
		return
	}

	switch r.URL.Path {
	case "/shrt.test.v1.BatchService/Preview":
		results := []map[string]any{}
		for i, line := range lines(body) {
			if strings.HasPrefix(line, "bad") {
				results = append(results, map[string]any{
					"error":  map[string]any{"code": "invalid_argument", "message": line + " was refused"},
					"amount": "",
				})
				continue
			}
			if f.batchUnset {
				results = append(results, map[string]any{"error": nil, "amount": fmt.Sprintf("%d", i+1)})
				continue
			}
			results = append(results, map[string]any{"error": okError(), "amount": "1"})
		}
		if f.itemKey != "" {
			for _, row := range results {
				row[f.itemKey] = map[string]any{"code": "invalid_argument", "message": "refused"}
				delete(row, "error")
			}
		}
		out := map[string]any{"error": okError(), "results": results}
		if f.batchDrift {
			out["no_such_field_in_the_proto"] = "x"
		}
		writeJSON(w, 200, out)
	case "/shrt.test.v1.BatchService/Receipt":
		results := []map[string]any{}
		for i, line := range lines(body) {
			row := map[string]any{"id": fmt.Sprintf("receipt-%d", i+1)}
			if f.receiptDrift {
				if strings.HasPrefix(line, "bad") {
					row["error"] = map[string]any{"code": "invalid_argument", "message": line + " was refused"}
				} else {
					row["error"] = okError()
				}
			}
			results = append(results, row)
		}
		writeJSON(w, 200, map[string]any{"error": okError(), "results": results})
	case "/shrt.test.v1.ThingService/Create":
		f.creates++
		f.nextID++
		if f.echoHeader != "" {
			writeJSON(w, 403, map[string]any{"code": "permission_denied", "message": "api key " + r.Header.Get(f.echoHeader) + " is not allowed"})
			return
		}
		if f.createRefusal != "" {
			writeJSON(w, 200, map[string]any{"error": map[string]any{"code": f.createRefusal, "message": "refused"}})
			return
		}
		writeJSON(w, 200, map[string]any{
			"error": okError(),
			"id":    fmt.Sprintf("thing-%d", f.nextID),
		})
	case "/shrt.test.v1.ThingService/Fetch":
		name := "widget"
		if f.drift != "" {
			name = f.drift
		}
		out := map[string]any{
			"error":     okError(),
			"id":        body["id"],
			"name":      name,
			"createdAt": fmt.Sprintf("stamp-%d", f.creates),
		}
		if f.unknownField {
			out["no_such_field_in_the_proto"] = "x"
		}
		if f.tokenKey {
			out[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] = 1
		}
		writeJSON(w, 200, out)
	default:
		writeJSON(w, 404, map[string]any{"code": "unimplemented", "message": r.URL.Path})
	}
}

func (f *fakeServer) authorized(r *http.Request) bool {
	raw := r.Header.Get("Authorization")
	if !strings.HasPrefix(raw, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(raw, "Bearer ")
	if f.rejectAll {
		delete(f.tokens, token)
		f.rejectAll = false
		return false
	}
	return f.tokens[token]
}

func (f *fakeServer) partnerAuthorized(r *http.Request) bool {
	return f.partnerTokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
}

func (f *fakeServer) partnerLoginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.partnerLogins
}

func (f *fakeServer) expireTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectAll = true
}

func (f *fakeServer) setDrift(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drift = name
}

func (f *fakeServer) loginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins
}

func (f *fakeServer) tokenIssuedTo(t *testing.T, username string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := f.issued[username]
	if !ok {
		t.Fatalf("no login was issued for %q — the premise of the assertion is missing, not the assertion", username)
	}
	return token
}

func (f *fakeServer) partnerTokenIssuedTo(t *testing.T, username string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	token, ok := f.partnerIssued[username]
	if !ok {
		t.Fatalf("no partner login was issued for %q — the premise of the assertion is missing, not the assertion", username)
	}
	return token
}

func (f *fakeServer) headerFor(procedure, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.calls {
		if p == procedure && i < len(f.headers) {
			return f.headers[i].Get(name)
		}
	}
	return ""
}

func okError() map[string]any {
	return map[string]any{"code": "OK", "message": ""}
}

func lines(body map[string]any) []string {
	raw, _ := body["lines"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeServer) forgetTokensInBand() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inBand = true
	f.tokens = map[string]bool{}
}

func (f *fakeServer) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeServer) creates0() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

func (f *fakeServer) forgetTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = map[string]bool{}
}

func testConfig(baseURL string) *config.Config {
	cfg := config.Default()
	cfg.Target.BaseURL = baseURL
	cfg.Auth = &config.Auth{
		Call:        "AuthService/Login",
		Body:        map[string]any{"username": "staff", "password": "secret"},
		TokenPath:   "access_token",
		ExpiresPath: "expires_at",
	}
	return cfg
}

func testChain() *chain.Chain {
	c := &chain.Chain{
		Name:     "thing-flow",
		Volatile: []string{"**.created_at", "**.id"},
		Steps: []*chain.Step{
			{
				ID:   "create",
				Call: "ThingService/Create",
				Body: map[string]any{"name": "widget", "kind": "KIND_A", "idempotency_key": "${uuid}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "id", NotEmpty: true},
				},
				Export: map[string]string{"thing_id": "id"},
			},
			{
				ID:   "fetch",
				Call: "ThingService/Fetch",
				Body: map[string]any{"id": "${create.id}"},
				Expect: []chain.Expectation{
					{Path: "error.code", Equals: "OK"},
					{Path: "name", Equals: "widget"},
				},
			},
		},
	}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func buildRunner(t testing.TB, cfg *config.Config, cat *catalog.Catalog) *runner.Runner {
	t.Helper()
	deps, err := runner.Build(context.Background(), cfg, cat, nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func newRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	return buildRunner(t, testConfig(srv.URL), catalogtest.New())
}

func profileRunner(t *testing.T, srv *fakeServer, cfg *config.Config) *runner.Runner {
	t.Helper()
	return buildRunner(t, cfg, catalogtest.New())
}

func newBatchRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	return buildRunner(t, testConfig(srv.URL), catalogtest.Batch())
}

func bareRunner(t *testing.T, h http.HandlerFunc, mutate func(*config.Config)) *runner.Runner {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := testConfig(srv.URL)
	cfg.Auth = nil
	if mutate != nil {
		mutate(cfg)
	}
	r := buildRunner(t, cfg, catalogtest.New())
	r.ValidateOutput = false
	return r
}

func answering(status int, contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func run(t *testing.T, r *runner.Runner, c *chain.Chain, opts runner.Options) *runner.Record {
	t.Helper()
	rec, err := r.Run(context.Background(), c, opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return rec
}

func normalized(t *testing.T, c *chain.Chain) *chain.Chain {
	t.Helper()
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return c
}

func okExpect() []chain.Expectation {
	return []chain.Expectation{{Path: "error.code", Equals: "OK"}}
}

func step(id, call string, body map[string]any, expect ...chain.Expectation) *chain.Step {
	return &chain.Step{ID: id, Call: call, Body: body, Expect: expect}
}

func thing(name string) map[string]any { return map[string]any{"name": name, "kind": "KIND_A"} }

func byID(id string) map[string]any { return map[string]any{"id": id} }

func flow(steps ...*chain.Step) *chain.Chain { return &chain.Chain{Name: "flow", Steps: steps} }

func stepByID(t *testing.T, rec *runner.Record, id string) *runner.StepRecord {
	t.Helper()
	s, ok := rec.Step(id)
	if !ok {
		t.Fatalf("no step %q in the record", id)
	}
	return s
}

func expectResult(t *testing.T, rec *runner.Record, path string) chain.ExpectResult {
	t.Helper()
	for _, sr := range rec.Steps {
		for _, e := range sr.Expect {
			if e.Path == path {
				return e
			}
		}
	}
	t.Fatalf("no expectation recorded for %q", path)
	return chain.ExpectResult{}
}

func recordText(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func yes() *bool            { v := true; return &v }
func no() *bool             { v := false; return &v }
func strp(s string) *string { return &s }

func partnerConfig(baseURL string) *config.Config {
	cfg := testConfig(baseURL)
	cfg.Auth.Profiles = map[string]*config.Auth{
		"partner": {Call: "PartnerAuthService/Login", Body: map[string]any{"username": "partner@example.com", "password": "portal"}},
	}
	return cfg
}

func shopRunner(t *testing.T, h http.HandlerFunc, validate bool) *runner.Runner {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	return &runner.Runner{Catalog: catalogtest.Shop(), Client: transport.New(transport.Options{BaseURL: srv.URL}), ValidateInput: true, ValidateOutput: validate}
}

func stockRunner(t *testing.T, body string, validate bool) *runner.Runner {
	return shopRunner(t, answering(200, "application/json", body), validate)
}

const stock5 = `{"status":{"code":"SUCCESS"},"qtyOnHand":"5"}`

func addStockChain(expect ...chain.Expectation) *chain.Chain {
	return &chain.Chain{Name: "zero", Steps: []*chain.Step{{
		ID: "stock", Call: "shop.catalog.v1.StockService/AddStock",
		Body:   map[string]any{"id_product": "p-1", "qty": "0"},
		Expect: expect,
	}}}
}

func batchChain(call string, lines []any, expect ...chain.Expectation) *chain.Chain {
	c := &chain.Chain{Name: "batch-flow", Steps: []*chain.Step{{ID: "batch", Call: call, Body: map[string]any{"lines": lines}, Expect: expect}}}
	if err := c.Normalize(); err != nil {
		panic(err)
	}
	return c
}

func itemEnvelopeResult(t *testing.T, rec *runner.Record) (chain.ExpectResult, bool) {
	t.Helper()
	for _, e := range rec.Steps[0].Expect {
		if e.Rule == "item_envelope" {
			return e, true
		}
	}
	return chain.ExpectResult{}, false
}

type bufferWriter struct {
	h      http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufferWriter) Header() http.Header         { return b.h }
func (b *bufferWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufferWriter) WriteHeader(s int)           { b.status = s }

func wrap(f *fakeServer, expiring bool, h func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc)) *httptest.Server {
	next := func(w http.ResponseWriter, r *http.Request) {
		if !expiring || !strings.HasSuffix(r.URL.Path, "/Login") {
			f.handle(w, r)
			return
		}
		rec := &bufferWriter{h: http.Header{}, status: 200}
		f.handle(rec, r)
		body := map[string]any{}
		_ = json.Unmarshal(rec.buf.Bytes(), &body)
		body["expiresAt"] = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
		writeJSON(w, rec.status, body)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r, next) }))
	return srv
}

func unauth(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{"code": "unauthenticated", "message": message})
}

type shopServer struct {
	*httptest.Server
	mu      sync.Mutex
	answers map[string]any
	calls   []string
}

func newShopServer(t *testing.T, answers map[string]any) *shopServer {
	t.Helper()
	s := &shopServer{answers: answers}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.calls = append(s.calls, r.URL.Path)
		s.mu.Unlock()
		rpc := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		answer, ok := s.answers[rpc]
		if !ok {
			answer = map[string]any{"status": map[string]any{"code": "SUCCESS"}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(s.Close)
	chain.SetEnvelope("status.code", "SUCCESS")
	t.Cleanup(func() { chain.SetEnvelope("", "") })
	return s
}

func (s *shopServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *shopServer) runner() *runner.Runner {
	return &runner.Runner{
		Catalog:        catalogtest.Shop(),
		Client:         transport.New(transport.Options{BaseURL: s.URL}),
		ValidateInput:  true,
		ValidateOutput: true,
	}
}
