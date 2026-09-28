package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLISliceVerifyWithoutWriteDoesNotReprintTheSliceAfterTheVerdict(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	out, _ := sliceVerify(t)
	verdict := strings.Index(out, "verify INCONCLUSIVE")
	if verdict < 0 {
		t.Fatalf("want an INCONCLUSIVE verdict:\n%s", out)
	}
	if strings.Contains(out[verdict:], "apiVersion: shrt/v1") || strings.Contains(out[verdict:], "steps:\n") {
		t.Fatalf("the verdict is the last word under -verify; the slice YAML is not printed after it:\n%s", out)
	}
	for _, want := range []string{"dropped write step(s) on entities the kept steps use", "-keep writes"} {
		if !strings.Contains(out[verdict:], want) {
			t.Errorf("name the dropped writes and give the -keep writes command (%q):\n%s", want, out)
		}
	}
}
