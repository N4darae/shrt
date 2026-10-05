package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const gateAuthConfig = `auth:
    call: AuthService/Login
    body:
        username: u
        password: p
    token_path: access_token
`

func waitingChain(name, first string) string {
	return `apiVersion: shrt/v1
name: ` + name + `
vars:
    tag: x
steps:
    - id: login
      call: AuthService/Login
      body:
          username: u
          password: p
` + first + `    - id: late
      wait: 4m
      call: ThingService/Fetch
      body:
          id: none-${vars.tag}
`
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw)+content)
}

func gateWithStderr(t *testing.T) (string, string, int) {
	t.Helper()
	var err error
	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() { err = runGate(context.Background(), []string{"-hollow-baseline", ""}) })
	})
	return out, errOut, exitCodeOf(err)
}

func TestAWaitingChainThatOnlyReadsItsOwnTagRunsBesideTheOthers(t *testing.T) {
	f := gateWorkspace(t, nil)
	appendFile(t, ".shrt/config.yaml", gateAuthConfig)
	writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", ""))
	queueDone := make(chan struct{})
	gateExec = func(ctx context.Context, args []string) gateOutcome {
		if args[1] == "a-hold" {
			select {
			case <-queueDone:
			case <-time.After(5 * time.Second):
				return gateOutcome{code: 1, side: gateSidecar{Error: "it waited for the queue in turn"}}
			}
		}
		out := f.exec(ctx, args)
		if args[1] == "cli-unique" {
			close(queueDone)
		}
		return out
	}
	out, errOut, code := gateWithStderr(t)
	if code != 0 || !strings.HasPrefix(out, "PASS a-hold, cli-thing-flow, cli-unique\n") {
		t.Fatalf("a waiting chain that writes nothing and reads only its own tag runs beside the queue, its line in its place, got %d:\n%s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "gate: a-hold waits 4m, started now, beside the other chains; -skip-waits leaves it out") {
		t.Errorf("the gate says at once that a chain waits on purpose:\n%s", errOut)
	}
	if !strings.Contains(out, "time: 0s; slowest: a-hold 0s (waits 4m0s by design, beside the other chains)\n") {
		t.Errorf("the gate ends with what took its time and why:\n%s", out)
	}
}

func TestAWaitingChainGatedAloneNamesNoOtherChains(t *testing.T) {
	f := gateWorkspace(t, nil)
	appendFile(t, ".shrt/config.yaml", gateAuthConfig)
	writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", ""))
	gateExec = func(ctx context.Context, args []string) gateOutcome {
		time.Sleep(200 * time.Millisecond)
		return f.exec(ctx, args)
	}
	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() { _ = runGate(context.Background(), []string{"-hollow-baseline", "", "a-hold"}) })
	})
	if !strings.Contains(errOut, "gate: a-hold waits 4m, started now; -skip-waits leaves it out") || !strings.Contains(errOut, "gate: waiting for a-hold, about 4m0s left") ||
		strings.Contains(errOut+out, "other chains") || !strings.Contains(out, "slowest: a-hold 0s (waits 4m0s by design)\n") {
		t.Errorf("with no other chain sent, no line mentions other chains:\n%s%s", errOut, out)
	}
}

func TestAWaitingChainThatWritesOrReadsSharedStateRunsInTurn(t *testing.T) {
	for _, c := range []struct{ first, why string }{
		{"    - id: make\n      call: ThingService/Create\n      body:\n          name: n-${vars.tag}\n", "make writes"},
		{"    - id: peek\n      call: ThingService/Fetch\n      body:\n          id: shared\n", "peek reads without ${vars.tag}"},
	} {
		f := gateWorkspace(t, nil)
		appendFile(t, ".shrt/config.yaml", gateAuthConfig)
		writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", c.first))
		out, errOut, code := gateWithStderr(t)
		if code != 0 || len(f.calls) == 0 || f.calls[0][1] != "a-hold" {
			t.Fatalf("%s: a waiting chain that %s runs in its turn, got %d, calls %v:\n%s", c.why, c.why, code, f.calls, out)
		}
		if !strings.Contains(errOut, "gate: a-hold waits 4m, in turn ("+c.why+"); -skip-waits leaves it out") {
			t.Errorf("%s: the gate says why it waits in turn:\n%s", c.why, errOut)
		}
		if !strings.Contains(out, "slowest: a-hold 0s (waits 4m0s by design, in turn: "+c.why+")\n") {
			t.Errorf("%s: the time line names the chain and why:\n%s", c.why, out)
		}
	}
}

