package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestUnquotedReferenceInFlowMappingNamesItsLineAndTheQuote(t *testing.T) {
	cases := map[string]string{
		"apiVersion: shrt/v1\nname: f\nsteps:\n  - id: a\n    body: {sku: ${uuid}}\n    call: X/Y\n":                    "line 5: quote",
		"apiVersion: shrt/v1\nname: f\nsteps:\n  - id: a\n    body: {a: 1,\n      b: [x-${vars.tag}]}\n    call: X/Y\n": "line 6: quote",
	}
	for body, want := range cases {
		path := filepath.Join(t.TempDir(), "f.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := chain.LoadFile(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in the parse error, got %v", want, err)
		}
	}
	path := filepath.Join(t.TempDir(), "g.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: shrt/v1\nname: g\nsteps:\n  - id: a\n    body: {a: 1\n    call: X/Y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.LoadFile(path); err == nil || strings.Contains(err.Error(), "quote a ${") {
		t.Fatalf("a flow error with no ${...} gets no quote hint, got %v", err)
	}
}
