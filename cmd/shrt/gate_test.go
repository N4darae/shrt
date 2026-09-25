package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeGate struct {
	calls    [][]string
	outcomes map[string][]gateOutcome
	tries    map[string]int
}

func (f *fakeGate) exec(_ context.Context, args []string) gateOutcome {
	f.calls = append(f.calls, args)
	key := args[0]
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		key += " " + args[1]
	}
	n := f.tries[key]
	f.tries[key] = n + 1
	list := f.outcomes[key]
	if len(list) == 0 {
		return gateOutcome{}
	}
	return list[min(n, len(list)-1)]
}

func gateWorkspace(t *testing.T, outcomes map[string][]gateOutcome) *fakeGate {
	t.Helper()
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	writeFile(t, ".shrt/safespots/cli-thing-flow.json", "{}\n")
	f := &fakeGate{outcomes: outcomes, tries: map[string]int{}}
	saved, savedSleep := gateExec, gateSleep
	gateExec, gateSleep = f.exec, func(context.Context, time.Duration) {}
	t.Cleanup(func() { gateExec, gateSleep = saved, savedSleep })
	return f
}

func runGateOut(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		captureStderr(t, func() { err = runGate(context.Background(), append([]string{"-hollow-baseline", ""}, args...)) })
	})
	if errors.Is(err, flag.ErrHelp) {
		t.Fatal("help")
	}
	if err != nil {
		out += "shrt gate: " + err.Error() + "\n"
	}
	return out, exitCodeOf(err)
}

func TestTheGateRunsEveryChainAndVerifiesEverySafeSpotWithAFreshTag(t *testing.T) {
	f := gateWorkspace(t, nil)
	out, code := runGateOut(t)
	if code != 0 || !strings.Contains(out, "PASS       cli-thing-flow") || !strings.Contains(out, "PASS       cli-unique") ||
		!strings.Contains(out, "gate: PASS: 2 chain(s)") {
		t.Fatalf("a green gate prints PASS per chain and exits 0, got %d:\n%s", code, out)
	}
	got := []string{}
	tags := map[string]bool{}
	for _, c := range f.calls {
		got = append(got, strings.Join(c[:2], " "))
		for i, a := range c {
			if a == "-var" {
				tags[c[i+1]] = true
			}
		}
		if c[1] == "cli-thing-flow" && strings.Contains(strings.Join(c, " "), "-var") {
			t.Errorf("a chain that reads no tag gets none: %v", c)
		}
	}
	if strings.Join(got, ",") != "run cli-thing-flow,verify cli-thing-flow,run cli-unique" {
		t.Fatalf("each chain runs, and each safe spot is verified: %v", got)
	}
	if len(tags) != 1 {
		t.Fatalf("the chain that reads tag gets one: %v", tags)
	}
}

func TestTheGateRetriesAnExit3OnceWithAFreshTag(t *testing.T) {
	f := gateWorkspace(t, map[string][]gateOutcome{"run cli-unique": {{code: 3}, {code: 0}}})
	out, code := runGateOut(t)
	if code != 0 || f.tries["run cli-unique"] != 2 {
		t.Fatalf("an exit 3 then 0 passes after one retry, got %d after %d tries:\n%s", code, f.tries["run cli-unique"], out)
	}
	tags := []string{}
	for _, c := range f.calls {
		if c[1] == "cli-unique" {
			tags = append(tags, c[len(c)-1])
		}
	}
	if len(tags) != 2 || tags[0] == tags[1] {
		t.Fatalf("each attempt gets its own tag: %v", tags)
	}
}

func TestTheGateSaysNoVerdictWhenTheRetryExits3Too(t *testing.T) {
	f := gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 3, stderr: "shrt verify: could not verify cli-thing-flow: down\n"}}})
	out, code := runGateOut(t)
	if code != 3 || f.tries["verify cli-thing-flow"] != 2 || !strings.Contains(out, "NO VERDICT cli-thing-flow  verify: could not verify cli-thing-flow: down") {
		t.Fatalf("exit 3 twice is no verdict, exit 3, got %d:\n%s", code, out)
	}
}

