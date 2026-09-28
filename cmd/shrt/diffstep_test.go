package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffStepPrintsOneRecordedStepsRequestAndResponse(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := e.store.ListRuns("cli-thing-flow")
	rec, err := e.store.LoadRun("cli-thing-flow", ids[len(ids)-1])
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	var req, resp bytes.Buffer
	_ = json.Compact(&req, st.Request)
	_ = json.Compact(&resp, st.Response)
	var derr error
	out := captureStdout(t, func() { derr = runDiff(context.Background(), []string{"cli-thing-flow", "-step", st.ID}) })
	if derr != nil {
		t.Fatalf("shrt diff -step: %v\n%s", derr, out)
	}
	for _, want := range []string{st.ID + " (" + shortRPC(st.Call) + ") passed in run " + rec.RunID + "\n", "request " + req.String() + "\n", "response " + resp.String() + "\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	derr = runDiff(context.Background(), []string{"cli-thing-flow", "latest", "-step", "no_such_step"})
	if derr == nil || !strings.Contains(derr.Error(), "its steps: "+st.ID) {
		t.Errorf("an unknown step names the run's steps, got %v", derr)
	}
}
