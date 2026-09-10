package transport_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/N4darae/shrt/transport"
)

func cacheSpec() transport.AuthSpec {
	return transport.AuthSpec{
		Procedure:   "/shrt.test.v1.AuthService/Login",
		Body:        func() ([]byte, error) { return []byte(`{"username":"staff"}`), nil },
		TokenPath:   "access_token",
		ExpiresPath: "expires_at",
	}
}

func countingInvoke(n *int, token string, expiresAt time.Time) transport.Handler {
	return func(ctx context.Context, call *transport.Call) (*transport.Result, error) {
		*n++
		body := fmt.Sprintf(`{"access_token":%q,"expires_at":%d}`, token, expiresAt.Unix())
		return &transport.Result{Status: 200, Body: []byte(body)}, nil
	}
}

func TestTokenCache_ASecondProcessReusesTheTokenInsteadOfLoggingInAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	expiry := time.Now().Add(30 * time.Minute)

	firstLogins := 0
	first := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&firstLogins, "tok-1", expiry))
	first.UseCache(path, "default")
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatalf("first token: %v", err)
	}
	if firstLogins != 1 {
		t.Fatalf("first process logged in %d times, want 1", firstLogins)
	}

	secondLogins := 0
	second := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&secondLogins, "tok-2", expiry))
	second.UseCache(path, "default")
	tok, err := second.Token(context.Background())
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if secondLogins != 0 {
		t.Fatalf("the second process logged in %d times — the point of the cache is that 92 chain "+
			"runs cost ONE login, because the login limiter is global (rate.Every(6s), burst 10) "+
			"and every extra login is 6s of forced sleep in a sweep", secondLogins)
	}
	if tok != "tok-1" {
		t.Errorf("token = %q, want the cached tok-1", tok)
	}
}

func TestTokenCache_AnExpiredEntryIsNotReused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	stale := 0
	first := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&stale, "old", time.Now().Add(10*time.Second)))
	first.UseCache(path, "default")
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	fresh := 0
	second := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&fresh, "new", time.Now().Add(time.Hour)))
	second.UseCache(path, "default")
	tok, err := second.Token(context.Background())
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if fresh != 1 {
		t.Fatalf("a token inside the leeway window must be re-fetched, logins = %d", fresh)
	}
	if tok != "new" {
		t.Errorf("token = %q, want the freshly minted one", tok)
	}
}

func TestTokenCache_ProfilesDoNotShareAnEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	expiry := time.Now().Add(time.Hour)

	staffLogins := 0
	staff := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&staffLogins, "staff-tok", expiry))
	staff.UseCache(path, "default")
	if _, err := staff.Token(context.Background()); err != nil {
		t.Fatal(err)
	}

	checkerLogins := 0
	checker := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&checkerLogins, "checker-tok", expiry))
	checker.UseCache(path, "checker")
	tok, err := checker.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if checkerLogins != 1 {
		t.Fatalf("the checker profile must not read the default profile's entry, logins = %d — a "+
			"sweep mints fresh checker credentials per chain, so a shared entry would hand one "+
			"chain another chain's identity", checkerLogins)
	}
	if tok != "checker-tok" {
		t.Errorf("token = %q, want checker-tok", tok)
	}
}

func TestTokenCache_AnEntryWithNoExpiryIsNeverReused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	noExpiry := transport.AuthSpec{
		Procedure: "/shrt.test.v1.AuthService/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"staff"}`), nil },
		TokenPath: "access_token",
	}

	firstLogins := 0
	first := transport.NewLoginTokenSource(noExpiry, countingInvoke(&firstLogins, "no-exp", time.Time{}))
	first.UseCache(path, "default")
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatal(err)
	}

	secondLogins := 0
	second := transport.NewLoginTokenSource(noExpiry, countingInvoke(&secondLogins, "fresh", time.Time{}))
	second.UseCache(path, "default")
	tok, err := second.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if secondLogins != 1 {
		t.Fatalf("an entry with no expiry must NOT be reused across processes, logins = %d. "+
			"In-process a missing expiry is harmless because the process is short-lived; on disk "+
			"it means 'valid forever', and a token that is in fact expired then fails every step "+
			"of a chain with an auth error that looks like a backend fault", secondLogins)
	}
	if tok != "fresh" {
		t.Errorf("token = %q, want the freshly minted one", tok)
	}
}

func TestTokenCache_AnUnreadableCacheIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	logins := 0
	src := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&logins, "tok", time.Now().Add(time.Hour)))
	src.UseCache(path, "default")
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("a corrupt cache must degrade to a real login, got %v", err)
	}
	if logins != 1 {
		t.Errorf("logins = %d, want 1", logins)
	}
}
