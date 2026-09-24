package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 1024)
		for {
			n, err := r.Read(chunk)
			buf = append(buf, chunk[:n]...)
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()
	fn()
	_ = w.Close()
	os.Stderr = saved
	return <-done
}

func TestCLISliceHelpSaysWriteTakesAnOptionalName(t *testing.T) {
	var err error
	out := captureStderr(t, func() { err = chainSlice(context.Background(), []string{"-h"}) })
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-write [name]") {
		t.Errorf("-write takes an optional name and the help must show it, not a bare boolean:\n%s", out)
	}
	if !strings.Contains(out, "-keep id[,id]") {
		t.Errorf("-keep must be listed with what it takes:\n%s", out)
	}
}

func TestCLISliceVerifyWithoutWriteLeavesNoOrphanRunRecord(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}

	out, _ := sliceVerify(t)
	if _, err := os.Stat(".shrt/runs/cli-noisy-flow-slice-fetch"); !os.IsNotExist(err) {
		t.Errorf("an unwritten slice has no chain file, so its run record would be an orphan; stat: %v", err)
	}
	if !strings.Contains(out, "slice run not kept") {
		t.Errorf("the output must say the slice run was not kept and how to keep it:\n%s", out)
	}
	var herr error
	hollow := captureStdout(t, func() { herr = chainHollow([]string{}) })
	if strings.Contains(hollow, "orphan") {
		t.Errorf("chain hollow reports an orphan after slice -verify (err %v):\n%s", herr, hollow)
	}
}

func TestCLISliceInconclusiveNamesTheCommandThatKeepsTheWrites(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	source := latestRunID(t, "cli-noisy-flow")

	out, err := sliceVerify(t)
	if exitCodeOf(err) != 3 {
		t.Fatalf("the plain slice is inconclusive, got exit %d:\n%s", exitCodeOf(err), out)
	}
	want := "next: shrt chain slice cli-noisy-flow -step fetch -run " + source + " -keep fill -verify -write"
	if !strings.Contains(out, want) {
		t.Fatalf("the INCONCLUSIVE advice must name a runnable command\nwant %q in:\n%s", want, out)
	}

	cmd := strings.Fields(regexp.MustCompile(`next: shrt chain slice (.*)`).FindStringSubmatch(out)[1])
	var rerr error
	again := captureStdout(t, func() { rerr = chainSlice(context.Background(), cmd) })
	if rerr != nil {
		t.Fatalf("the suggested command must settle the question, got %v:\n%s", rerr, again)
	}
	if !strings.Contains(again, "verify reproduced") {
		t.Errorf("with the write kept the verdict is a receipt:\n%s", again)
	}
	if _, err := os.Stat(".shrt/chains/cli-noisy-flow-slice-fetch.yaml"); err != nil {
		t.Errorf("-write must keep the slice: %v", err)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-noisy-flow-slice-fetch")
	if err != nil || len(entries) != 1 {
		t.Errorf("a written slice keeps its verify run record next to its chain: %v, %d", err, len(entries))
	}
}
