package runner_test

import (
	"context"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestInBandUnauthenticatedInvalidatesTheToken(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	r := newRunner(t, srv)
	if _, err := r.Run(context.Background(), testChain(), runner.Options{}); err != nil {
		t.Fatalf("warm-up run: %v", err)
	}
	if srv.loginCount() != 1 {
		t.Fatalf("warm-up should have logged in once, got %d", srv.loginCount())
	}

	srv.forgetTokensInBand()

	rec, err := r.Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if srv.loginCount() != 2 {
		t.Fatalf("the backend forgot every token and answered HTTP 200 with an in-band "+
			"unauthenticated envelope, which is the shape conventions.envelope_path exists to "+
			"describe. The cached token should have been dropped and a fresh login sent; logins=%d",
			srv.loginCount())
	}
	if !rec.Passed() {
		t.Fatalf("after re-authenticating the chain should pass: %s", rec.Failure)
	}
}

func TestInBandUnauthenticatedIsReadAtTheConfiguredPath(t *testing.T) {
	if chain.DefaultEnvelopePath != "error.code" {
		t.Skip("default envelope path changed")
	}
	srv := newFakeServer()
	defer srv.Close()
	srv.inBandPath = "status.code"

	r := newRunner(t, srv)
	if _, err := r.Run(context.Background(), testChain(), runner.Options{}); err != nil {
		t.Fatalf("warm-up run: %v", err)
	}
	srv.forgetTokensInBand()

	if _, err := r.Run(context.Background(), testChain(), runner.Options{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if srv.loginCount() != 1 {
		t.Fatalf("this deployment spells its verdict at status.code, which the config does not "+
			"declare, so error.code is absent and nothing should be read as unauthenticated; "+
			"logins=%d", srv.loginCount())
	}
}
