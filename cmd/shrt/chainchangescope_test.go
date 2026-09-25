package main

import (
	"context"
	"strings"
	"testing"
)

const readsChain = `apiVersion: shrt/v1
name: reads-flow
steps:
    - id: c1
      call: ThingService/Create
      body:
          name: widget
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: f1
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
    - id: f2
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
    - id: f3
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
`

const f3Step = `    - id: f3
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
`

const topRead = `steps:
    - id: top
      call: ThingService/Fetch
      body:
          id: nothing
      expect:
          - path: error.code
            equals: OK
`

func confirmReadsChain(t *testing.T, total *int) {
	t.Helper()
	srv := newRenameTotalBackend(total)
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/reads-flow.yaml", readsChain)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"reads-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"reads-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"reads-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
}

func TestAChainChangeDoesNotExplainAChangeAtAStepItCannotAffect(t *testing.T) {
	for name, edit := range map[string][]string{
		"delete a trailing read":            {f3Step, ""},
		"insert a read and rename a step":   {"steps:\n", topRead, "- id: f1\n", "- id: fa\n"},
		"rename a step and delete another":  {"- id: f1\n", "- id: fa\n", f3Step, ""},
		"delete a trailing read, rename f2": {f3Step, "", "- id: f2\n", "- id: fb\n"},
	} {
		t.Run(name, func(t *testing.T) {
			total := 750
			confirmReadsChain(t, &total)
			raw := string(mustRead(t, ".shrt/chains/reads-flow.yaml"))
			for i := 0; i < len(edit); i += 2 {
				if !strings.Contains(raw, edit[i]) {
					t.Fatalf("edit %q not found", edit[i])
				}
				raw = strings.Replace(raw, edit[i], edit[i+1], 1)
			}
			writeFile(t, ".shrt/chains/reads-flow.yaml", raw)
			total = 751
			var verr error
			out := captureStdout(t, func() { verr = runVerify(context.Background(), []string{"reads-flow", "-quiet"}) })
			if code := exitCodeOf(verr); code != 1 || !strings.Contains(verr.Error(), "regression") {
				t.Fatalf("total changed 750 -> 751 at a step the edit cannot affect: want a regression, got %d: %v\n%s", code, verr, out)
			}
			if strings.Contains(out, "after a chain change, so they are not evidence") {
				t.Errorf("the edit is said to explain a change it cannot affect:\n%s", out)
			}
			if strings.Contains(raw, "- id: fa\n") && (!strings.Contains(out, "f1 -> fa") || !strings.Contains(out, "[fa] changed    total want=750 got=751")) {
				t.Errorf("the renamed step is paired with its old self and its change judged:\n%s", out)
			}
		})
	}
}

func TestARemovedWriteStillExplainsTheStepsAfterIt(t *testing.T) {
	total := 750
	confirmSameCallChain(t, &total)
	raw := string(mustRead(t, ".shrt/chains/pair-flow.yaml"))
	c2 := raw[strings.Index(raw, "    - id: c2\n"):strings.Index(raw, "    - id: f1\n")]
	f2 := raw[strings.Index(raw, "    - id: f2\n"):]
	writeFile(t, ".shrt/chains/pair-flow.yaml", strings.Replace(strings.Replace(raw, c2, "", 1), f2, "", 1))
	total = 751
	var verr error
	out := captureStdout(t, func() { verr = runVerify(context.Background(), []string{"pair-flow", "-quiet"}) })
	if verr == nil || !strings.Contains(verr.Error(), "drift after a chain change") {
		t.Fatalf("removing the write c2 can change what f1 reads back: want drift after a chain change, got %v\n%s", verr, out)
	}
}
