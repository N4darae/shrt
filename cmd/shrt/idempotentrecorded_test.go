package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const idempotentOwnedChain = `apiVersion: shrt/v1
name: cli-idem-owned
vars:
    tag: one
steps:
    - id: own
      call: ThingService/Create
      body:
          name: owner-${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body:
          name: ${own.id}
          idempotency_key: k-${vars.tag}
      expect:
          - path: error.code
            equals: OK
`

func TestAKeyAnEarlierRecordedRunSentIsAFixtureReuseNotARegression(t *testing.T) {
	srv := newIdempotentBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-idem-owned.yaml", idempotentOwnedChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-idem-owned", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem-owned", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-idem-owned", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if err := runRun(ctx, []string{"cli-idem-owned", "-quiet", "-var", "tag=two"}); err != nil {
			t.Fatalf("second run: %v", err)
		}
	})
	runs, _ := os.ReadDir(filepath.Join(".shrt", "runs", "cli-idem-owned"))
	if len(runs) != 2 {
		t.Fatalf("want two run records, got %d", len(runs))
	}
	second := ""
	for _, r := range runs {
		raw, _ := os.ReadFile(filepath.Join(".shrt", "runs", "cli-idem-owned", r.Name()))
		if strings.Contains(string(raw), "k-two") {
			second = strings.TrimSuffix(r.Name(), ".json")
		}
	}
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-idem-owned", "-quiet", "-var", "tag=two"}) })
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 || strings.HasPrefix(err.Error(), "regression") {
		t.Fatalf("the backend replayed what run %s created with key k-two: fixture reused, exit 3, not a regression: %v\n%s", second, err, out)
	}
	for _, want := range []string{"fixture reused", "k-two", second, "-var tag="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "confirmed run") {
		t.Errorf("the key is not the confirmed run's, so do not say it is: %v", err)
	}
}
