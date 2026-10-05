package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	coredistillation "github.com/N4darae/shrt"
	"github.com/N4darae/shrt/agentkit"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
)

func adoptedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Root = root
	cfg.Descriptor.Source = ""
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := agentkit.Install(root, agentkit.DocAssets(), true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, cfg.Descriptor.File), "descriptor")
	writeFile(t, filepath.Join(root, ".gitignore"), strings.Join(cfg.NeverCommit(), "\n")+"\n")
	t.Chdir(root)
	return root
}

func cliDoctor(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runDoctor(context.Background(), args) })
	var coded *exitError
	if err != nil && !errors.As(err, &coded) {
		t.Fatalf("shrt doctor returned %v, not an exitError", err)
	}
	return out, exitCodeOf(err)
}

func TestDoctorFailsOnDriftedDocsOrKitAndOnWarningsOnlyUnderStrict(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, root string)
		args  []string
		code  int
		want  []string
		not   []string
	}{
		{name: "freshly installed, no kit", not: []string{"FAIL  agentkit", "ok   "}},
		{name: "-v lists every ok check", args: []string{"-v"}, want: []string{"ok   build: ", "ok   docs: ", "\n\n"}},
		{name: "json", args: []string{"-json"}, want: []string{`"Findings"`, `"Check"`, `"Level"`, `"Detail"`, `"ok"`}},
		{name: "matching kit", args: []string{"-v"}, want: []string{"agent kit"}, not: []string{"FAIL  agentkit"},
			setup: func(t *testing.T, root string) {
				if _, err := agentkit.Install(root, agentkit.ClaudeAssets(), true); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "drifted kit", code: 1, want: []string{".claude/skills/shrt/SKILL.md", "drifted"},
			setup: func(t *testing.T, root string) {
				if _, err := agentkit.Install(root, agentkit.ClaudeAssets(), true); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(root, ".claude", "skills", "shrt", "SKILL.md"), "a skill from an older binary\n")
			}},
		{name: "drifted docs", code: 1,
			setup: func(t *testing.T, root string) {
				installed := filepath.Join(root, config.DocsDir, coredistillation.DocNames[0])
				writeFile(t, installed, string(mustRead(t, installed))+"\na rule from an older binary\n")
			}},
		{name: "a warning", want: []string{"WARN gitignore: ", "\n\n"}, not: []string{"ok   "}, setup: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".gitignore"), ".shrt/tokens.json\n")
		}},
		{name: "a warning under -strict", args: []string{"-strict"}, code: 1,
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, ".gitignore"), ".shrt/tokens.json\n")
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := adoptedRepo(t)
			if c.setup != nil {
				c.setup(t, root)
			}
			out, code := cliDoctor(t, c.args...)
			if code != c.code {
				t.Fatalf("exit %d, want %d:\n%s", code, c.code, out)
			}
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("want %q in:\n%s", want, out)
				}
			}
			for _, not := range c.not {
				if strings.Contains(out, not) {
					t.Errorf("want no %q in:\n%s", not, out)
				}
			}
		})
	}
}

func TestDoctorStatesItsCountsOnceWhenItFails(t *testing.T) {
	root := adoptedRepo(t)
	writeFile(t, filepath.Join(root, ".gitignore"), ".shrt/tokens.json\n")
	for _, c := range []struct {
		args  []string
		shown bool
	}{{[]string{"-strict"}, true}, {[]string{"-strict", "-json"}, false}} {
		var err error
		out := captureStdout(t, func() { err = runDoctor(context.Background(), c.args) })
		var shown shownError
		if exitCodeOf(err) != 1 || errors.As(err, &shown) != c.shown {
			t.Errorf("%v: exit 1, and the error is printed only when the text summary did not already say it: %v\n%s", c.args, err, out)
		}
	}
}

