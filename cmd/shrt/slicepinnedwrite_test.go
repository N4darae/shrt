package main

import (
	"context"
	"strings"
	"testing"
)

const pinnedWriteChain = `apiVersion: shrt/v1
name: cli-pinned-write
vars:
    tag: T1
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: act
      call: ThingService/Create
      body:
          name: ${create.id}
      expect:
          - path: error.code
            equals: OK
`

func pinnedWriteWorkspace(t *testing.T) {
	t.Helper()
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-pinned-write.yaml", pinnedWriteChain)
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-pinned-write", "-quiet", "-var", "tag=T9"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
}

func pinnedWriteSlice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = chainSlice(context.Background(), append([]string{"cli-pinned-write"}, args...))
	})
	return out, err
}

func TestCLISlicePinVerifyRefusesToResendAWriteOnTheRecordedEntity(t *testing.T) {
	pinnedWriteWorkspace(t)
	out, err := pinnedWriteSlice(t, "-step", "act", "-mode", "pin", "-run", "latest", "-verify")
	if err == nil || exitCodeOf(err) != 2 {
		t.Fatalf("a pinned write re-sent on the recorded run's entity must be refused before sending (exit 2), got %v\n%s", err, out)
	}
	for _, want := range []string{"act (ThingService/Create) on create.id=thing-1", "-resend-writes", "shrt chain slice cli-pinned-write -step act -keep writes -run "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(out, "verify ") {
		t.Fatalf("nothing may be sent:\n%s", out)
	}
	if _, err := pinnedWriteSlice(t, "-step", "act", "-mode", "pin", "-run", "latest", "-verify", "-resend-writes"); err != nil && exitCodeOf(err) == 2 {
		t.Fatalf("-resend-writes sends it anyway: %v", err)
	}
}

func TestCLISlicePinWarnsThatRunningItResendsAWrite(t *testing.T) {
	pinnedWriteWorkspace(t)
	out, err := pinnedWriteSlice(t, "-step", "act", "-mode", "pin", "-run", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "WARNING: running this slice re-sends write step(s)") || !strings.Contains(out, "-keep writes") {
		t.Fatalf("a printed pin slice of a write must warn before anyone runs it:\n%s", out)
	}
}
