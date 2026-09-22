package pathmask_test

import (
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestARedactorLeavesABooleanFlagReadable(t *testing.T) {
	r := pathmask.NewRedactor([]string{"**.*password"})

	out, ok := r.Apply(map[string]any{
		"password":             "hunter2",
		"must_change_password": false,
	}).(map[string]any)
	if !ok {
		t.Fatal("Apply did not return a map")
	}

	if out["password"] != pathmask.MaskRedacted {
		t.Errorf("the credential still has to go: %v", out["password"])
	}
	if out["must_change_password"] != false {
		t.Errorf("must_change_password = %v, want false. A bool cannot be a credential, and redacting "+
			"it costs the reason a failed assertion failed: the record prints want=<redacted> "+
			"got=<redacted> and says nothing", out["must_change_password"])
	}
}

func TestAVolatileMaskerStillMasksABoolean(t *testing.T) {
	m := pathmask.NewMasker([]string{"**.is_stale"})

	out, ok := m.Apply(map[string]any{"is_stale": true}).(map[string]any)
	if !ok {
		t.Fatal("Apply did not return a map")
	}
	if out["is_stale"] != pathmask.MaskVolatile {
		t.Errorf("volatile masking is about a value that changes between runs, not about secrecy, so a "+
			"bool is a legitimate target there: %v", out["is_stale"])
	}
}

func TestMasksValueSeparatesTheFlagFromTheSecretAtOnePath(t *testing.T) {
	r := pathmask.NewRedactor([]string{"**.*password"})

	if !r.MasksValue("login.password", "hunter2") {
		t.Error("a string at a redacted path is still redacted")
	}
	if r.MasksValue("login.must_change_password", true) {
		t.Error("a bool at the same glob is not")
	}
}
