package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runIDs(t *testing.T) []string {
	t.Helper()
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := e.store.ListRuns("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestCLIDiffOfRunsAndReplays(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	for range 2 {
		if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	}
	ids := runIDs(t)
	if out, err := fixCmd(t, "diff", "cli-thing-flow"); err != nil {
		t.Fatalf("two runs differing only in ids and timestamps compare the same: %v\n%s", err, out)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err == nil {
		t.Fatal("diff must not create a safe spot")
	}
	if out, err := fixCmd(t, "diff", ids[0], ids[1]); err != nil || !strings.Contains(out, "cli-thing-flow") {
		t.Fatalf("diff of two run ids finds and names their chain: %v\n%s", err, out)
	}
	out, err := fixCmd(t, "diff", "cli-thing-flow", "latest", "latest~1")
	for _, want := range []string{"run A = latest (" + ids[1], "run B = latest~1 (" + ids[0], "b= is the older value"} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("the header lacks %q: %v\n%s", want, err, out)
		}
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.LoadRun("cli-thing-flow", ids[1])
	if err != nil {
		t.Fatal(err)
	}
	st := rec.Steps[0]
	var req, resp bytes.Buffer
	_ = json.Compact(&req, st.Request)
	_ = json.Compact(&resp, st.Response)
	out, err = fixCmd(t, "diff", "cli-thing-flow", "-step", st.ID)
	for _, want := range []string{st.ID + " (" + shortRPC(st.Call) + ") passed in run " + rec.RunID + "\n", "request " + req.String() + "\n", "response " + resp.String() + "\n"} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("diff -step: want %q: %v\n%s", want, err, out)
		}
	}
	last := rec.Steps[len(rec.Steps)-1]
	out, err = fixCmd(t, "diff", "cli-thing-flow", "-step", st.ID+","+last.ID)
	if err != nil || !strings.Contains(out, "response "+resp.String()+"\n") || strings.Count(out, " in run "+rec.RunID+"\n") != 2 {
		t.Errorf("diff -step a,b prints each step: %v\n%s", err, out)
	}
	writeFile(t, ".shrt/chains/other-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "name: cli-thing-flow", "name: other-flow", 1))
	for _, tc := range []struct {
		args        []string
		code        int
		want, never string
	}{
		{[]string{"cli-thing-flow", "latest", "-step", "no_such_step"}, 2, "its steps: " + st.ID, ""},
		{[]string{"cli-thing-flow", "latest", "no-such-run"}, 2, "chain cli-thing-flow has no run no-such-run", "no such file"},
		{[]string{"cli-thing-flow", "bogus", "latest"}, 2, "newest first: " + ids[1] + ", " + ids[0] + ")", ""},
		{[]string{"cli-thing-flow", "other-flow"}, 2, "cli-thing-flow and other-flow are chain names, not run ids", "no run cli-thing-flow"},
		{[]string{"cli-thing-flow", "latest"}, 2, "cli-thing-flow is a chain name: with two arguments both are run ids", ""},
		{[]string{"-nope"}, 1, "", ""},
	} {
		var err error
		captureStderr(t, func() { _, err = fixCmd(t, "diff", tc.args...) })
		if exitCodeOf(err) != tc.code || err == nil || !strings.Contains(err.Error(), tc.want) || tc.never != "" && strings.Contains(err.Error(), tc.never) {
			t.Errorf("diff %v: want exit %d saying %q, got %d: %v", tc.args, tc.code, tc.want, exitCodeOf(err), err)
		}
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	name = "gadget"
	if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err == nil {
		t.Fatal("the fetch step asserts name widget, so this run must fail")
	}
	out, err = fixCmd(t, "diff", "cli-thing-flow", "latest~1", "latest")
	if exitCodeOf(err) != 1 {
		t.Fatalf("runs that differ exit 1, got %v\n%s", err, out)
	}
	for _, want := range []string{"passed -> failed: fetch", "a=widget b=gadget"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "first failing step moved") {
		t.Errorf("the status change already names the first failing step:\n%s", out)
	}
	if strings.Contains(out, "thing-2") || strings.Contains(out, "thing-3") {
		t.Errorf("ids differ every run and must be masked:\n%s", out)
	}
	fixCmd(t, "verify", "cli-thing-flow", "-quiet")
	ids = runIDs(t)
	replay, err := e.store.LoadRun("cli-thing-flow", ids[3])
	if err != nil || replay.ReplayOf != ids[1] {
		t.Fatalf("a verify replay records the safe spot's run it replays: %v %+v", err, replay)
	}
	out, err = fixCmd(t, "diff", "cli-thing-flow")
	if exitCodeOf(err) != 1 || !strings.Contains(out, "run A "+ids[1]) || !strings.Contains(out, "run B "+ids[2]) {
		t.Fatalf("the default diff skips the replay right after a run and compares %s with %s: %v\n%s", ids[1], ids[2], err, out)
	}
	fixCmd(t, "verify", "cli-thing-flow", "-quiet")
	ids = runIDs(t)
	out, err = fixCmd(t, "diff", "cli-thing-flow")
	if err != nil || !strings.Contains(out, "run A "+ids[2]) || !strings.Contains(out, "run B "+ids[4]) {
		t.Fatalf("a replay beside another replay is the newest record, compared with the latest run: %v\n%s", err, out)
	}
	if err := os.Remove(filepath.Join(".shrt", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixCmd(t, "diff", "cli-thing-flow"); exitCodeOf(err) != 1 {
		t.Fatalf("a setup diff cannot load exits 1 like every other command, got %v", err)
	}
	if out := helpOf(t, "diff"); !strings.Contains(out, "a flag that cannot be parsed") || strings.Contains(out, "or bad usage") {
		t.Fatalf("diff -h says a flag it cannot parse exits 1:\n%s", out)
	}
}

func TestCLIDiffMasksWhatVerifyMasks(t *testing.T) {
	t.Run("a fixture echo of a chain run from a file outside the chains dir", func(t *testing.T) {
		chdirToFakeShop(t, newFakeShop())
		writeFile(t, ".shrt/scratch/probe.yaml", `name: probe
steps:
    - id: create
      call: shop.catalog.v1.ProductService/CreateProduct
      body:
        sku: sku-${vars.tag}-a
        name: Probe
        price_minor: "5"
      expect:
        - path: status.code
          equals: SUCCESS
`)
		for range 2 {
			if _, err := fixCmd(t, "run", ".shrt/scratch/probe.yaml", "-quiet"); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
		}
		if out, err := fixCmd(t, "diff", "probe"); err != nil || !strings.Contains(out, "not counted: ") || strings.Contains(out, "product.sku") {
			t.Fatalf("the sku only echoes the tag the run sent: %v\n%s", err, out)
		}
	})
	t.Run("a field declared in one run and never sent", func(t *testing.T) {
		f := fixUndeclaredWorkspace(t, 0, false)
		fixCmd(t, "run", "cli-thing-flow", "-quiet")
		if out, err := fixCmd(t, "diff", "cli-thing-flow"); err != nil || !strings.Contains(out, "not counted: ") {
			t.Fatalf("a field the backend never sent is no difference: %v\n%s", err, out)
		}
		f.set(func(f *fixThing) { f.total = 7 })
		fixCmd(t, "run", "cli-thing-flow", "-quiet")
		if out, err := fixCmd(t, "diff", "cli-thing-flow"); err == nil || !strings.Contains(out, "total") {
			t.Fatalf("a value the backend now sends is still a difference: %v\n%s", err, out)
		}
	})
}
