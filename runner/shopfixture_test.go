package runner_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/transport"
)

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
