package pathmask_test

import (
	"testing"

	"github.com/N4darae/shrt/pathmask"
)

func TestMatchGlobsWithinOneSegment(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**.*password", "steps.0.request.new_password", true},
		{"**.*password", "steps.0.request.old_password", true},
		{"**.*password", "steps.0.request.password", true},
		{"**.*password", "vars.staff_password", true},
		{"**.*password", "steps.0.request.newPassword", true},
		{"**.*password", "steps.0.request.password_hint", false},
		{"**.*token", "response.access_token", true},
		{"**.*token", "response.tokenizer", false},
		{"**.password", "steps.0.request.new_password", false},
		{"**.password", "steps.0.request.password", true},
		{"**.*", "vars.anything", true},
		{"a.*.c", "a.b.c", true},
		{"a.*.c", "a.b.d", false},
	}
	for _, c := range cases {
		if got := pathmask.Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestRedactorAppliesAGlobbedSegment(t *testing.T) {
	m := pathmask.NewRedactor([]string{"**.*password"})
	out := m.Apply(map[string]any{
		"old_password": "before",
		"new_password": "after",
		"username":     "alice",
	})
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("Apply returned %T", out)
	}
	for _, k := range []string{"old_password", "new_password"} {
		if got[k] != pathmask.MaskRedacted {
			t.Errorf("%s = %v, want %s", k, got[k], pathmask.MaskRedacted)
		}
	}
	if got["username"] != "alice" {
		t.Errorf("username = %v, want alice — a glob must not widen past its own segment", got["username"])
	}
}