func TestTheGateDoesNotRetryAFailureAndGroupsItsCauses(t *testing.T) {
	item := func(step string) gateItem {
		return gateItem{Step: step, Call: "shrt.test.v1.ThingService/Create", Path: "items.0.price", Want: "250", Got: "249"}
	}
	f := gateWorkspace(t, map[string][]gateOutcome{
		"run cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{item("create"), item("create_2")}}}},
		"run cli-unique":     {{code: 1, side: gateSidecar{Items: []gateItem{item("create"), {Step: "fetch", Call: "shrt.test.v1.ThingService/Fetch", Path: "name", Want: "a", Got: "b"}}}}},
		"verify cli-thing-flow": {{code: 1, stdout: "cli-thing-flow: DRIFT\nREGRESSION: something\n",
			side: gateSidecar{Items: []gateItem{item("create")}}}},
	})
	out, code := runGateOut(t)
	if code != 1 || f.tries["run cli-thing-flow"] != 1 {
		t.Fatalf("a failure fails the gate at once, exit 1, got %d:\n%s", code, out)
	}
	for _, want := range []string{
		"FAIL       cli-thing-flow  create (ThingService/Create) items.0.price want=250 got=249",
		"  REGRESSION: something",
		"  ThingService/Create items[].price: 3 step(s) in 2 chain(s), e.g. cli-thing-flow create want=250 got=249",
		"  ThingService/Fetch name: 1 step(s) in 1 chain(s)",
		"FAIL: 2 of 2 chain(s) failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "ThingService/Create items[]") > strings.Index(out, "ThingService/Fetch name") {
		t.Errorf("the most widespread group comes first:\n%s", out)
	}
}

func TestTheGateFailsWhenOneProfilesTokensAreRefusedEarlyInTwoRuns(t *testing.T) {
	early := func(p string) []gateOutcome { return []gateOutcome{{side: gateSidecar{EarlyProfile: p}}} }
	gateWorkspace(t, map[string][]gateOutcome{"run cli-thing-flow": early("default")})
	out, code := runGateOut(t)
	if code != 0 || !strings.Contains(out, "note: a token of auth profile default was refused early once") {
		t.Fatalf("one early refusal is a note, exit 0; got %d:\n%s", code, out)
	}
	gateWorkspace(t, map[string][]gateOutcome{"run cli-thing-flow": early("default"), "run cli-unique": early("default")})
	out, code = runGateOut(t)
	if code != 1 || !strings.Contains(out, "FINDING: tokens of auth profile default were refused early in 2 runs of this gate") {
		t.Fatalf("the same profile's tokens refused early twice fail the gate; got %d:\n%s", code, out)
	}
	gateWorkspace(t, map[string][]gateOutcome{"run cli-thing-flow": early("default"), "run cli-unique": early("clerk")})
	if out, code = runGateOut(t); code != 0 {
		t.Fatalf("one early refusal per profile is what a single restart explains; got %d:\n%s", code, out)
	}
}

func TestTheGateKeepsKeptRedAndChecksTheHollowRatchet(t *testing.T) {
	f := gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{side: gateSidecar{KeptRed: "as_pinned"}}},
		"chain hollow":   {{code: 1, stderr: "hollow: 2 reported, baseline 0\n"}},
	})
	var err error
	out := captureStdout(t, func() { err = runGate(context.Background(), nil) })
	if !strings.Contains(out, "KEPT RED   cli-unique") || exitCodeOf(err) != 1 || !strings.Contains(out, "hollow ratchet: hollow: 2 reported, baseline 0") {
		t.Fatalf("kept red is its own verdict, and a ratchet failure fails the gate: %v\n%s", err, out)
	}
	if last := f.calls[len(f.calls)-1]; strings.Join(last, " ") != "chain hollow -gate -baseline .shrt/hollow-baseline" {
		t.Fatalf("the ratchet runs last against the baseline file: %v", last)
	}
}

func TestTheGateReadsWhatARealRunAndVerifyReport(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	saved := gateExec
	t.Cleanup(func() { gateExec = saved })
	gateExec = func(ctx context.Context, args []string) gateOutcome {
		side := t.TempDir() + "/side.json"
		t.Setenv(gateReportEnv, side)
		var err error
		out := captureStdout(t, func() { err = commands[args[0]].run(ctx, args[1:]) })
		o := gateOutcome{stdout: out, code: exitCodeOf(err)}
		if err != nil {
			o.stderr = "shrt " + args[0] + ": " + err.Error()
		}
		if raw, rerr := os.ReadFile(side); rerr == nil {
			_ = json.Unmarshal(raw, &o.side)
		}
		return o
	}
	out, code := runGateOut(t)
	if code != 0 || !strings.Contains(out, "PASS       cli-thing-flow") {
		t.Fatalf("green, got %d:\n%s", code, out)
	}
	name = "gadget"
	out, code = runGateOut(t)
	if code != 1 || !strings.Contains(out, "FAIL       cli-thing-flow  fetch (ThingService/Fetch) name want=widget got=gadget") ||
		!strings.Contains(out, "ThingService/Fetch name: 1 step(s) in 1 chain(s)") {
		t.Fatalf("a changed name fails the gate and is grouped, got %d:\n%s", code, out)
	}
}
