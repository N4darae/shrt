package store_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/store"
)

func TestAMissingBaselineSaysHowToCreateIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope")
	_, _, err := store.Ratchet(path, 7)
	if err == nil {
		t.Fatal("a missing baseline cannot be ratcheted against")
	}
	for _, want := range []string{path, "does not exist", "echo 7 > " + path, "write 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error must say how to create the baseline (%q), got %v", want, err)
		}
	}
}
