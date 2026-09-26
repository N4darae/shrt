package main

import (
	"context"
	"strings"
	"testing"
)

const undeclaredTagChain = `apiVersion: shrt/v1
name: cli-fresh-tag
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: total
            equals: 300
`

func TestAnUndeclaredTagIsFreshEveryRunAndVerifyStillComparesTheRuns(t *testing.T) {
	regressed := false
	srv := newTotalBackend(&regressed)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-fresh-tag.yaml", undeclaredTagChain)
	ctx := context.Background()
	e, err := loadEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{}
	for i := 0; i < 2; i++ {
		captureStdout(t, func() {
			if err := runRun(ctx, []string{"cli-fresh-tag", "-quiet"}); err != nil {
				t.Fatalf("run %d with no -var: %v", i, err)
			}
		})
		rec, err := e.store.LatestRun("cli-fresh-tag")
		if err != nil {
			t.Fatal(err)
		}
		tag, _ := rec.Vars["tag"].(string)
		if tag == "" || tags[tag] {
			t.Fatalf("run %d: each run records a tag of its own, got %v after %v", i, rec.Vars, tags)
		}
		tags[tag] = true
	}
	captureStdout(t, func() {
		if err := runConfirm(ctx, []string{"cli-fresh-tag", "-note", "total 300"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-fresh-tag", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-fresh-tag", "-quiet"}) })
	if verr != nil || !strings.Contains(out, "no drift") {
		t.Fatalf("a fresh tag only renames fixtures, so verify of a healthy backend is clean: %v\n%s", verr, out)
	}
	regressed = true
	out = captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-fresh-tag", "-quiet"}) })
	if verr == nil || !strings.Contains(verr.Error(), "regression") || strings.Contains(out, "drift with different input") {
		t.Fatalf("with fresh tags a changed total is still a regression: %v\n%s", verr, out)
	}
}
