package transport

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTokenCache_ADifferentCredentialUnderTheSameProfileIsNotReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")

	first := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"checker-one","password":"p1"}`), nil },
	}, nil)
	first.UseCache(path, "checker")
	first.writeCache("token-for-checker-one", time.Now().Add(time.Hour), time.Now(), time.Time{})

	second := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"checker-two","password":"p2"}`), nil },
	}, nil)
	second.UseCache(path, "checker")

	if tok, _, _, _, ok := second.readCache(); ok {
		t.Fatalf("a chain that logs in as checker-two must not be handed checker-one's token %q: "+
			"an authorisation assertion made under the wrong identity proves nothing", tok)
	}

	same := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"checker-one","password":"p1"}`), nil },
	}, nil)
	same.UseCache(path, "checker")
	tok, _, _, _, ok := same.readCache()
	if !ok || tok != "token-for-checker-one" {
		t.Fatalf("the same credential must still hit the cache, got %q ok=%v", tok, ok)
	}
}

func TestTokenCache_TwoProfilesWithTheSameCredentialStayApart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	body := func() ([]byte, error) { return []byte(`{"username":"same","password":"same"}`), nil }

	a := NewLoginTokenSource(AuthSpec{Procedure: "svc/Login", Body: body}, nil)
	a.UseCache(path, "profileA")
	a.writeCache("token-a", time.Now().Add(time.Hour), time.Now(), time.Time{})

	b := NewLoginTokenSource(AuthSpec{Procedure: "other/Login", Body: body}, nil)
	b.UseCache(path, "profileB")
	if _, _, _, _, ok := b.readCache(); ok {
		t.Fatal("two different profiles must not share a cache entry")
	}
}

func TestTokenCache_ATokenIsNeverHandedToAnotherTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	body := func() ([]byte, error) { return []byte(`{"username":"admin","password":"p"}`), nil }

	issuer := NewLoginTokenSource(AuthSpec{Procedure: "svc/Login", Body: body, Target: "http://127.0.0.1:18099"}, nil)
	issuer.UseCache(path, "default")
	issuer.writeCache("token-from-18099", time.Now().Add(time.Hour), time.Now(), time.Time{})

	other := NewLoginTokenSource(AuthSpec{Procedure: "svc/Login", Body: body, Target: "http://127.0.0.1:18777"}, nil)
	other.UseCache(path, "default")
	if tok, _, _, _, ok := other.readCache(); ok {
		t.Fatalf("a token minted by one backend must not be sent to another: got %q for a different target", tok)
	}

	same := NewLoginTokenSource(AuthSpec{Procedure: "svc/Login", Body: body, Target: "http://127.0.0.1:18099"}, nil)
	same.UseCache(path, "default")
	if tok, _, _, _, ok := same.readCache(); !ok || tok != "token-from-18099" {
		t.Fatalf("the same target must still hit the cache, got %q ok=%v", tok, ok)
	}
}
