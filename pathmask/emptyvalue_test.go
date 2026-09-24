package pathmask_test

import (
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestARedactorLeavesAnEmptySecretVisible(t *testing.T) {
	r := pathmask.NewRedactor([]string{"**.access_token"})
	got := r.Apply(map[string]any{"access_token": "", "login": map[string]any{"access_token": "tok-1"}}).(map[string]any)
	if got["access_token"] != "" {
		t.Fatalf("an empty token hides that none was sent when masked, got %v", got["access_token"])
	}
	if inner := got["login"].(map[string]any); inner["access_token"] != pathmask.MaskRedacted {
		t.Fatalf("a real token must still be masked, got %v", inner["access_token"])
	}
	if r.MasksValue("access_token", nil) || !r.MasksValue("access_token", "0") {
		t.Fatal("null reveals nothing and stays visible; the string \"0\" could be a secret and is masked")
	}
}
