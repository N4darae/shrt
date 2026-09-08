package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

const pendingDeployOverlay = `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: create a thing
    required: [name]
    status: draft
    failures:
      - code: 1210
        reason: IdempotencyReplayMismatch
        when: a replayed key names a different request
        pending_deploy: a47bb688
`

const pendingDeployBothOverlay = `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: create a thing
    required: [name]
    status: draft
    failures:
      - code: 1210
        reason: IdempotencyReplayMismatch
        unreachable: no api sequence reaches it
        pending_deploy: a47bb688
`

const pendingDeployProseOverlay = `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: create a thing
    required: [name]
    status: draft
    failures:
      - code: 1210
        reason: IdempotencyReplayMismatch
        pending_deploy: fixed upstream but not released yet
`

func issuesFor(t *testing.T, body string) []contract.Issue {
	t.Helper()
	return contract.LintLibrary(libraryFrom(t, body), catalogtest.New())
}

func matching(issues []contract.Issue, needle string) []contract.Issue {
	out := []contract.Issue{}
	for _, i := range issues {
		if strings.Contains(i.Message, needle) {
			out = append(out, i)
		}
	}
	return out
}

func TestPendingDeployIsAccepted(t *testing.T) {
	for _, i := range issuesFor(t, pendingDeployOverlay) {
		if i.Severity == contract.SeverityError {
			t.Fatalf("clean pending_deploy raised an error: %s %s", i.Field, i.Message)
		}
	}
}

func TestPendingDeployKeepsWhenProse(t *testing.T) {
	got := matching(issuesFor(t, pendingDeployOverlay), "when is misleading")
	if len(got) != 0 {
		t.Fatalf("pending_deploy must not suppress when, unlike unreachable: %v", got[0].Message)
	}
}

func TestPendingDeployWithUnreachableIsAnError(t *testing.T) {
	got := matching(issuesFor(t, pendingDeployBothOverlay), "pending_deploy")
	if len(got) == 0 {
		t.Fatal("pending_deploy alongside unreachable must be an error: the two make opposite claims")
	}
	if got[0].Severity != contract.SeverityError {
		t.Fatalf("want error severity, got %s", got[0].Severity)
	}
}

func TestPendingDeployMustBeCommitish(t *testing.T) {
	got := matching(issuesFor(t, pendingDeployProseOverlay), "commit")
	if len(got) == 0 {
		t.Fatal("prose in pending_deploy must be rejected: the field's whole value is that a script can test ancestry")
	}
	if got[0].Severity != contract.SeverityError {
		t.Fatalf("want error severity, got %s", got[0].Severity)
	}
}

func TestPendingDeployEntriesAreCounted(t *testing.T) {
	lib := libraryFrom(t, pendingDeployOverlay)
	got := contract.PendingDeploy(lib)
	if len(got) != 1 {
		t.Fatalf("want 1 pending-deploy entry, got %d", len(got))
	}
	if got[0].Commit != "a47bb688" {
		t.Fatalf("commit: want a47bb688, got %q", got[0].Commit)
	}
	if got[0].Code != 1210 {
		t.Fatalf("code: want 1210, got %d", got[0].Code)
	}
	if !strings.HasSuffix(got[0].RPC, "ThingService/Create") {
		t.Fatalf("rpc: want the declaring rpc, got %q", got[0].RPC)
	}
}
