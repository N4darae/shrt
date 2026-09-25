package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func TestTheGateChecksSessionLifetimeOnceForTwoProfiles(t *testing.T) {
	b := &shortSessionBackend{uses: 1000, life: 300 * time.Millisecond}
	shortSessionWorkspace(t, b, sessionReadStep+`    - id: fetch_as_other
      call: ThingService/Fetch
      auth: other
      body: {id: thing-1}
      expect: [{path: error.code, equals: OK}, {path: name, equals: widget}]
`)
	raw, err := os.ReadFile(".shrt/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".shrt/config.yaml", strings.Replace(string(raw), "    expires_path: expires_at\n", `    expires_path: expires_at
    profiles:
        other:
            call: shrt.test.v1.AuthService/Login
            body:
                username: ${env.LIFE_USER}
                password: ${env.LIFE_PASSWORD}
            token_path: access_token
            expires_path: expires_at
`, 1))
	realGate(t)
	cacheASessionToken(t)
	time.Sleep(400 * time.Millisecond)
	out, code := runGateOut(t)
	if code != 1 || strings.Count(out, "checking session lifetime") != 1 || strings.Count(out, "end early: fresh token") != 1 {
		t.Fatalf("one held token settles sessions that end early for every profile: one check, one FINDING, got %d:\n%s", code, out)
	}
	if strings.Contains(out, "refused early once") {
		t.Fatalf("the finding covers the other profile's early refusal:\n%s", out)
	}
}

func TestTheGateLabelsWhatItsOwnOutputExplains(t *testing.T) {
	flaky := "FINDING: intermittent failure at ThingService/Fetch: the backend fails this rpc on some calls and answers it on others, " +
		"a defect in the backend (flaky under load, an exhausted pool, a race), not a deterministic regression at that step; " +
		"a re-run may pass and does not clear it; step 2 fetch got internal: pool exhausted, but " + strings.Repeat("x", 200)
	fetch := gateItem{Step: "fetch", Call: "shrt.test.v1.ThingService/Fetch", Path: "(failed)", Got: "internal: pool exhausted"}
	classed := fetch
	classed.Class = "regression"
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-thing-flow":    {{code: 1, stdout: "  " + flaky + "\n", side: gateSidecar{Items: []gateItem{fetch}}}},
		"verify cli-thing-flow": {{code: 1, side: gateSidecar{Items: []gateItem{classed}}}},
		"run cli-unique": {{code: 1, stdout: "  " + strings.Replace(flaky, "step 2 fetch", "step 3 fetch", 1) + "\n",
			side: gateSidecar{KeptRed: "not_as_pinned", Items: []gateItem{fetch}}}},
	})
	out, _ := runGateOut(t)
	for _, want := range []string{
		"FAIL       cli-thing-flow  intermittent: fetch (ThingService/Fetch) (failed) internal: pool exhausted\n",
		"not a deterministic regression at that step; ...\n",
		"FAIL       cli-unique      intermittent: 1 step(s) from Fetch (failed), reported above\n",
		"  FINDING: intermittent failure at ThingService/Fetch, as above\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "not a deterministic regression") != 1 {
		t.Errorf("each FINDING is printed once:\n%s", out)
	}
}

func TestTheGateSaysNotAsPinnedForAKeptRedChainThatFailedOtherwise(t *testing.T) {
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{code: 1, stderr: "shrt run: chain cli-unique: kept red, but it did not fail as pinned: NEW FAILURE outside the pinned defect: fetch refused at transport: internal\n",
			side: gateSidecar{KeptRed: "not_as_pinned"}}},
	})
	out, _ := runGateOut(t)
	if !strings.Contains(out, "FAIL       cli-unique      not as pinned: run: NEW FAILURE outside the pinned defect: fetch refused at transport: internal\n") {
		t.Errorf("the FAIL line says the chain did not fail as pinned:\n%s", out)
	}
}

func TestTheGateHeadlinesTheLengthOfAListThatShrank(t *testing.T) {
	gateWorkspace(t, map[string][]gateOutcome{
		"run cli-unique": {{code: 1, side: gateSidecar{Items: []gateItem{{Step: "batch", Call: "shrt.test.v1.ThingService/Create",
			Path: "results.11.status.code", Want: "SUCCESS", Got: "<none>", Length: "results length want=12 got=5"}}}}},
	})
	out, _ := runGateOut(t)
	if !strings.Contains(out, "FAIL       cli-unique      batch (ThingService/Create) results length want=12 got=5\n") {
		t.Errorf("the headline is the length, not the first missing index:\n%s", out)
	}
}

func TestARunItemPastTheEndOfItsListCarriesTheLength(t *testing.T) {
	st := &runner.StepRecord{ID: "batch", Response: []byte(`{"results":[{},{},{},{},{}]}`), Expect: []chain.ExpectResult{
		{Path: "results.4.status.code", Passed: true},
		{Path: "results.9.status.code"},
		{Path: "results.11.status.code"},
		{Path: "results.12", Rule: "exists", Want: false, Passed: true},
	}}
	if got := pastEnd(st, "results.9.status.code"); got != "results length want=12 got=5" {
		t.Errorf("got %q", got)
	}
	if got := pastEnd(st, "results.4.status.code"); got != "" {
		t.Errorf("an item the list holds has no length headline, got %q", got)
	}
}
