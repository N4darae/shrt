package main

import (
	"context"
	"strings"
	"testing"
)

const expectVarChain = `apiVersion: shrt/v1
name: cli-expect-var
vars:
    tag: first
    total: 300
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
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
          - path: total
            equals: ${vars.total}
`

func TestVerifyNamesAnExpectationValueAVarChanged(t *testing.T) {
	regressed := false
	srv := newTotalBackend(&regressed)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-expect-var.yaml", expectVarChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-expect-var", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-expect-var", "-note", "total 300"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-expect-var", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	verify := func(args ...string) (string, error) {
		var err error
		out := captureStdout(t, func() { err = runVerify(ctx, append([]string{"cli-expect-var", "-quiet"}, args...)) })
		return out, err
	}
	out, err := verify("-var", "tag=second", "-var", "total=4")
	if err == nil {
		t.Fatalf("the expectation now fails, verify must not pass:\n%s", out)
	}
	if !strings.Contains(out, "expectation differs from the confirmed run at fetch: total equals 300 -> 4 (${vars.total})") {
		t.Errorf("verify must name the expectation whose resolved value changed:\n%s", out)
	}
	if strings.HasPrefix(err.Error(), "regression") || !strings.Contains(err.Error(), "total=4, confirmed with 300") {
		t.Errorf("the responses match; the var changed the expectation, not the backend: %v\n%s", err, out)
	}
	if out, err := verify("-var", "tag=third"); err != nil || strings.Contains(out, "expectation differs") {
		t.Errorf("a fresh fixture tag changes no expectation value: %v\n%s", err, out)
	}
}
