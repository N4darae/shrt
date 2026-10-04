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

func mint(spec transport.AuthSpec, path, profile, token string, expiresAt time.Time) (string, int, error) {
	logins := 0
	src := transport.NewLoginTokenSource(spec, countingInvoke(&logins, token, expiresAt))
	src.UseCache(path, profile)
	tok, err := src.Token(context.Background())
	return tok, logins, err
}

func TestTokenCache_ASecondProcessReusesTheTokenInsteadOfLoggingInAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	expiry := time.Now().Add(30 * time.Minute)

	_, firstLogins, err := mint(cacheSpec(), path, "default", "tok-1", expiry)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	if firstLogins != 1 {
		t.Fatalf("first process logged in %d times, want 1", firstLogins)
	}

	tok, secondLogins, err := mint(cacheSpec(), path, "default", "tok-2", expiry)
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
	path := filepath.Join(t.TempDir(), "token.json")

	if _, _, err := mint(cacheSpec(), path, "default", "old", time.Now().Add(10*time.Second)); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	tok, fresh, err := mint(cacheSpec(), path, "default", "new", time.Now().Add(time.Hour))
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
	path := filepath.Join(t.TempDir(), "token.json")
	expiry := time.Now().Add(time.Hour)

	if _, _, err := mint(cacheSpec(), path, "default", "staff-tok", expiry); err != nil {
		t.Fatal(err)
	}

	tok, checkerLogins, err := mint(cacheSpec(), path, "checker", "checker-tok", expiry)
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
	path := filepath.Join(t.TempDir(), "token.json")

	noExpiry := cacheSpec()
	noExpiry.ExpiresPath = ""

	if _, _, err := mint(noExpiry, path, "default", "no-exp", time.Time{}); err != nil {
		t.Fatal(err)
	}

	tok, secondLogins, err := mint(noExpiry, path, "default", "fresh", time.Time{})
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
	path := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, logins, err := mint(cacheSpec(), path, "default", "tok", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("a corrupt cache must degrade to a real login, got %v", err)
	}
	if logins != 1 {
		t.Errorf("logins = %d, want 1", logins)
	}
}
