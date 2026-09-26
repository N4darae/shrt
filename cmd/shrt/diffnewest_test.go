package main

import (
	"context"
	"strings"
	"testing"
)

func TestCLIDiffOfAnUnknownRunListsTheNewestRunsNewestFirst(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	for range 4 {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil || len(ids) != 4 {
		t.Fatalf("want 4 runs, got %v %v", ids, err)
	}
	err = runDiff(context.Background(), []string{"cli-thing-flow", "bogus", "latest"})
	if err == nil {
		t.Fatal("an unknown run id must fail")
	}
	want := "newest first: " + ids[3] + ", " + ids[2] + ", " + ids[1] + ")"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the listing is labelled newest, so the newest run comes first; want %q in %v", want, err)
	}
}
