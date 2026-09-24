package transport_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/N4darae/shrt/transport"
)

func TestUnreachableIsADialFailureNotAConnectError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c := transport.New(transport.Options{BaseURL: "http://" + addr})
	_, err = c.Do(context.Background(), &transport.Call{Procedure: "x.v1.S/M"})
	if err == nil {
		t.Fatal("want a dial error against a closed port")
	}
	if !transport.Unreachable(err) {
		t.Fatalf("connection refused must count as unreachable, got %v", err)
	}
	if transport.Unreachable(errors.New("auth login rejected")) || transport.Unreachable(&transport.Error{Code: "unavailable"}) {
		t.Fatal("a Connect error or other failure is not a dead target")
	}
}
