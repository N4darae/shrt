package hollow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/hollow"
)

func writeAllow(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAllowlistRejectsAnEntryWithNoReason(t *testing.T) {
	for _, body := range []string{
		"some-chain some_step\n",
		"some-chain some_step   \n",
		"some-chain\n",
	} {
		if _, err := hollow.LoadAllowlist(writeAllow(t, body), true); err == nil {
			t.Fatalf("an entry with no reason must be an error, not an empty reason: %q", body)
		} else if !strings.Contains(err.Error(), "reason") {
			t.Fatalf("the error must say a reason is required, got %v", err)
		}
	}
}

func TestAllowlistSkipsBlanksAndComments(t *testing.T) {
	path := writeAllow(t, "\n# a comment with no reason\n   # indented comment\n\nc s the read is refused before it runs\n")
	a, err := hollow.LoadAllowlist(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if a.Len() != 1 {
		t.Fatalf("want 1 entry, got %d", a.Len())
	}
	reason, ok := a.Reason("c", "s")
	if !ok || reason != "the read is refused before it runs" {
		t.Fatalf("reason not carried through: %q %v", reason, ok)
	}
}

func TestAllowlistMissingFileIsEmptyUnlessAsked(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "nope.txt")
	a, err := hollow.LoadAllowlist(absent, false)
	if err != nil || a.Len() != 0 {
		t.Fatalf("a default allowlist that does not exist is simply empty, got %v %d", err, a.Len())
	}
	if _, err := hollow.LoadAllowlist(absent, true); err == nil {
		t.Fatal("an explicitly named allowlist that does not exist must fail")
	}
}

func TestAllowlistDoesNotMatchOnStepNameSubstrings(t *testing.T) {
	path := writeAllow(t, "chain reject_bad_id the filter refuses before the read runs\n")
	a, err := hollow.LoadAllowlist(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Reason("chain", "reject_bad_id_v2"); ok {
		t.Fatal("an allowlist entry must exempt exactly one step, never a name prefix")
	}
	if _, ok := a.Reason("other-chain", "reject_bad_id"); ok {
		t.Fatal("an allowlist entry is scoped to its chain")
	}
}
