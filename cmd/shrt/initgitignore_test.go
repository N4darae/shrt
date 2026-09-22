package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestInitWritesAGitignoreThatKeepsTheTokenCacheOut(t *testing.T) {
	root := t.TempDir()

	added, err := ensureGitignore(root, config.Default().NeverCommit())
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("a repo with no .gitignore must get one")
	}

	body := readGitignore(t, root)
	if !strings.Contains(body, ".shrt/tokens.json") {
		t.Errorf("the token cache holds live bearer tokens and is the one entry that cannot be fixed "+
			"after the fact: %q", body)
	}
	if !strings.Contains(body, ".shrt/runs/") {
		t.Errorf("build output belongs there too: %q", body)
	}
}

func TestInitAppendsOnlyWhatIsMissingFromAnExistingGitignore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(path, []byte("/bin\n.shrt/runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureGitignore(root, config.Default().NeverCommit()); err != nil {
		t.Fatal(err)
	}

	body := readGitignore(t, root)
	if !strings.HasPrefix(body, "/bin\n.shrt/runs/\n") {
		t.Errorf("an adopter's own entries are not ours to rewrite: %q", body)
	}
	if strings.Count(body, ".shrt/runs/") != 1 {
		t.Errorf("a line already present must not be duplicated: %q", body)
	}
	if !strings.Contains(body, ".shrt/tokens.json") {
		t.Errorf("the missing lines still have to arrive: %q", body)
	}
}

func TestInitLeavesAnAlreadyCompleteGitignoreAlone(t *testing.T) {
	root := t.TempDir()
	want := strings.Join(config.Default().NeverCommit(), "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	added, err := ensureGitignore(root, config.Default().NeverCommit())
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("re-running init must not keep appending, or the file grows on every run")
	}
	if got := readGitignore(t, root); got != want {
		t.Errorf("the file was rewritten when nothing was missing: %q", got)
	}
}

func TestInitDoesNotSwallowAFinalLineWithoutANewline(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/bin"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureGitignore(root, config.Default().NeverCommit()); err != nil {
		t.Fatal(err)
	}

	body := readGitignore(t, root)
	if !strings.HasPrefix(body, "/bin\n") {
		t.Errorf("appending to a file with no trailing newline must not join two patterns into one: %q", body)
	}
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
