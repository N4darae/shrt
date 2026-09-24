package transport_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/transport"
)

func TestARefusedLoginSaysWhatTheBackendAnswered(t *testing.T) {
	refuse := func(ctx context.Context, call *transport.Call) (*transport.Result, error) {
		body := `{"status":{"code":"REJECTED","details":[{"app_code":1001,"reason":"BadCredentials"}]}}`
		return &transport.Result{Status: 200, Body: []byte(body)}, nil
	}
	_, err := transport.NewLoginTokenSource(cacheSpec(), refuse).Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "BadCredentials") {
		t.Fatalf("a login the backend refused in-band must show the refusal, got %v", err)
	}
}
