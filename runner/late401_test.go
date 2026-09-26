package runner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

type discardWriter struct{ h http.Header }

func (d *discardWriter) Header() http.Header         { return d.h }
func (d *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardWriter) WriteHeader(int)             {}

func lateUnauthServer(f *fakeServer, rpc string) *httptest.Server {
	var mu sync.Mutex
	done := false
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		first := !done && strings.HasSuffix(r.URL.Path, "/"+rpc)
		if first {
			done = true
		}
		mu.Unlock()
		if !first {
			f.handle(w, r)
			return
		}
		f.handle(&discardWriter{h: http.Header{}}, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"token expired"}`))
	}))
}

func lateRunner(t *testing.T, url string) *runner.Runner {
	t.Helper()
	deps, err := runner.Build(context.Background(), testConfig(url), catalogtest.New(), nil)
	if err != nil {
		t.Fatalf("build deps: %v", err)
	}
	return &runner.Runner{Catalog: deps.Catalog, Client: deps.Client, ValidateInput: true, ValidateOutput: true, Auth: deps.Bindings}
}

func TestAWriteAnswered401AfterItRanIsNotSentTwice(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := lateUnauthServer(f, "Create")
	defer srv.Close()

	rec, err := lateRunner(t, srv.URL).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 {
		t.Fatalf("the backend performed Create and then answered 401; an automatic re-login and re-send "+
			"performs the write twice (creates=%d)", f.creates)
	}
	st := rec.Steps[0]
	if st.AuthRetry != runner.AuthRetryNotResent || !strings.Contains(st.Warning, "not re-sent") {
		t.Fatalf("the step record must say the 401 was not retried and why: auth_retry=%q warning=%q", st.AuthRetry, st.Warning)
	}
	if rec.Passed() {
		t.Fatal("the write was answered 401, so the step cannot pass")
	}

	again, err := lateRunner(t, srv.URL).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Passed() {
		t.Fatalf("the rejected token was dropped, so the next run logs in fresh and passes: %s", again.Failure)
	}
}

func TestARead401IsResentAndTheStepSaysSo(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	srv := lateUnauthServer(f, "Fetch")
	defer srv.Close()

	rec, err := lateRunner(t, srv.URL).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Passed() {
		t.Fatalf("a read answered 401 is re-sent after a fresh login: %s", rec.Failure)
	}
	st := rec.Steps[1]
	if st.AuthRetry != runner.AuthRetryResent || !strings.Contains(st.Warning, "re-sent") {
		t.Fatalf("the step record must say the call was sent twice: auth_retry=%q warning=%q", st.AuthRetry, st.Warning)
	}
}
