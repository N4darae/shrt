package main

import (
	"context"
	"strings"
	"testing"
)

func TestDiffOfSelectorsLabelsEachSideWithItsSelectorAndID(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	for range 2 {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	var derr error
	out := captureStdout(t, func() { derr = runDiff(context.Background(), []string{"cli-thing-flow", "latest", "latest~1"}) })
	if derr != nil {
		t.Fatalf("shrt diff: %v\n%s", derr, out)
	}
	newest, older := ids[len(ids)-1], ids[len(ids)-2]
	for _, want := range []string{"run A = latest (" + newest, "run B = latest~1 (" + older, "b= is the older value"} {
		if !strings.Contains(out, want) {
			t.Errorf("the header lacks %q:\n%s", want, out)
		}
	}
}
