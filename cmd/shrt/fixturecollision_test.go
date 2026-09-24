package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func approveUniqueChain(t *testing.T, ctx context.Context) {
	t.Helper()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unique", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-note", "unique names"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
}

func createForeignThing(t *testing.T, base, name string) {
	t.Helper()
	resp, err := http.Post(base+"/shrt.test.v1.ThingService/Create", "application/json", strings.NewReader(`{"name":"`+name+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestVerifyCallsAForeignUniquenessConflictAFixtureCollision(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	createForeignThing(t, srv.URL, "widget foreign7")
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=foreign7"}) })
	if err == nil || strings.Contains(err.Error(), "regression") {
		t.Fatalf("a conflict with a record another client created is not a regression: %v\n%s", err, out)
	}
	var coded *exitError
	if !errors.As(err, &coded) || coded.code != 3 {
		t.Fatalf("a fixture collision is could-not-verify, exit 3, got %v", err)
	}
	if !strings.Contains(err.Error(), "fixture collision") || !strings.Contains(err.Error(), "-var tag=") {
		t.Fatalf("the verdict must say fixture collision and suggest a fresh -var: %v", err)
	}
	if strings.Contains(err.Error(), "fixture reused") {
		t.Fatalf("no run of this chain used tag=foreign7, so it is not a reuse: %v", err)
	}
}

func TestRunPrintsTheFixtureCollisionHint(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	createForeignThing(t, srv.URL, "widget foreign8")
	var err error
	out := captureStdout(t, func() { err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=foreign8"}) })
	if err == nil {
		t.Fatal("a refused create does not pass")
	}
	if !strings.Contains(out, "fixture collision") || !strings.Contains(out, "shrt run cli-unique -var tag=") {
		t.Fatalf("run must name the fixture collision and suggest a fresh -var:\n%s", out)
	}
}

func TestAnEarlierRefusedRunDidNotUseTheFixture(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	createForeignThing(t, srv.URL, "widget dup")
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=dup"}) })
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=dup"}) })
	if err == nil || strings.Contains(err.Error(), "fixture reused") {
		t.Fatalf("the earlier run's create was refused, so it created nothing and did not use the value: %v\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "fixture collision") {
		t.Fatalf("with no own run having created the record, it is a fixture collision: %v", err)
	}
}
