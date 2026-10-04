package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
)

type fakeGate struct {
	mu       sync.Mutex
	calls    [][]string
	outcomes map[string][]gateOutcome
	tries    map[string]int
}

func (f *fakeGate) exec(_ context.Context, args []string) gateOutcome {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	if strings.Join(got, ",") != "verify cli-thing-flow,run cli-unique" {
		t.Fatalf("a chain with a safe spot is sent once, by verify, and one without runs: %v", got)
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
	for _, tag := range tags {
		if v := strings.TrimPrefix(tag, "tag="); len(v) != len(chain.NewRunTag()) || v[0] != 't' {
			t.Errorf("the gate's tag has run's shape, so a length limit cannot fail only the gate: %q", tag)
		}
	}
}

func TestTheGateSaysNoVerdictWhenTheRetryExits3Too(t *testing.T) {
	f := gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": {{code: 3, side: gateSidecar{Error: "could not verify cli-thing-flow: down"}}}})
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
		"run cli-unique": {{code: 1, side: gateSidecar{Sent: map[string]string{"create": " sent {\"name\":\"x\"}"}, Items: []gateItem{item("create"),
			{Step: "fetch", Call: "shrt.test.v1.ThingService/Fetch", Path: "name", Want: "a", Got: "b"},
			{Step: "fetch_2", Call: "shrt.test.v1.ThingService/Fetch", Path: "count", Rule: "not_equal", Want: "0", Got: "0", Reason: reason{Kind: reasonWrite, Step: "create", RPC: "shrt.test.v1.ThingService/Create"}}}}}},
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Notes: []string{"REGRESSION: something"}, Items: []gateItem{item("create"), item("create_2")}}}},
	})
	out, code := runGateOut(t)
	if code != 1 || f.tries["verify cli-thing-flow"] != 1 || f.tries["run cli-thing-flow"] != 0 {
		t.Fatalf("a failure fails the gate at once, exit 1, got %d:\n%s", code, out)
	}
	for _, want := range []string{
		"FAIL       cli-thing-flow  create (ThingService/Create) items.0.price want=250 got=249",
		"  REGRESSION: something",
		"FAIL       cli-unique      fetch_2 (ThingService/Fetch) count want≠0 got=0; suspect write create (ThingService/Create)\n",
		"  ThingService/Create items[].price, count: 4 step(s) in 2 chain(s); e.g. cli-unique fetch_2; write create\n",
		"  ThingService/Fetch name: 1 step(s) in 1 chain(s); e.g. cli-unique fetch name want=a got=b\n",
		"FAIL: 2 of 2 chain(s) failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestTheGateFailsWhenOneProfilesTokensAreRefusedEarlyInTwoRuns(t *testing.T) {
	early := func(p string) []gateOutcome { return []gateOutcome{{side: gateSidecar{EarlyProfile: p}}} }
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": early("default")})
	out, code := runGateOut(t)
	if code != 0 || !strings.Contains(out, "note: a token of auth profile default was refused early once") {
		t.Fatalf("one early refusal is a note, exit 0; got %d:\n%s", code, out)
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": early("default"), "run cli-unique": early("default")})
	out, code = runGateOut(t)
	if code != 1 || !strings.Contains(out, "FINDING: tokens of auth profile default were refused early in 2 runs of this gate") {
		t.Fatalf("the same profile's tokens refused early twice fail the gate; got %d:\n%s", code, out)
	}
	gateWorkspace(t, map[string][]gateOutcome{"verify cli-thing-flow": early("default"), "run cli-unique": early("clerk")})
	if out, code = runGateOut(t); code != 0 || strings.Count(out, "note: ") != 1 || !strings.Contains(out, "auth profiles clerk, default was refused early once") {
		t.Fatalf("one early refusal per profile is what a single restart explains, said once; got %d:\n%s", code, out)
	}
}

func TestTheGateKeepsKeptRedAndChecksTheHollowRatchet(t *testing.T) {
	f := gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{side: gateSidecar{KeptRed: "as_pinned"}}},
		"chain hollow":   {{code: 1, stderr: "shrt chain: hollow: 2 reported, worse than the baseline 0.\nRun 'shrt chain hollow' to see which.\n"}},
	})
	writeFile(t, ".shrt/hollow-baseline", "0\n")
	var err error
	out := captureStdout(t, func() { err = runGate(context.Background(), nil) })
	if !strings.Contains(out, "KEPT RED   cli-unique") || exitCodeOf(err) != 1 || !strings.Contains(out, "hollow ratchet: 2 reported, worse than the baseline 0.\n") {
		t.Fatalf("kept red is its own verdict, and a ratchet failure fails the gate with the error's first line: %v\n%s", err, out)
	}
	if last := f.calls[len(f.calls)-1]; strings.Join(last, " ") != "chain hollow -gate -baseline .shrt/hollow-baseline" {
		t.Fatalf("the ratchet runs last against the baseline file: %v", last)
	}
}

