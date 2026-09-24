package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func writeNoisyChain(t *testing.T, createName string) {
	t.Helper()
	writeFile(t, ".shrt/chains/cli-noisy-flow.yaml", `apiVersion: shrt/v1
name: cli-noisy-flow
steps:
    - id: create
      call: ThingService/Create
      body:
          name: `+createName+`
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fill
      call: ThingService/Create
      body:
          name: filler
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: widget
`)
}

func sliceVerify(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-noisy-flow", "-step", "fetch", "-verify", "-run", "latest"}, args...))
	})
	return out, err
}

func exitCodeOf(err error) int {
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	if err != nil {
		return 1
	}
	return 0
}

func TestCLISliceVerifyIsInconclusiveWhenItDroppedAWrite(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}

	out, err := sliceVerify(t)
	if strings.Contains(out, "verify reproduced") {
		t.Errorf("the slice dropped a write, so a matching verdict can be an accident of the state it "+
			"skipped; claiming reproduced contradicts the under-inclusion warning above it:\n%s", out)
	}
	if !strings.Contains(out, "INCONCLUSIVE") {
		t.Errorf("a matching verdict on an under-inclusive slice must read as inconclusive:\n%s", out)
	}
	if err == nil {
		t.Error("an inconclusive verify is not a receipt and must not exit 0")
	}
	if got := exitCodeOf(err); got != 3 {
		t.Errorf("inconclusive exits %d, want 3, distinct from not reproduced (1) and did not run (2)", got)
	}
}

func TestCLISliceVerifySaysDidNotRunWhenAnEnvVarIsUnset(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "${env.SHRT_SLICE_TEST_NAME}")
	t.Setenv("SHRT_SLICE_TEST_NAME", "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	if err := os.Unsetenv("SHRT_SLICE_TEST_NAME"); err != nil {
		t.Fatal(err)
	}

	out, err := sliceVerify(t)
	text := out
	if err != nil {
		text += "\n" + err.Error()
	}
	if strings.Contains(strings.ToLower(text), "not reproduced") {
		t.Errorf("nothing reached the backend, so nothing can be not-reproduced:\n%s", text)
	}
	if !strings.Contains(text, "DID NOT RUN") {
		t.Errorf("a slice that stopped before its target must say it did not run:\n%s", text)
	}
	if !strings.Contains(text, "SHRT_SLICE_TEST_NAME") {
		t.Errorf("the reason the slice stopped should be shown:\n%s", text)
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("did not run exits %d, want 2", got)
	}
}

func TestCLISliceVerifyLabelsTheConfiguredEnvelopePath(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", string(raw)+"conventions:\n    envelope_path: error.message\n")
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}

	out, _ := sliceVerify(t)
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "source:") || strings.HasPrefix(l, "slice:") {
			if !strings.Contains(l, "error.message") || strings.Contains(l, "error.code") {
				t.Errorf("the verdict label must name conventions.envelope_path, not the default: %q", l)
			}
		}
	}
	if !strings.Contains(out, "source:") {
		t.Fatalf("no verdict lines printed:\n%s", out)
	}
}
