package coredistillation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/contract"
)

func writeUnder(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantUnknownKey(t *testing.T, what string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s with an unknown key loaded", what)
	}
	if !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "not found in type") {
		t.Fatalf("%s: want %s, not the raw decoder text, got:\n%v", what, want, err)
	}
}

func TestAnUnknownKeyIsNamedWithItsLineAndTheNearestKnownKey(t *testing.T) {
	dir := t.TempDir()

	chainPath := filepath.Join(dir, "c.yaml")
	writeUnder(t, chainPath, "apiVersion: shrt/v1\nname: c\nsteps:\n  - id: a\n    call: ThingService/Create\n    expects:\n      - path: id\n        not_empty: true\n")
	_, err := chain.LoadFile(chainPath)
	wantUnknownKey(t, "a chain", err, `unknown key "expects" at line 6 (did you mean "expect"?)`)

	overlayPath := filepath.Join(dir, "o.yaml")
	writeUnder(t, overlayPath, "apiVersion: shrt/contract/v1\ndomain: things\nrpcs:\n  ThingService/Create:\n    summary: make one\n    requird: [name]\n")
	_, err = contract.LoadOverlay(overlayPath)
	wantUnknownKey(t, "an overlay", err, `unknown key "requird" at line 6 (did you mean "required"?)`)

	root := filepath.Join(dir, "ws")
	writeUnder(t, filepath.Join(root, ".shrt", "config.yaml"), "target:\n  base_url: http://127.0.0.1:1\n  timeuot: 5s\n")
	_, err = config.Load(root)
	wantUnknownKey(t, "a config", err, `unknown key "timeuot" at line 3 (did you mean "timeout"?)`)

	writeUnder(t, filepath.Join(root, ".shrt", "config.yaml"), "target:\n  base_url: http://127.0.0.1:1\n  zzzzzzzz: 5s\n")
	_, err = config.Load(root)
	wantUnknownKey(t, "a config", err, `unknown key "zzzzzzzz" at line 3`)
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("nothing is near zzzzzzzz, so there is no suggestion: %v", err)
	}
}
