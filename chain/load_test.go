package chain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLoadFileRefuses(t *testing.T) {
	head := "apiVersion: shrt/v1\nname: f\nsteps:\n  - id: a\n"
	for body, want := range map[string]string{
		head + "    call: X/Y\n    allow_failure: true\n":                                        "allow_failure",
		head + "    call: X/Y\n    expect:\n      - path: error.code\n        one_of: [OK, x]\n": "one_of",
		head + "    body: {sku: ${uuid}}\n    call: X/Y\n":                                       "line 5: quote",
		head + "    body: {a: 1,\n      b: [x-${vars.tag}]}\n    call: X/Y\n":                    "line 6: quote",
		head + "    body: {a: 1\n    call: X/Y\n":                                                "",
	} {
		path := filepath.Join(t.TempDir(), "f.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := chain.LoadFile(path)
		if err == nil || !strings.Contains(err.Error(), want) || (want == "" && strings.Contains(err.Error(), "quote a ${")) {
			t.Errorf("want %q in the load error, got %v for\n%s", want, err, body)
		}
	}
	dup := &chain.Chain{Name: "t", Steps: []*chain.Step{{ID: "a", Call: "ThingService/Create"}, {ID: "a", Call: "ThingService/Fetch"}}}
	if err := dup.Normalize(); err == nil {
		t.Error("duplicate step ids must be rejected")
	}
}

func TestLoadDirPartialKeepsGoodChainsWhenOneIsBroken(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"alpha.yaml":  "apiVersion: shrt/v1\nname: alpha\nsteps:\n  - id: a\n    call: ThingService/Create\n",
		"beta.yaml":   "apiVersion: shrt/v1\nname: beta\nsteps:\n  - id: a\n    call: ThingService/Create\n",
		"broken.yaml": "apiVersion: shrt/v1\nname: broken\nsteps:\n  - id: dup\n    call: X/Y\n  - id: dup\n    call: X/Z\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chains, broken, err := chain.LoadDirPartial(dir)
	if err != nil || len(chains) != 2 || len(broken) != 1 {
		t.Fatalf("want 2 chains and 1 failure, got %d %d %v", len(chains), len(broken), err)
	}
}