func TestAMisspeltChainNameGetsADidYouMean(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	ctx := context.Background()
	const typo = "cli-thing-flwo"
	for name, call := range map[string]func() error{
		"run":     func() error { return runRun(ctx, []string{typo}) },
		"verify":  func() error { return runVerify(ctx, []string{typo}) },
		"slice":   func() error { return chainSlice(ctx, []string{typo, "-step", "create"}) },
		"confirm": func() error { return runConfirm(ctx, []string{typo, "-note", "checked"}) },
		"diff":    func() error { return runDiff(ctx, []string{typo}) },
	} {
		var err error
		captureStdout(t, func() { err = call() })
		if err == nil || !strings.Contains(err.Error(), `did you mean "cli-thing-flow"?`) {
			t.Errorf("%s %s: want a did-you-mean naming cli-thing-flow, got %v", name, typo, err)
		}
	}
}

func TestAChainNamedOtherwiseThanItsFileIsVerifiedAgainstItsOwnName(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	verify := func(want string, wantErr bool) {
		t.Helper()
		for _, ref := range []string{"cli-thing-flow", "thing-new"} {
			var err error
			out := captureStdout(t, func() { err = runVerify(ctx, []string{ref, "-quiet"}) })
			if (err != nil) != wantErr || !strings.Contains(out+fmt.Sprint(err), want) {
				t.Errorf("verify %s: want %q: %v\n%s", ref, want, err, out)
			}
		}
	}
	fixApprove(t, "cli-thing-flow")
	cliEdit(t, cliFlow, "name: cli-thing-flow\n", "name: thing-new\n")
	verify("thing-new has no safe spot", true)
	fixApprove(t, "cli-thing-flow")
	if _, err := os.Stat(".shrt/safespots/thing-new.json"); err != nil {
		t.Fatalf("confirming by the file name writes the safe spot of the chain's name: %v", err)
	}
	verify("thing-new: no drift", false)
	var err error
	out := captureStdout(t, func() { err = chainGroup.run(ctx, []string{"lint", "cli-thing-flow"}) })
	if err != nil || !strings.Contains(out, "declares name: thing-new") || !strings.Contains(out, "rename the file to thing-new.yaml") {
		t.Errorf("lint warns about the mismatch and passes: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = chainGroup.run(ctx, []string{"ls"}) })
	if err != nil || !regexp.MustCompile(`\*\s+thing-new`).MatchString(out) || !strings.Contains(out, "file cli-thing-flow.yaml") {
		t.Errorf("chain ls lists thing-new with its safe spot and notes its file: %v\n%s", err, out)
	}
}

func TestTwoChainFilesClaimingOneNameAreRefused(t *testing.T) {
	approvedThingFlow(t)
	raw := string(mustRead(t, cliFlow))
	writeFile(t, ".shrt/chains/scratchy.yaml", strings.Replace(raw, "name: widget\n", "name: gadget\n", 1))
	before, _ := os.ReadDir(".shrt/runs/cli-thing-flow")
	ctx := context.Background()
	for _, run := range []func() error{
		func() error { return runRun(ctx, []string{"scratchy", "-quiet"}) },
		func() error { return runRun(ctx, []string{"cli-thing-flow", "-quiet"}) },
		func() error { return runVerify(ctx, []string{"scratchy", "-quiet"}) },
		func() error { return runConfirm(ctx, []string{"cli-thing-flow", "-supersede", "-note", "checked"}) },
	} {
		var err error
		out := captureStdout(t, func() { err = run() })
		if err == nil || !strings.Contains(err.Error(), `2 chain files declare name "cli-thing-flow"`) ||
			!strings.Contains(err.Error(), ".shrt/chains/scratchy.yaml") || !strings.Contains(err.Error(), cliFlow) {
			t.Errorf("refused naming both files: %v\n%s", err, out)
		}
	}
	if after, _ := os.ReadDir(".shrt/runs/cli-thing-flow"); len(after) != len(before) {
		t.Fatalf("nothing is recorded while two files claim one name: %d run(s) before, %d after", len(before), len(after))
	}
	var err error
	captureStdout(t, func() { err = runConfirm(ctx, []string{"cli-thing-flow", "-reject"}) })
	if err != nil && strings.Contains(err.Error(), "declare name") {
		t.Fatalf("-reject still works while the names clash, got %v", err)
	}
	os.Remove(".shrt/chains/scratchy.yaml")
	writeFile(t, ".shrt/chains/sw1.yaml", strings.Replace(raw, "name: cli-thing-flow\n", "name: sw2\n", 1))
	writeFile(t, ".shrt/chains/sw2.yaml", strings.Replace(raw, "name: cli-thing-flow\n", "name: sw1\n", 1))
	for _, ref := range []string{"sw1", "sw2"} {
		out := captureStdout(t, func() { err = runRun(ctx, []string{ref, "-quiet"}) })
		if err == nil || !strings.Contains(err.Error(), `"`+ref+`" names two different chains`) {
			t.Errorf("run %s is ambiguous and refused: %v\n%s", ref, err, out)
		}
		out = captureStdout(t, func() { err = runVerify(ctx, []string{ref, "-quiet"}) })
		if err == nil || !strings.Contains(err.Error(), "names two different chains") {
			t.Errorf("verify %s is refused the same way: %v\n%s", ref, err, out)
		}
	}
	out := captureStdout(t, func() { _ = chainGroup.run(ctx, []string{"lint", "sw1"}) })
	if strings.Contains(out, "verify the same chain") || !strings.Contains(out, "declares name: sw2, while sw2.yaml is chain sw1") {
		t.Errorf("lint does not call two different chains one chain:\n%s", out)
	}
}

func TestRunRefusesABodyTheProtoRejectsBeforeSendingIt(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-bogus.yaml", "apiVersion: shrt/v1\nname: cli-bogus\nsteps:\n    - id: create\n      call: ThingService/Create\n      body:\n          name: widget\n          kind: KIND_BOGUS\n      expect:\n          - path: error.code\n            equals: OK\n")
	writeFile(t, ".shrt/chains/cli-qty.yaml", `apiVersion: shrt/v1
name: cli-qty
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
      export:
          thing_id: id
    - id: again
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          qty: ${thing_id}
      expect:
          - path: error.code
            equals: OK
`)
	ctx := context.Background()
	for _, args := range [][]string{{"cli-bogus", "-quiet"}, {"cli-bogus", "-quiet", "-dry-run"}, {"cli-qty", "-quiet", "-dry-run"}, {"cli-qty", "-quiet"}} {
		var err error
		out := captureStdout(t, func() { err = runRun(ctx, args) })
		if exitCodeOf(err) != 1 || strings.Contains(out, "again sent") || (args[0] == "cli-bogus" && !strings.Contains(err.Error(), "nothing was sent")) {
			t.Errorf("%v: a body the proto rejects is refused unsent, exit 1: %v\n%s", args, err, out)
		}
	}
	out := captureStdout(t, func() { _ = runRun(ctx, []string{"cli-qty", "-dry-run"}) })
	if !strings.Contains(out, "${thing_id} fills qty, declared int64") || strings.Contains(out, `qty: ""`) {
		t.Errorf("a dry run names the reference and the field types:\n%s", out)
	}
}

func TestProgressColumnsAlignForALongStepID(t *testing.T) {
	srv := newFakeCLIBackend()
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	long := "create_the_first_widget_of_many"
	writeFile(t, ".shrt/chains/cli-long-ids.yaml", "apiVersion: shrt/v1\nname: cli-long-ids\nsteps:\n    - id: "+long+
		"\n      call: ThingService/Create\n      body:\n          name: widget\n          kind: KIND_A\n    - id: fetch\n      call: ThingService/Fetch\n      body:\n          id: ${"+long+".id}\n")
	out := captureStdout(t, func() { _ = runRun(context.Background(), []string{"cli-long-ids"}) })
	cols := []int{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 5 && (f[2] == long || f[2] == "fetch") {
			cols = append(cols, strings.LastIndex(line, " "+f[3]+" "))
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("the call column starts at the same place on every progress line, got %v:\n%s", cols, out)
	}
}

func TestAnUnapprovedRedactThatBlankedNothingSaysSo(t *testing.T) {
	confirmedThingFlow(t)
	cliEdit(t, cliFlow, "name: gadget\n", "name: widget\n")
	writeFile(t, ".shrt/config.yaml", string(mustRead(t, ".shrt/config.yaml"))+"redact:\n    - '**.nickname'\n")
	var err error
	out := captureStdout(t, func() { err = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"}) })
	if err == nil || strings.Contains(err.Error(), "the value(s) they blanked were not compared") || !strings.Contains(err.Error(), "hid nothing this run") ||
		!strings.Contains(out, "hid nothing this run") {
		t.Fatalf("an unapproved redact still fails verify, saying it hid nothing: %v\n%s", err, out)
	}
}

func TestCouldNotVerifyGivesTheRemedyForWhatWentUnanswered(t *testing.T) {
	rec := &runner.Record{Chain: "c", Steps: []*runner.StepRecord{{ID: "create", Status: runner.StatusError}}}
	for _, c := range []struct{ why, want, not string }{
		{"POST http://127.0.0.1:1/x: sent, no answer before target.timeout (700ms): context deadline exceeded", "raise target.timeout", "start or reach the target"},
		{"auth login rejected: http_502: <html><body>502 Bad Gateway</body></html>", "wait until it is up", "credentials"},
		{"POST http://x/Create: the backend closed the connection before a response arrived (EOF): it most likely stopped", "check the backend is up", "credentials"},
		{"auth login rejected: http_401: unauthenticated: bad password", "fix the credentials", "wait until it is up"},
	} {
		err := couldNotVerifyAfter("c", "create", c.why, rec, nil)
		if !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), c.not) {
			t.Errorf("%s: want %q and no %q: %v", c.why, c.want, c.not, err)
		}
	}
}

func TestVerifyAGatewayUnavailableIsCouldNotVerify(t *testing.T) {
	var down atomic.Bool
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/shrt.test.v1.ThingService/Fetch" && down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"unavailable","message":"upstream connect error"}`))
			return
		}
		next++
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next), "name": body["name"]})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	fixApprove(t, "cli-unique")
	down.Store(true)
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-unique", "-quiet", "-var", "tag=gw1"}) })
	if exitCodeOf(err) != 3 || strings.Contains(err.Error(), "regression") {
		t.Fatalf("an unavailable answer from a gateway is could-not-verify, exit 3, got %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=gw2"}) })
	if exitCodeOf(err) != 3 {
		t.Fatalf("run: an unavailable answer is error, exit 3, got %v\n%s", err, out)
	}
}

