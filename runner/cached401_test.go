package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

type bufferWriter struct {
	h      http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufferWriter) Header() http.Header         { return b.h }
func (b *bufferWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }
func (b *bufferWriter) WriteHeader(s int)           { b.status = s }

func expiringLoginServer(f *fakeServer) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Login") {
			f.handle(w, r)
			return
		}
		rec := &bufferWriter{h: http.Header{}, status: 200}
		f.handle(rec, r)
		body := map[string]any{}
		_ = json.Unmarshal(rec.buf.Bytes(), &body)
		body["expiresAt"] = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func cachedRunner(t *testing.T, url, root string) *runner.Runner {
	t.Helper()
	cfg := testConfig(url)
	cfg.Root = root
	deps, err := runner.Build(context.Background(), cfg, catalogtest.New(), nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func TestACachedTokenTheRestartedBackendRefusesIsReplacedAndTheWriteResent(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := expiringLoginServer(f)
	defer srv.Close()
	root := t.TempDir()

	first, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Passed() {
		t.Fatalf("setup: the first run must pass: %s", first.Failure)
	}
	f.mu.Lock()
	f.tokens = map[string]bool{}
	creates := f.creates
	f.mu.Unlock()

	rec, err := cachedRunner(t, srv.URL, root).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Passed() {
		t.Fatalf("the backend restarted and refused the cached token at authentication, so a fresh login and a re-send "+
			"is safe and the run must pass: %s", rec.Failure)
	}
	if f.creates != creates+1 {
		t.Fatalf("the refused write was not performed, so it runs exactly once more (creates %d -> %d)", creates, f.creates)
	}
	st := rec.Steps[0]
	if st.AuthRetry != runner.AuthRetryResent || !strings.Contains(st.Warning, "cache") {
		t.Fatalf("the step must say it was re-sent after its cached token was refused: auth_retry=%q warning=%q", st.AuthRetry, st.Warning)
	}
}

func TestAWriteRefusedAtAuthenticationIsAnErrorNotAFailure(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := lateUnauthServer(f, "Create")
	defer srv.Close()

	rec, err := lateRunner(t, srv.URL).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	if st.Status != runner.StatusError {
		t.Fatalf("a 401 is the backend refusing authentication, not a verdict about the rpc: status=%s, want error", st.Status)
	}
	if rec.Status != runner.StatusError {
		t.Fatalf("the run carries the step's error status: %s", rec.Status)
	}
}
