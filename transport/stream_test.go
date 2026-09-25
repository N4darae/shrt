package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func streamServer(t *testing.T, write func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if got := r.Header.Get("Content-Type"); got != ContentTypeStreamJSON {
			t.Errorf("content type %q, want %q", got, ContentTypeStreamJSON)
		}
		if len(raw) < 5 || string(raw[5:]) != `{"id":"x"}` {
			t.Errorf("the request must be one enveloped frame, got %q", raw)
		}
		w.Header().Set("Content-Type", ContentTypeStreamJSON)
		write(w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAServerStreamRecordsItsFirstMessage(t *testing.T) {
	srv := streamServer(t, func(w http.ResponseWriter) {
		_, _ = w.Write(frame(0, []byte(`{"n":1}`)))
		_, _ = w.Write(frame(0, []byte(`{"n":2}`)))
		_, _ = w.Write(frame(2, []byte(`{}`)))
	})
	res, err := New(Options{BaseURL: srv.URL}).Do(context.Background(), &Call{Procedure: "/s.v1.S/Watch", Body: []byte(`{"id":"x"}`), Stream: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != nil || string(res.Body) != `{"messages":[{"n":1}]}` {
		t.Fatalf("want the first message only, got %s (%v)", res.Body, res.Error)
	}
}

func TestAServerStreamEndStreamErrorIsAConnectError(t *testing.T) {
	srv := streamServer(t, func(w http.ResponseWriter) {
		_, _ = w.Write(frame(2, []byte(`{"error":{"code":"unauthenticated","message":"no token"}}`)))
	})
	res, err := New(Options{BaseURL: srv.URL}).Do(context.Background(), &Call{Procedure: "/s.v1.S/Watch", Body: []byte(`{"id":"x"}`), Stream: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == nil || res.Error.Code != "unauthenticated" || res.Status != http.StatusUnauthorized {
		t.Fatalf("an end-stream error must read as the unary one: %d %+v", res.Status, res.Error)
	}
	var e Error
	if json.Unmarshal(res.Body, &e) != nil || e.Code != "unauthenticated" {
		t.Fatalf("the body carries the error, got %s", res.Body)
	}
}

func TestAServerStreamThatStaysOpenStopsAfterTheWait(t *testing.T) {
	defer func(was time.Duration) { StreamWait = was }(StreamWait)
	StreamWait = 50 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	srv := streamServer(t, func(w http.ResponseWriter) {
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	})
	res, err := New(Options{BaseURL: srv.URL}).Do(context.Background(), &Call{Procedure: "/s.v1.S/Watch", Body: []byte(`{"id":"x"}`), Stream: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != nil || string(res.Body) != `{"messages":[]}` {
		t.Fatalf("a stream still open after the wait ends with what it sent, got %s (%v)", res.Body, res.Error)
	}
}
