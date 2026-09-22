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

func TestInvalidateDropsTheCachedTokenAndNotTheOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	expiry := time.Now().Add(30 * time.Minute)

	other := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(new(int), "tok-other", expiry))
	other.UseCache(path, "partner")
	if _, err := other.Token(context.Background()); err != nil {
		t.Fatalf("seed the other profile: %v", err)
	}

	logins := 0
	src := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&logins, "tok-1", expiry))
	src.UseCache(path, "default")
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("first token: %v", err)
	}
	if logins != 1 {
		t.Fatalf("setup: logins = %d, want 1", logins)
	}

	src.Invalidate()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	entries := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("after invalidating one profile the cache should hold only the other profile's "+
			"entry, got %d", len(entries))
	}

	fresh := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(new(int), "tok-2", expiry))
	fresh.UseCache(path, "default")
	tok, err := fresh.Token(context.Background())
	if err != nil {
		t.Fatalf("token after invalidate: %v", err)
	}
	if tok == "tok-1" {
		t.Fatal("Invalidate cleared the in-memory token and left the same token on disk, so the " +
			"very next Token() read it back and the retry re-sent the credential the server had " +
			"just rejected. A token the backend has forgotten is dead for every process, not just " +
			"this one")
	}
}

func TestInvalidateWithoutACacheIsHarmless(t *testing.T) {
	logins := 0
	src := transport.NewLoginTokenSource(cacheSpec(), countingInvoke(&logins, "tok-1", time.Now().Add(time.Hour)))
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	src.Invalidate()
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins != 2 {
		t.Fatalf("logins = %d, want 2", logins)
	}
}