func TestTheGateWritesAMissingHollowBaseline(t *testing.T) {
	t.Setenv("CI", "")
	gateWorkspace(t, map[string][]gateOutcome{"chain hollow": {{stdout: `{"reported": 2}`}}})
	var err error
	out := captureStdout(t, func() { err = runGate(context.Background(), nil) })
	raw, _ := os.ReadFile(".shrt/hollow-baseline")
	if err != nil || string(raw) != "2\n" || !strings.Contains(out, "hollow ratchet: .shrt/hollow-baseline did not exist; wrote today's count, 2, to it") {
		t.Fatalf("a missing baseline is written with today's count on the first gate: %v %q\n%s", err, raw, out)
	}
}

func TestTheGateInCIFailsOnAMissingHollowBaselineInsteadOfWritingIt(t *testing.T) {
	t.Setenv("CI", "true")
	gateWorkspace(t, map[string][]gateOutcome{"chain hollow": {{stdout: `{"reported": 2}`}}})
	var err error
	out := captureStdout(t, func() { err = runGate(context.Background(), nil) })
	if _, statErr := os.Stat(".shrt/hollow-baseline"); exitCodeOf(err) != 1 || statErr == nil || !strings.Contains(out, ".shrt/hollow-baseline is missing") {
		t.Fatalf("a CI gate never sets its own ratchet: %v\n%s", err, out)
	}
}

func TestTheGateReadsWhatARealRunAndVerifyReport(t *testing.T) {
	name, extra := "widget", false
	driftWorkspace(t, &name, &extra)
	inProcessGate(t)
	out, code := runGateOut(t)
	if code != 0 || !strings.Contains(out, "PASS       cli-thing-flow") {
		t.Fatalf("green, got %d:\n%s", code, out)
	}
	name = "gadget"
	out, code = runGateOut(t)
	if code != 1 || !strings.Contains(out, "FAIL       cli-thing-flow  regression: fetch (ThingService/Fetch) name want=widget got=gadget") ||
		!strings.Contains(out, "got=gadget; suspect write create (ThingService/Create)\n") ||
		!strings.Contains(out, "  ThingService/Create name: 1 step(s) in 1 chain(s); e.g. cli-thing-flow fetch; write create\n") {
		t.Fatalf("a changed name fails the gate and is grouped, got %d:\n%s", code, out)
	}
}

func TestTheGateFailLineSaysWhatVerifyCallsItAndEachNoteOnce(t *testing.T) {
	item := gateItem{Step: "fetch", Call: "shrt.test.v1.ThingService/Fetch", Path: "items.0.id", Want: "a", Got: "b"}
	classed := item
	classed.Class = "order changed"
	latency := func(ms int64) []diff.LatencyFlag {
		return []diff.LatencyFlag{{Step: "fetch", Call: item.Call, AfterMS: ms, Confirmed: true}}
	}
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-thing-flow":    {{code: 1, side: gateSidecar{Latency: latency(701), Items: []gateItem{item}}}},
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Latency: latency(702), RunToo: true, Items: []gateItem{classed}}}},
	})
	out, _ := runGateOut(t)
	if !strings.Contains(out, "FAIL       cli-thing-flow  order changed: fetch (ThingService/Fetch) items.0.id want=a got=b") {
		t.Errorf("the FAIL line carries verify's class of its first failure:\n%s", out)
	}
	if n := strings.Count(out, "LATENCY: Fetch at step fetch"); n != 1 {
		t.Errorf("run and verify flag the same slow step: one line, got %d:\n%s", n, out)
	}
}

