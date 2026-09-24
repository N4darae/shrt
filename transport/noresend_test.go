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

func TestADroppedConnectionAfterAnIdempotencyKeyedWriteIsNeverReSent(t *testing.T) {
	for _, header := range []string{"Idempotency-Key", "X-Idempotency-Key", ""} {
		t.Run(header, func(t *testing.T) {
			var writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)
				if !strings.HasSuffix(r.URL.Path, "/AddStock") {
					_, _ = w.Write([]byte(`{}`))
					return
				}
				writes.Add(1)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL})
			if _, err := c.Do(context.Background(), &Call{Procedure: "/svc.v1.S/GetProduct", Body: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
			h := http.Header{}
			if header != "" {
				h.Set(header, "k-1")
			}
			_, err := c.Do(context.Background(), &Call{Procedure: "/svc.v1.S/AddStock", Body: []byte(`{"qty":5}`), Header: h})
			if err == nil {
				t.Fatal("a connection dropped after the write was sent must be an error")
			}
			if n := writes.Load(); n != 1 {
				t.Fatalf("the write reached the server %d times; shrt must never re-send it", n)
			}
			for _, want := range []string{"closed the connection", "whether the call took effect is unknown"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the error must say the call's effect is unknown (%q missing): %v", want, err)
				}
			}
		})
	}
}
