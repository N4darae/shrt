package transport_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/transport"
)

func straddlingInvoke(lifetime time.Duration) transport.Handler {
	return func(ctx context.Context, call *transport.Call) (*transport.Result, error) {
		issued := time.Now()
		expires := issued.Add(lifetime).Unix()
		next := issued.Truncate(time.Second).Add(time.Second + 20*time.Millisecond)
		time.Sleep(time.Until(next))
		body := fmt.Sprintf(`{"access_token":"tok-straddle","expires_at":%d}`, expires)
		return &transport.Result{Status: 200, Body: []byte(body)}, nil
	}
}

func TestALoginAnsweredAcrossASecondBoundaryStatesTheLifetimeTheServerGave(t *testing.T) {
	src := transport.NewLoginTokenSource(cacheSpec(), straddlingInvoke(time.Hour))
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := src.Refusal(tok, time.Now())
	if !ok {
		t.Fatal("the minted token must have timing")
	}
	stated, ok := r.Stated()
	if !ok || stated != time.Hour {
		t.Fatalf("the server said the token lives 3600s; answered after a second boundary it must still read 3600s, got %v: %+v", stated, r)
	}
	if !r.Early() {
		t.Fatalf("a token refused right after issue with an hour left is early: %+v", r)
	}
}

func TestACachedTokenFromALoginAcrossASecondBoundaryKeepsItsStatedLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	first := transport.NewLoginTokenSource(cacheSpec(), straddlingInvoke(time.Hour))
	first.UseCache(path, "default")
	if _, err := first.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := transport.NewLoginTokenSource(cacheSpec(), func(ctx context.Context, call *transport.Call) (*transport.Result, error) {
		t.Fatal("the cached token must be reused")
		return nil, nil
	})
	second.UseCache(path, "default")
	tok, err := second.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, _ := second.Refusal(tok, time.Now())
	if stated, ok := r.Stated(); !ok || stated != time.Hour {
		t.Fatalf("the cache must keep what the stated lifetime is derived from, got %v: %+v", stated, r)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "sent_at") {
		t.Fatalf("the cache must record when the login was sent: %s", raw)
	}
}

func TestStatedLifetimePicksTheRoundFigureTheLoginWindowAllows(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	cases := []struct {
		name         string
		sent, issued time.Time
		expires      time.Time
		want         time.Duration
	}{
		{"within one second", base.Add(100 * time.Millisecond), base.Add(300 * time.Millisecond), base.Add(time.Hour), time.Hour},
		{"server issued before the boundary", base.Add(900 * time.Millisecond), base.Add(1100 * time.Millisecond), base.Add(time.Hour), time.Hour},
		{"server issued after the boundary", base.Add(900 * time.Millisecond), base.Add(1100 * time.Millisecond), base.Add(time.Second + time.Hour), time.Hour},
		{"no send time recorded", time.Time{}, base.Add(1100 * time.Millisecond), base.Add(time.Hour), time.Hour - time.Second},
		{"odd lifetime", base.Add(900 * time.Millisecond), base.Add(1100 * time.Millisecond), base.Add(1234 * time.Second), 1234 * time.Second},
		{"slow login", base.Add(900 * time.Millisecond), base.Add(3100 * time.Millisecond), base.Add(2*time.Second + 90*time.Second), 90 * time.Second},
	}
	for _, c := range cases {
		r := transport.TokenRefusal{SentAt: c.sent, IssuedAt: c.issued, ExpiresAt: c.expires, RefusedAt: c.issued}
		if got, ok := r.Stated(); !ok || got != c.want {
			t.Errorf("%s: stated %v, want %v", c.name, got, c.want)
		}
	}
}

func TestACachedReloginTokenAcceptedYoungerThanTheRefusalsKeepsTheChain(t *testing.T) {
	for _, tc := range []struct {
		name string
		age  time.Duration
		kept bool
	}{
		{"younger than the refusal", 5 * time.Second, true},
		{"older than the refusal", 30 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token.json")
			src := transport.NewLoginTokenSource(cacheSpec(), func(ctx context.Context, call *transport.Call) (*transport.Result, error) {
				t.Fatal("the cached token must be reused")
				return nil, nil
			})
			src.UseCache(path, "default")
			now := time.Now()
			refused := now.Add(-tc.age)
			entry := fmt.Sprintf(`{%q:{"token":"tok-relogin","expires_at":%q,"issued_at":%q,"relogins":[%q],"relogin_ages":[%d]}}`,
				src.CacheKey(), now.Add(time.Hour).Format(time.RFC3339Nano), refused.Format(time.RFC3339Nano),
				refused.Format(time.RFC3339Nano), int64(20*time.Second))
			if err := os.WriteFile(path, []byte(entry), 0o600); err != nil {
				t.Fatal(err)
			}
			tok, err := src.Token(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			src.Accepted(tok)
			r, _ := src.Refusal(tok, time.Now())
			if kept := len(r.Relogins) == 1; kept != tc.kept {
				t.Fatalf("a token accepted at %v after a refusal at 20s: chain kept=%v, want %v", tc.age, kept, tc.kept)
			}
			raw, _ := os.ReadFile(path)
			if kept := strings.Contains(string(raw), "relogins"); kept != tc.kept {
				t.Fatalf("the cache must keep the chain only while the token is younger than the refusal: %s", raw)
			}
		})
	}
}
