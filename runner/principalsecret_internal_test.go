package runner

import "testing"

func TestThePrincipalDigestLeavesOutANestedSecret(t *testing.T) {
	digest := func(secret string) string {
		bs := AuthBindings{{Profile: "default", Procedure: "/shop.auth.v1.AuthService/Login",
			BodyFields: map[string]any{"username": "admin", "password": secret,
				"device": map[string]any{"name": "ci", "client_secret": secret}}}}
		return bs.principals()["default"]
	}
	if a, b := digest("pw-one"), digest("pw-two"); a == "" || a != b {
		t.Fatalf("a secret, nested or not, is not part of who logged in, got %q and %q", a, b)
	}
	other := AuthBindings{{Profile: "default", Procedure: "/shop.auth.v1.AuthService/Login",
		BodyFields: map[string]any{"username": "clerk", "password": "pw-one"}}}.principals()["default"]
	if other == digest("pw-one") {
		t.Fatal("another username is another principal")
	}
}
