package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func flowStyled(n *yaml.Node) {
	if n.Kind == yaml.SequenceNode {
		n.Style = yaml.FlowStyle
	}
	for _, c := range n.Content {
		flowStyled(c)
	}
}

func TestContractInitRerunLeavesAnUnchangedOverlayAlone(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	captureStdout(t, func() {
		if err := contractInit([]string{"-all"}); err != nil {
			t.Fatalf("contract init -all: %v", err)
		}
	})
	paths, err := filepath.Glob(".shrt/contracts/*.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no overlay written: %v", err)
	}
	want := map[string][]byte{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		flowStyled(&doc)
		curated, err := yaml.Marshal(&doc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(curated), "[") {
			continue
		}
		writeFile(t, p, string(curated))
		want[p] = curated
	}
	if len(want) == 0 {
		t.Fatal("no overlay has a list to restyle")
	}
	out := captureStdout(t, func() {
		if err := contractInit([]string{"-all"}); err != nil {
			t.Fatalf("contract init -all rerun: %v", err)
		}
	})
	for p, curated := range want {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(curated) {
			t.Errorf("%s was rewritten though its content did not change:\n--- before\n%s\n--- after\n%s", p, curated, got)
		}
		if !strings.Contains(out, "unchanged "+p) {
			t.Errorf("the rerun must say %s is unchanged:\n%s", p, out)
		}
	}
}
