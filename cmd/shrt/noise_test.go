package main

import (
	"context"
	"strings"
	"testing"
)

func TestVersionInitAndChainLsPrintNoParagraphTheyDoNotNeed(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runVersion(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "OLD rules") || strings.Contains(out, "\n\n") {
		t.Errorf("version prints the build and one line on shrt doctor at most:\n%s", out)
	}

	srv := loginServer(t, `{"error":{"code":"DONE"},"access_token":"tok","expires_at":"0"}`)
	dir := loginWorkspace(t, "")
	restore := chdir(t, dir)
	t.Setenv("API_USER", "u")
	t.Setenv("API_PASSWORD", "p")
	if out := cliInit(t, "-base-url", srv.URL, "-v"); strings.Contains(out, "latency:") {
		t.Errorf("init -v says nothing about latency that GRAMMAR.md does not:\n%s", out)
	}
	restore()

	chdirToFreshCLIWorkspace(t, newFakeCLIBackend().URL)
	out = captureStdout(t, func() {
		if err := chainList(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasSuffix(out, "\n1 chain(s)\n") {
		t.Errorf("with no chain marked, the count is the footer:\n%s", out)
	}
	writeFile(t, ".shrt/chains/cli-red.yaml", "apiVersion: shrt/v1\nname: cli-red\nkept_red:\n    - step: fetch\n      path: error.code\nsteps:\n    - id: fetch\n      call: ThingService/Fetch\n      body:\n          id: x\n      expect:\n          - path: error.code\n            equals: OK\n")
	out = captureStdout(t, func() {
		if err := chainList(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasSuffix(out, "\n2 chain(s); R = kept red\n") {
		t.Errorf("the legend names only the marks shown:\n%s", out)
	}
}