func TestAnUnreadVarIsAWarningAndATypoOfARealOneIsRefused(t *testing.T) {
	if msg := unusedVarError([]string{"tag"}, "probe", nil).Error(); strings.HasSuffix(msg, "vars this chain reads: ") ||
		!strings.Contains(msg, "reads no vars") || !strings.Contains(msg, "-var tag") {
		t.Fatalf("an empty list of read vars is worded: %q", msg)
	}
	if msg := unusedVarError([]string{"tagg"}, "probe", []string{"run_tag", "sku"}).Error(); !strings.Contains(msg, "vars this chain reads: run_tag, sku") {
		t.Fatalf("got %q", msg)
	}
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	writeFile(t, ".shrt/chains/cli-plain.yaml", strings.NewReplacer("widget ${vars.tag}", "plain widget", "vars:\n    tag: first\n", "", "name: cli-unique", "name: cli-plain").Replace(uniqueNameChain))
	ctx := context.Background()
	var err error
	run := func(args ...string) string {
		return captureStderr(t, func() { captureStdout(t, func() { err = runRun(ctx, append(args, "-quiet")) }) })
	}
	if stderr := run("cli-plain", "-var", "tag=loop1"); err != nil || stderr != "" {
		t.Fatalf("a chain that reads no vars takes any -var silently: %v %q", err, stderr)
	}
	if stderr := run("cli-unique", "-var", "zzz=loop1"); err != nil || !strings.Contains(stderr, `warning: -var zzz: chain "cli-unique" never reads it (it reads tag)`) {
		t.Fatalf("an unread var of a chain that reads others is named in a warning: %v %q", err, stderr)
	}
	run("cli-unique", "-var", "tga=loop1")
	if err == nil || !strings.Contains(err.Error(), "looks mistyped") || !strings.Contains(err.Error(), "(tag)") {
		t.Fatalf("a name one edit from a var the chain reads is refused: %v", err)
	}
}