func TestTheGateTimeLineNamesTheChainsThatTookMostOfIt(t *testing.T) {
	chains := []*gateChain{{name: "fast", took: time.Second}, {name: "slow", took: 50 * time.Second}, {name: "mid", took: 20 * time.Second}}
	if got := gateTime(chains, 20*time.Second); got != "" {
		t.Errorf("a short gate with no waiting chain has no time line, got %q", got)
	}
	if got, want := gateTime(chains, 71*time.Second), "time: 1m11s; slowest: slow 50s, mid 20s"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSkipWaitsLeavesOutAWaitingChainAndNeverCountsItAsPassing(t *testing.T) {
	for _, c := range []struct {
		name     string
		outcomes map[string][]gateOutcome
		code     int
		verdict  string
	}{
		{"the rest passed", nil, 3, "NO VERDICT: 2 of 3 chains passed, 1 skipped"},
		{"another failed", map[string][]gateOutcome{"run cli-unique": {{code: 1, side: gateSidecar{Error: "boom"}}}}, 1, "FAIL: 1 of 3 chains failed, 1 skipped; details: shrt verify <chain> -run latest"},
	} {
		f := gateWorkspace(t, c.outcomes)
		appendFile(t, ".shrt/config.yaml", gateAuthConfig)
		writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", ""))
		_, plain, _ := gateWithStderr(t)
		if !strings.Contains(plain, ", started now, beside the other chains; -skip-waits leaves it out\n") {
			t.Errorf("%s: the start line names the flag that leaves a waiting chain out:\n%s", c.name, plain)
		}
		f.calls = nil
		var err error
		var errOut string
		out := captureStdout(t, func() {
			errOut = captureStderr(t, func() { err = runGate(context.Background(), []string{"-hollow-baseline", "", "-skip-waits"}) })
		})
		if exitCodeOf(err) != c.code || err == nil || !strings.HasSuffix(err.Error(), c.verdict) {
			t.Errorf("%s: a skipped chain never counts as passing, got %d %v", c.name, exitCodeOf(err), err)
		}
		if !strings.Contains(out, "\nSKIPPED a-hold  waits 4m; shrt gate a-hold runs it\n") || strings.Contains(out, "time:") {
			t.Errorf("%s: the skipped chain keeps its place with its wait, and no time line counts it:\n%s", c.name, out)
		}
		if strings.Contains(errOut, "a-hold") {
			t.Errorf("%s: the SKIPPED line alone says which chain it leaves out:\n%s", c.name, errOut)
		}
		for _, call := range f.calls {
			if call[1] == "a-hold" {
				t.Errorf("%s: a skipped chain is not sent: %v", c.name, call)
			}
		}
	}
}

func TestReproLeavesOutAWaitingChainUnlessKeptOrNamed(t *testing.T) {
	f := gateWorkspace(t, nil)
	appendFile(t, ".shrt/config.yaml", gateAuthConfig)
	writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", ""))
	sent := func() bool {
		return slices.ContainsFunc(f.calls, func(call []string) bool { return call[1] == "a-hold" })
	}
	var err error
	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() { err = runGate(context.Background(), []string{"-hollow-baseline", "", "-repro"}) })
	})
	if exitCodeOf(err) != 3 || err == nil || !strings.HasSuffix(err.Error(), "NO VERDICT: 2 of 3 chains passed, 1 skipped") || sent() {
		t.Fatalf("-repro leaves a waiting chain out and never counts it as passing, got %d %v, calls %v:\n%s", exitCodeOf(err), err, f.calls, out)
	}
	if !strings.Contains(out, "\nSKIPPED a-hold  waits 4m; shrt gate a-hold runs it\n") || strings.Contains(errOut, "a-hold") {
		t.Errorf("the skipped chain keeps its place, and the start line names what left it out:\n%s%s", out, errOut)
	}
	for _, args := range [][]string{{"-repro", "-skip-waits=false"}, {"-repro", "a-hold", "cli-unique"}, nil} {
		f.calls = nil
		if out, code := runGateOut(t, args...); code != 0 || !sent() || strings.Contains(out, "SKIPPED") {
			t.Errorf("gate %v sends the waiting chain, got %d, calls %v:\n%s", args, code, f.calls, out)
		}
	}
}

func TestChainLsMarksAChainThatWaitsWithItsTotalWait(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/a-hold.yaml", waitingChain("a-hold", ""))
	var err error
	out := captureStdout(t, func() { err = chainList(nil) })
	if err != nil || !strings.Contains(out, "  W a-hold  2 steps, waits 4m\n") || !strings.Contains(out, "    cli-thing-flow  2 steps\n") ||
		!strings.Contains(out, "W waits by design (gate -repro skips it; shrt gate <chain> runs it)") {
		t.Fatalf("a chain with wait: steps is marked W with its total wait, before any gate starts (%v):\n%s", err, out)
	}
	out = captureStdout(t, func() { err = chainList([]string{"-json"}) })
	if err != nil || !strings.Contains(out, `"waits": "4m"`) {
		t.Fatalf("the JSON row carries the wait (%v):\n%s", err, out)
	}
}
