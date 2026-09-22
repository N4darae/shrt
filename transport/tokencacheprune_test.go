package transport_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/N4darae/shrt/transport"
)

func cacheEntries(t *testing.T, path string) map[string]struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	out := map[string]struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	return out
}

func TestTokenCacheDropsExpiredEntriesWhenItIsRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	stale := map[string]any{
		"checker#deadbeefdeadbeef":  map[string]any{"token": "old-1", "expires_at": "2026-01-01T00:00:00Z"},
		"checker2#feedfacefeedface": map[string]any{"token": "old-2", "expires_at": "2026-02-01T00:00:00Z"},
		"keeper#0123456789abcdef":   map[string]any{"token": "live", "expires_at": "2099-01-01T00:00:00Z"},
	}
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	logins := 0
	src := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&logins, "tok-new", time.Now().Add(time.Hour)))
	src.UseCache(path, "default")
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("token: %v", err)
	}

	entries := cacheEntries(t, path)
	if _, ok := entries["checker#deadbeefdeadbeef"]; ok {
		t.Error("an expired entry survived a rewrite. Every throwaway identity a chain mints leaves one " +
			"behind, so the file grows without bound — 500 entries, 459 of them expired, on the box " +
			"this was measured on")
	}
	if _, ok := entries["checker2#feedfacefeedface"]; ok {
		t.Error("the second expired entry survived too")
	}
	if _, ok := entries["keeper#0123456789abcdef"]; !ok {
		t.Error("a token that has not expired must stay: reuse across processes is the point of the cache")
	}
	if len(entries) != 2 {
		t.Errorf("want the live entry plus the one just written, got %d: %v", len(entries), entries)
	}
}

func TestTokenCacheKeepsAnEntryWithNoExpiryRatherThanGuessing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	raw, err := json.Marshal(map[string]any{
		"unknown#0000000000000000": map[string]any{"token": "no-expiry"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	logins := 0
	src := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&logins, "tok-new", time.Now().Add(time.Hour)))
	src.UseCache(path, "default")
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("token: %v", err)
	}

	if _, ok := cacheEntries(t, path)["unknown#0000000000000000"]; !ok {
		t.Error("a zero expiry means the writer never recorded one, not that it lapsed in 1970; " +
			"deleting it would throw away a token that may still work")
	}
}
