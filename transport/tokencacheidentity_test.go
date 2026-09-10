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
	first.writeCache("token-for-checker-one", time.Now().Add(time.Hour))

	second := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"checker-two","password":"p2"}`), nil },
	}, nil)
	second.UseCache(path, "checker")

	if tok, _, ok := second.readCache(); ok {
		t.Fatalf("a chain that logs in as checker-two must not be handed checker-one's token %q: "+
			"an authorisation assertion made under the wrong identity proves nothing", tok)
	}

	same := NewLoginTokenSource(AuthSpec{
		Procedure: "svc/Login",
		Body:      func() ([]byte, error) { return []byte(`{"username":"checker-one","password":"p1"}`), nil },
	}, nil)
	same.UseCache(path, "checker")
	tok, _, ok := same.readCache()
	if !ok || tok != "token-for-checker-one" {
		t.Fatalf("the same credential must still hit the cache, got %q ok=%v", tok, ok)
	}
}

func TestTokenCache_TwoProfilesWithTheSameCredentialStayApart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	body := func() ([]byte, error) { return []byte(`{"username":"same","password":"same"}`), nil }

	a := NewLoginTokenSource(AuthSpec{Procedure: "svc/Login", Body: body}, nil)
	a.UseCache(path, "profileA")
	a.writeCache("token-a", time.Now().Add(time.Hour))

	b := NewLoginTokenSource(AuthSpec{Procedure: "other/Login", Body: body}, nil)
	b.UseCache(path, "profileB")
	if _, _, ok := b.readCache(); ok {
		t.Fatal("two different profiles must not share a cache entry")
	}
}
