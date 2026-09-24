package transport

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

func Unreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func ConnectionClosed(err error) bool {
	return err != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNABORTED))
}

func closedError(err error) error {
	return fmt.Errorf("the backend closed the connection before a response arrived (%w): it most likely stopped or "+
		"crashed while this request was in flight, so whether the call took effect is unknown. This is not a verdict "+
		"about the rpc: check the backend is up and run again", err)
}
