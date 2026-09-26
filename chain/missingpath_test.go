package chain

import (
	"errors"
	"testing"
)

func TestAMissingPathNamesTheNearestPartTheResponseHad(t *testing.T) {
	s := NewScope(nil)
	s.Record("create_product", nil, map[string]any{"status": map[string]any{"code": "SUCCESS", "details": []any{}}})
	_, err := s.ResolveValue("${create_product.status.details.0.reason}")
	var missing *MissingPathError
	if !errors.As(err, &missing) {
		t.Fatalf("want a MissingPathError, got %v", err)
	}
	want := `unresolved reference ${create_product.status.details.0.reason}: step "create_product" answered without status.details.0.reason (status.details is [])`
	if err.Error() != want {
		t.Fatalf("got  %s\nwant %s", err, want)
	}
}