func TestATruncatedWantAndGotKeepWhereTheyFirstDiffer(t *testing.T) {
	want := strings.Repeat("x", 80) + "-LEFT-" + strings.Repeat("y", 20)
	got := strings.Repeat("x", 80) + "-RIGHT-" + strings.Repeat("y", 20)
	w, g := capPair(want, got, 60)
	if !strings.Contains(w, "LEFT") || !strings.Contains(g, "RIGHT") || len(w) > 60 || len(g) > 60 {
		t.Errorf("the first difference stays visible within the cap: %q %q", w, g)
	}
	if w, _ := capPair("short", "other", 60); w != "short" {
		t.Errorf("short values are kept whole: %q", w)
	}
}

func TestTheGateNamesASlowRpcAsItsOwnSuspect(t *testing.T) {
	flags := []diff.LatencyFlag{
		{Step: "list", Call: "shrt.test.v1.ThingService/List", BeforeMS: 3, AfterMS: 701, Confirmed: true},
		{Step: "list_2", Call: "shrt.test.v1.ThingService/List", BeforeMS: 1, AfterMS: 702, Confirmed: true},
		{Step: "create", Call: "shrt.test.v1.ThingService/Create", BeforeMS: 1, AfterMS: 400},
	}
	items := latencyItems(flags)
	if len(items) != 2 || items[0].Class != "latency" || items[0].Want != "3ms" || items[0].Got != "701ms" {
		t.Fatalf("only confirmed slow steps become gate items: %+v", items)
	}
	gateWorkspace(t, map[string][]gateOutcome{
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: items}}},
	})
	out, code := runGateOut(t)
	if code != 1 || !strings.Contains(out, "ThingService/List latency: 2 step(s) in 1 chain(s); e.g. cli-thing-flow list; slower than in the safe spot's run") {
		t.Fatalf("a latency regression is grouped under the slow rpc itself, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "list (ThingService/List) latency safe spot 3ms, now 701ms (+698ms)") || strings.Contains(out, "want=3ms") {
		t.Errorf("a latency change reads as the safe spot's and this run's time, not a threshold:\n%s", out)
	}
}

func TestRunHandsItsNotesAndErrorToTheGateInTheSidecar(t *testing.T) {
	b := &resettableUniqueBackend{}
	srv := b.server()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", fixLiteralChain("@"))
	ctx := context.Background()
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=one"}) })
	path := t.TempDir() + "/side.json"
	t.Setenv(gateReportEnv, path)
	captureStdout(t, func() { _ = runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=two"}) })
	var side gateSidecar
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &side) != nil || len(side.Notes) != 1 || !strings.HasPrefix(side.Notes[0], "CHAIN DEFECT: the chain collides with itself") || side.Error == "" {
		t.Fatalf("the CHAIN DEFECT line and the error reach the gate in the sidecar: %s", raw)
	}
}

func TestTheGateRunsAChainWithoutASafeSpotPastItsFirstFailure(t *testing.T) {
	twoDefectWorkspace(t, "name", "gadget")
	inProcessGate(t)
	out, code := runGateOut(t, "-v", "cli-two-defects")
	if code != 1 || !strings.Contains(out, "    [fetch] name want=gadget got=widget (and 1 more at fetch_again)\n") {
		t.Fatalf("a chain with no safe spot runs with -keep-going, so a failing step after the first reaches the gate, got %d:\n%s", code, out)
	}
}
