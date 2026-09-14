package catalog_test

import (
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestDetectLoginsFindsTheLoginAndWhatItNeeds(t *testing.T) {
	found := catalog.DetectLogins(catalogtest.New())
	if len(found) == 0 {
		t.Fatal("no login detected in the test descriptor, which has AuthService/Login — without this " +
			"shrt init cannot scaffold auth, and auth is the one hand edit every adopter must make")
	}
	best := found[0]
	if best.Method.Name != "Login" {
		t.Fatalf("best candidate is %s, want Login", best.Method.FullName)
	}
	if best.UserField != "username" || best.PasswordName != "password" {
		t.Errorf("credential fields = %q/%q, want username/password", best.UserField, best.PasswordName)
	}
	if best.TokenPath != "access_token" {
		t.Errorf("token path = %q, want access_token", best.TokenPath)
	}
	if best.ExpiresPath != "expires_at" {
		t.Errorf("expires path = %q, want expires_at", best.ExpiresPath)
	}
}

func TestDetectLoginsRefusesAnRPCThatReturnsNoToken(t *testing.T) {
	for _, c := range catalog.DetectLogins(catalogtest.New()) {
		if c.TokenPath == "" {
			t.Fatalf("%s was offered as a login while returning no token — scaffolding auth from it "+
				"would produce a config that cannot work", c.Method.FullName)
		}
		if c.Method.Name == "Create" || c.Method.Name == "Fetch" {
			t.Fatalf("%s was offered as a login", c.Method.FullName)
		}
	}
}

func TestDetectLoginsRanksAReadWriteRPCBelowARealLogin(t *testing.T) {
	found := catalog.DetectLogins(catalogtest.Rich())
	for i := 1; i < len(found); i++ {
		if found[i-1].Score < found[i].Score {
			t.Fatalf("candidates are not sorted by confidence: %d before %d", found[i-1].Score, found[i].Score)
		}
	}
}