func TestVarFlagKeepsTheTypeTheProtoFieldExpects(t *testing.T) {
	for in, want := range map[string]any{
		"draft=true": true, "draft=false": false, "page_size=7": int64(7), "rate=1.5": 1.5, "business_date=2026-01-01": "2026-01-01",
		"name=true story": "true story", "code=007": "007", "amount=1.50": "1.50", "empty=": "",
	} {
		v := varFlags{}
		if err := v.Set(in); err != nil {
			t.Fatalf("-var %s: %v", in, err)
		}
		for _, got := range v {
			if got != want {
				t.Errorf("-var %s gave %#v, want %#v", in, got, want)
			}
		}
	}
	if err := (varFlags{}).Set("no-equals-sign"); err == nil {
		t.Fatal("-var with no = was accepted")
	}
}

const fakeShrt = `#!/usr/bin/env bash
n=0
[ -f "$FAKE_DIR/count-$1" ] && n=$(cat "$FAKE_DIR/count-$1")
n=$((n+1))
echo "$n" > "$FAKE_DIR/count-$1"
echo "$*" >> "$FAKE_DIR/calls"
codes="FAKE_$(echo "$1" | tr a-z A-Z)"
code=$(echo "${!codes:-0}" | cut -d, -f"$n")
said="FAKE_SAY_$(echo "$1" | tr a-z A-Z)"
[ -n "${!said:-}" ] && printf '%s\n' "${!said}"
exit "${code:-0}"
`

