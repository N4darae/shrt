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
	want := "next: shrt chain slice cli-noisy-flow -step fetch -run " + source + " -keep writes -verify -write"
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
	if _, err := os.Stat(".shrt/chains/cli-noisy-flow-slice-fetch.yaml"); err == nil {
		t.Errorf("with every write kept the slice is the chain itself, so -write records the verdict there instead of copying it")
	}
	raw, err := os.ReadFile(".shrt/chains/cli-noisy-flow.yaml")
	if err != nil || !strings.Contains(string(raw), "VERIFIED by 'shrt chain slice -verify'") {
		t.Errorf("the verdict must be recorded in the chain the slice equals: %v\n%s", err, raw)
	}
	entries, err := os.ReadDir(".shrt/runs/cli-noisy-flow")
	if err != nil || len(entries) != 2 {
		t.Errorf("the verify run record is kept next to the chain it ran: %v, %d", err, len(entries))
	}
}

func TestCLISliceNextKeepsTheWritePathTheUserGave(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	out, err := sliceVerify(t, "-write", ".shrt/scratch/noisy-repro.yaml")
	if exitCodeOf(err) != 3 {
		t.Fatalf("the plain slice is inconclusive, got exit %d:\n%s", exitCodeOf(err), out)
	}
	if !strings.Contains(out, "-verify -write .shrt/scratch/noisy-repro.yaml\n") {
		t.Fatalf("next must write where the user asked, not into .shrt/chains:\n%s", out)
	}
}

func TestCLISliceRecordsAnInconclusiveVerdictInPlaceOfTheHypothesis(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeNoisyChain(t, "widget")
	if err := runRun(context.Background(), []string{"cli-noisy-flow", "-quiet"}); err != nil {
		t.Fatalf("shrt run: %v", err)
	}
	out, err := sliceVerify(t, "-write", ".shrt/scratch/noisy-repro.yaml")
	if exitCodeOf(err) != 3 {
		t.Fatalf("the plain slice is inconclusive, got exit %d:\n%s", exitCodeOf(err), out)
	}
	written := string(mustRead(t, ".shrt/scratch/noisy-repro.yaml"))
	if !strings.Contains(written, "INCONCLUSIVE by 'shrt chain slice -verify'") || strings.Contains(written, "HYPOTHESIS") {
		t.Fatalf("the INCONCLUSIVE verdict replaces the hypothesis paragraph:\n%s", written)
	}
}
