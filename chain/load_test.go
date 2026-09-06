package chain_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLoadDirPartialKeepsGoodChainsWhenOneIsBroken(t *testing.T) {
	dir := t.TempDir()
	good := "apiVersion: shrt/v1\nname: %s\nsteps:\n  - id: a\n    call: ThingService/Create\n"
	write(t, dir, "alpha.yaml", good, "alpha")
	write(t, dir, "beta.yaml", good, "beta")
	write(t, dir, "broken.yaml",
		"apiVersion: shrt/v1\nname: broken\nsteps:\n  - id: dup\n    call: X/Y\n  - id: dup\n    call: X/Z\n")

	chains, broken, err := chain.LoadDirPartial(dir)
	if err != nil {
		t.Fatalf("LoadDirPartial: %v", err)
	}
	if len(chains) != 2 {
		t.Fatalf("want the 2 valid chains, got %d", len(chains))
	}
	if len(broken) != 1 {
		t.Fatalf("want 1 reported failure, got %d", len(broken))
	}
}

func write(t *testing.T, dir, name, format string, args ...any) {
	t.Helper()
	content := format
	if len(args) > 0 {
		content = fmt.Sprintf(format, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
