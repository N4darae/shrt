package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSend_NeverFollowsARedirect(t *testing.T) {
	var elsewhere atomic.Int32
	var seenAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		seenAuth.Store(r.Header.Get("Authorization"))
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"foreign-token"}`))
	}))
	defer other.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", other.URL+r.URL.Path)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer target.Close()

	c := New(Options{BaseURL: target.URL})
	call := &Call{Procedure: "svc.v1.S/Get", Body: []byte(`{"password":"pw"}`), Header: http.Header{"Authorization": {"Bearer tok"}}}
	res, err := c.Do(context.Background(), call)
	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("the redirect was followed %d time(s) and the other host saw Authorization %q", n, seenAuth.Load())
	}
	if err == nil {
		t.Fatalf("a 307 must be a transport error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), other.URL) || !strings.Contains(err.Error(), "307") {
		t.Fatalf("the error must name the status and the Location: %v", err)
	}
}

func TestLogin_RedirectIsNotResent(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"foreign-token"}`))
	}))
	defer other.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", other.URL+r.URL.Path)
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer target.Close()

	c := New(Options{BaseURL: target.URL})
	src := NewLoginTokenSource(AuthSpec{
		Procedure: "svc.v1.Auth/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"u","password":"pw"}`), nil },
		TokenPath: "access_token",
	}, c.Raw)
	tok, err := src.Token(context.Background())
	if elsewhere.Load() != 0 {
		t.Fatalf("the login body was re-posted to the redirect target, token %q", tok)
	}
	if err == nil || !strings.Contains(err.Error(), other.URL) {
		t.Fatalf("login must fail naming the Location, got token %q err %v", tok, err)
	}
}
