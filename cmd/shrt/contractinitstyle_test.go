package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestContractInitRerunKeepsFlowListsWhenItAddsAnRPC(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	captureStdout(t, func() {
		if err := contractInit([]string{"-all"}); err != nil {
			t.Fatalf("contract init -all: %v", err)
		}
	})
	paths, err := filepath.Glob(".shrt/contracts/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var target, dropped string
	var kept []string
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		rpcs := mappingChild(doc.Content[0], "rpcs")
		if rpcs == nil || len(rpcs.Content) < 4 {
			continue
		}
		flowStyled(&doc)
		dropped = rpcs.Content[0].Value
		rpcs.Content = rpcs.Content[2:]
		for i := 0; i < len(rpcs.Content); i += 2 {
			kept = append(kept, rpcs.Content[i].Value)
		}
		curated, err := yaml.Marshal(&doc)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, string(curated))
		target = p
		break
	}
	if target == "" {
		t.Fatal("no overlay with two rpcs to curate")
	}
	captureStdout(t, func() {
		if err := contractInit([]string{"-all"}); err != nil {
			t.Fatalf("contract init -all rerun: %v", err)
		}
	})
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, dropped+":") {
		t.Fatalf("the rerun scaffolds the missing rpc %s:\n%s", dropped, text)
	}
	block := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			block++
		}
	}
	if block > 0 {
		t.Errorf("a flow-style list the user wrote is rewritten as block style:\n%s", text)
	}
	if first := strings.Index(text, kept[0]+":"); first < 0 || first > strings.Index(text, dropped+":") {
		t.Errorf("the rpcs already in the file keep their place, and the new one comes after them:\n%s", text)
	}
}

func mappingChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
