package transport_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/N4darae/shrt/transport"
)

func TestAnUnreachableTargetIsNamedOnceAndNotBlamedOnTheBackend(t *testing.T) {
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
	msg := err.Error()
	if n := strings.Count(msg, "http://"+addr); n != 1 {
		t.Errorf("the URL must be printed once, it appears %d times: %s", n, msg)
	}
	if !strings.Contains(msg, "could not be reached") || !strings.Contains(msg, "down or not started") {
		t.Errorf("say the target could not be reached because it is down or not started: %s", msg)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("keep the cause: %s", msg)
	}
	if !transport.Unreachable(err) {
		t.Fatalf("the rewording must still count as unreachable: %v", err)
	}
}
