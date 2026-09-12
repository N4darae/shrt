package contract_test

import (
	"testing"

	"github.com/N4darae/shrt/contract"
)

func TestPlaceholderStrictness(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		scaffold bool
		authored bool
	}{
		{"empty string", "", true, true},
		{"absent enum member", "SIGN_FLAG_UNSPECIFIED", true, true},
		{"nil", nil, true, true},
		{"empty list", []any{}, true, true},
		{"list of blanks", []any{"", ""}, true, true},
		{"empty map", map[string]any{}, true, true},
		{"int64 zero as text", "0", true, false},
		{"numeric zero", float64(0), true, false},
		{"real value", "100", false, false},
		{"template", "${uuid}", false, false},
		{"false", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contract.IsPlaceholder(c.value, contract.ScaffoldedBody); got != c.scaffold {
				t.Errorf("ScaffoldedBody: got %v want %v", got, c.scaffold)
			}
			if got := contract.IsPlaceholder(c.value, contract.AuthoredBody); got != c.authored {
				t.Errorf("AuthoredBody: got %v want %v", got, c.authored)
			}
		})
	}
}

func TestHasUsableValueWalksPaths(t *testing.T) {
	body := map[string]any{
		"id_deal": "d-1",
		"lines":   []any{map[string]any{"id_asset": "a-1", "qty": "0"}},
		"nested":  map[string]any{"code": ""},
	}
	for _, c := range []struct {
		path string
		how  contract.Strictness
		want bool
	}{
		{"id_deal", contract.AuthoredBody, true},
		{"lines.0.id_asset", contract.AuthoredBody, true},
		{"lines.id_asset", contract.AuthoredBody, true},
		{"lines.0.qty", contract.AuthoredBody, true},
		{"lines.0.qty", contract.ScaffoldedBody, false},
		{"nested.code", contract.AuthoredBody, false},
		{"absent", contract.AuthoredBody, false},
	} {
		if got := contract.HasUsableValue(body, c.path, c.how); got != c.want {
			t.Errorf("%s (%v): got %v want %v", c.path, c.how, got, c.want)
		}
	}
}
