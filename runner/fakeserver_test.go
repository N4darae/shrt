package runner_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
	batchUnset    bool
	refuseLogin   bool
	inBand        bool
	inBandPath    string
	batchDrift    bool
	receiptDrift  bool
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

func (f *fakeServer) lastHeader(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.headers) == 0 {
		return ""
	}
	return f.headers[len(f.headers)-1].Get(name)
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
