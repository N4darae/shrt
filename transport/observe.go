package transport

import (
	"context"
	"time"
)

type Event struct {
	Procedure string
	Attempt   int
	Status    int
	Latency   time.Duration
	Err       error
}

func WithObserver(fn func(Event)) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, call *Call) (*Result, error) {
			res, err := next(ctx, call)
			ev := Event{Procedure: call.Procedure, Attempt: 1, Err: err}
			if res != nil {
				ev.Status = res.Status
				ev.Latency = res.Latency
			}
			fn(ev)
			return res, err
		}
	}
}

func WithRetry(attempts int, backoff time.Duration, retryable func(*Result, error) bool) Middleware {
	if attempts < 1 {
		attempts = 1
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, call *Call) (*Result, error) {
			var res *Result
			var err error
			for i := range attempts {
				res, err = next(ctx, call)
				if !retryable(res, err) {
					return res, err
				}
				if i == attempts-1 {
					break
				}
				select {
				case <-ctx.Done():
					return res, ctx.Err()
				case <-time.After(backoff * time.Duration(i+1)):
				}
			}
			return res, err
		}
	}
}