func TestTheReadmeGateRunsTheStaticChecksThenExitsAsShrtGateDoes(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	_, section, _ := strings.Cut(string(mustRead(t, "../../README.md")), "### CI gate")
	_, block, _ := strings.Cut(section, "```bash\n")
	script, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("README has no closed bash block under ### CI gate")
	}
	gate := func(overlay bool, env ...string) (int, string) {
		dir := t.TempDir()
		bin, work := filepath.Join(dir, "bin"), filepath.Join(dir, "work")
		writeFile(t, filepath.Join(work, ".shrt", "docs", "GRAMMAR.md"), "grammar\n")
		writeFile(t, filepath.Join(work, ".shrt", "chains", "flow.yaml"), "name: flow\nvars:\n  tag: t\n")
		writeFile(t, filepath.Join(work, ".shrt", "safespots", "flow.json"), "{}\n")
		writeFile(t, filepath.Join(work, "gate.sh"), script)
		if overlay {
			writeFile(t, filepath.Join(work, ".shrt", "contracts", "items.yaml"), "domain: items\n")
		}
		for name, body := range map[string]string{"shrt": fakeShrt, "git": "#!/usr/bin/env bash\npwd\n", "sleep": "#!/usr/bin/env bash\necho \"$*\" >> \"$FAKE_DIR/slept\"\n"} {
			writeFile(t, filepath.Join(bin, name), body)
			if err := os.Chmod(filepath.Join(bin, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("bash", "gate.sh")
		cmd.Dir = work
		cmd.Env = append(append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_DIR="+dir), env...)
		_, _ = cmd.CombinedOutput()
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		return cmd.ProcessState.ExitCode(), string(calls)
	}
	if code, calls := gate(true); code != 0 || calls != "catalog build\ndoctor -strict\ncontract lint\ncontract quality -gate -baseline .shrt/quality-baseline\nchain lint -strict\ngate\n" {
		t.Fatalf("a green wrapper runs the static checks in order, then shrt gate, and exits 0: %d\n%s", code, calls)
	}
	for _, code := range []int{1, 3} {
		if got, _ := gate(true, "FAKE_GATE="+strconv.Itoa(code)); got != code {
			t.Fatalf("shrt gate exiting %d exits the wrapper %d, got %d", code, code, got)
		}
	}
	if got, calls := gate(true, "FAKE_DOCTOR=1"); got != 1 || strings.Contains(calls, "gate") {
		t.Fatalf("a failed static check stops the wrapper before the gate: exit %d\n%s", got, calls)
	}
	if code, calls := gate(false, "FAKE_CONTRACT=1"); code != 0 || strings.Contains(calls, "contract") || !strings.Contains(calls, "chain lint -strict\ngate\n") {
		t.Fatalf("without an overlay the wrapper skips the contract checks: exit %d\n%s", code, calls)
	}
	if code, _ := gate(true, "FAKE_CONTRACT=1"); code != 1 {
		t.Fatalf("with an overlay, a contract error fails the wrapper, got %d", code)
	}
}
