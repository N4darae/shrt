package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAConnectionClosedBeforeAResponseIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	_, err := New(Options{BaseURL: srv.URL}).Do(context.Background(), &Call{Procedure: "/svc.v1.S/ListProducts", Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("a connection closed without a response must be an error")
	}
	for _, want := range []string{"closed the connection before a response", "stopped or crashed", "not a verdict"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error must explain the closed connection (%q missing): %v", want, err)
		}
	}
}
