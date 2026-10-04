package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunSavesAStampedRecordAndADryRunSendsNothing(t *testing.T) {
	calls := 0
	verTServe(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		verTWrite(w, verTOK("id", "thing-1", "name", "widget"))
	}, false)
	verTRun(t, []verTCheck{
		{args: []string{"run", "cli-thing-flow", "-dry-run", "-quiet"}, code: 0, then: func(t *testing.T, _ string) {
			if calls != 0 {
				t.Errorf("a dry run must send nothing, the backend saw %d call(s)", calls)
			}
		}},
		{args: []string{"run", "cli-thing-flow", "-build", "rc-2"}, code: 0, has: []string{"build: rc-2"}, then: func(t *testing.T, _ string) {
			entries, err := os.ReadDir(".shrt/runs/cli-thing-flow")
			if err != nil || len(entries) != 1 {
				t.Fatalf("want one saved run record: %v", err)
			}
			var rec map[string]any
			if err := json.Unmarshal(mustRead(t, filepath.Join(".shrt/runs/cli-thing-flow", entries[0].Name())), &rec); err != nil {
				t.Fatal(err)
			}
			if rec["status"] != "passed" || rec["build"] != "rc-2" {
				t.Errorf("saved record status=%v build=%v, want passed rc-2", rec["status"], rec["build"])
			}
		}},
	})
}

func TestRunRefusesAScratchFileNamedLikeAChainInPathsChains(t *testing.T) {
	verTServe(t, func(w http.ResponseWriter, r *http.Request) { verTWrite(w, verTOK("id", "thing-1", "name", "widget")) }, false)
	writeFile(t, filepath.Join("scratch", "fake.yaml"), "name: cli-thing-flow\nsteps:\n    - id: only\n      call: ThingService/Fetch\n      body: {id: thing-1}\n")
	verTRun(t, []verTCheck{
		{args: []string{"run", filepath.Join("scratch", "fake.yaml"), "-quiet"}, code: 1, has: []string{"cli-thing-flow", "rename"}, then: func(t *testing.T, _ string) {
			if entries, _ := os.ReadDir(".shrt/runs/cli-thing-flow"); len(entries) > 0 {
				t.Errorf("the refused run was stored as the chain's run: %d file(s)", len(entries))
			}
		}},
		{args: []string{"run", filepath.Join(".shrt", "chains", "cli-thing-flow.yaml"), "-quiet"}, code: 0},
	})
}

func TestRunAdvisesRaisingTheTimeoutWhenAStepGotNoAnswerInTime(t *testing.T) {
	srv := verTServe(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Fetch") {
			time.Sleep(400 * time.Millisecond)
		}
		verTWrite(w, verTOK("id", "thing-1", "name", "widget"))
	}, false)
	verTEdit(t, ".shrt/config.yaml", "base_url: "+srv.URL+"\n", "base_url: "+srv.URL+"\n    timeout: 100ms\n")()
	verTRun(t, []verTCheck{{args: []string{"run", "cli-thing-flow", "-quiet"}, code: 3, has: []string{"raise target.timeout"}}})
}

func TestRunOfAFailingChainEndsWithThePinCommand(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	const pin = "pin it: shrt chain pin cli-two-defects (re-runs with -keep-going when needed)"
	out, code := verTShrt(t, "run", "cli-two-defects", "-keep-going", "-var", "tag=T30")
	if lines := strings.Split(strings.TrimSpace(strings.Split(out, "\nERR: ")[0]), "\n"); code != 1 || lines[len(lines)-1] != pin {
		t.Fatalf("a red run ends with the pin command:\n%s", out)
	}
	if _, err := twoDefectSlice(t, "-step", "fetch", "-kept-red=fetch,fetch_again", "-write", ".shrt/chains/cli-two-defects.yaml"); err != nil {
		t.Fatalf("slice: %v", err)
	}
	verTRun(t, []verTCheck{{args: []string{"run", "cli-two-defects", "-keep-going", "-var", "tag=T31"}, code: 0, not: []string{"pin it:"}}})
}

func TestCLIInitBootstrapsAFreshDirectoryWithNoBackendNeeded(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"-build=false"}); err != nil {
			t.Fatalf("shrt init: %v", err)
		}
	})
	for _, want := range []string{".shrt/config.yaml", ".shrt/docs/README.md", ".shrt/docs/GRAMMAR.md", ".shrt/docs/PLAYBOOK.md",
		".shrt/docs/PITFALLS.md", ".claude/skills/shrt/SKILL.md", ".claude/agents/shrt-contract-author.md"} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("shrt init did not create %s: %v", want, err)
		}
	}
}

func TestCLIConfirmProposesAndOnlyAPersonApproves(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	const spot, pending = ".shrt/safespots/cli-thing-flow.json", ".shrt/safespots/pending/cli-thing-flow"
	exists := func(path string, want bool) func(*testing.T, string) {
		return func(t *testing.T, _ string) {
			if _, err := os.Stat(path); (err == nil) != want {
				t.Errorf("%s exists=%v, want %v", path, err == nil, want)
			}
		}
	}
	c := func(args ...string) []string { return append([]string{"confirm", "cli-thing-flow"}, args...) }
	verTRun(t, []verTCheck{
		{args: []string{"run", "cli-thing-flow", "-quiet"}, code: 0},
		{args: c(), code: 1},
		{args: c("-note", "fetch returns the created name"), code: 0, has: []string{"NOT a safe spot yet", "-approve -by <their email>", "2 steps calling"},
			not: []string{"| # | step |"}, then: func(t *testing.T, _ string) {
				exists(spot, false)(t, "")
				report := string(mustRead(t, pending+".md"))
				for _, want := range []string{"fetch returns the created name", "## Steps", "shrt confirm cli-thing-flow -approve", "| # | step | sent | asserted, all held | backend answered |"} {
					if !strings.Contains(report, want) {
						t.Errorf("report lacks %q:\n%s", want, report)
					}
				}
			}},
		{args: c("-approve"), code: 1},
		{args: c("-approve", "-by", "agent"), code: 1, then: exists(spot, false)},
		{args: c("-approve", "-by", "alice@example.test"), code: 0, then: func(t *testing.T, _ string) {
			exists(spot, true)(t, "")
			exists(pending+".json", false)(t, "")
		}},
		{args: c("-note", "again"), code: 1},
		{args: c("-note", "again", "-supersede"), code: 0},
		{args: c("-reject"), code: 0, then: exists(pending+".md", false)},
	})
}
