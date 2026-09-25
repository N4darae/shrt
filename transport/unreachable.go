package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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

const SentNoAnswer = "sent, no answer"

const NoAnswerBeforeTimeout = SentNoAnswer + " before target.timeout"

const ClosedAfterSending = SentNoAnswer + ": the backend closed the connection"

func TimedOut(err error) bool {
	var ne net.Error
	return err != nil && (errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()))
}

func ConnectionClosed(err error) bool {
	return err != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNABORTED))
}

const closedCrashGuess = ": it most likely stopped or crashed while this request was in flight, so whether the call took effect is unknown"

const closedNotAVerdict = " This is not a verdict about the rpc: check the backend is up and run again"

func StillUp(message string) string {
	message = strings.Replace(message, closedCrashGuess, ": whether the call took effect is unknown", 1)
	if !strings.Contains(message, closedNotAVerdict) {
		return message
	}
	return strings.TrimSuffix(strings.Replace(message, closedNotAVerdict, "", 1), ".")
}

func closedError(err error, sent bool) error {
	if !sent {
		return fmt.Errorf("the backend closed the connection before the request was written (%w), so it was not sent "+
			"and took no effect. This is not a verdict about the rpc: check the backend is up and run again", err)
	}
	return fmt.Errorf("%s before a response arrived (%w)"+closedCrashGuess+"."+closedNotAVerdict, ClosedAfterSending, err)
}
