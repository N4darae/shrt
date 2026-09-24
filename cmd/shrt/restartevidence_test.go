package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

func saveTwoRuns(t *testing.T, build func() *runner.Record) *sessionLoss {
	t.Helper()
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	var last string
	for _, id := range []string{"20990101T000000Z-ev1", "20990101T000001Z-ev2"} {
		rec := build()
		rec.RunID = id
		if _, err := e.store.SaveRun(rec); err != nil {
			t.Fatal(err)
		}
		last = id
	}
	rec, err := e.store.LoadRun("cli-thing-flow", last)
	if err != nil {
		t.Fatal(err)
	}
	return examineSessionLoss(e, rec)
}

func resentAcceptedCreate() *runner.StepRecord {
	return &runner.StepRecord{ID: "clerk_create", Call: "ThingService/Create", Status: runner.StatusPassed, HTTPStatus: 200,
		AuthProfile: "clerk", AuthRetry: runner.AuthRetryResent, Request: json.RawMessage(`{"name":"clerk"}`),
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-77","name":"clerk"}`)}
}

func TestACallResentAndAcceptedAfterReloginIsRestartEvidence(t *testing.T) {
	loss := saveTwoRuns(t, func() *runner.Record {
		return restartRecord("", createdThing(), resentAcceptedCreate(), refusedFetch())
	})
	if loss == nil {
		t.Fatal("a token accepted earlier and then refused is a session loss")
	}
	if loss.finding() {
		t.Fatalf("a call re-sent after a fresh login and accepted is restart evidence, not a finding: %s", loss.line())
	}
	if !strings.Contains(loss.line(), "clerk_create") {
		t.Fatalf("the line must name the re-sent step as the evidence: %s", loss.line())
	}
}

func TestAListThatShrankAfterTheReloginIsRestartEvidence(t *testing.T) {
	list := func(status string, body string) *runner.StepRecord {
		return &runner.StepRecord{ID: "list", Call: "ThingService/List", Status: status, HTTPStatus: 200,
			Request: json.RawMessage(`{"prefix":"w"}`), Response: json.RawMessage(body)}
	}
	loss := saveTwoRuns(t, func() *runner.Record {
		before := list(runner.StatusPassed, `{"error":{"code":"OK"},"things":[{"id":"a"},{"id":"b"}]}`)
		after := list(runner.StatusPassed, `{"error":{"code":"OK"},"things":[]}`)
		after.ID = "list_again"
		return restartRecord("", createdThing(), before, refusedFetch(), after)
	})
	if loss == nil || loss.finding() {
		t.Fatalf("a list that had 2 items before the refusal and 0 after is restart evidence: %v", loss)
	}
	if !strings.Contains(loss.line(), "list_again") {
		t.Fatalf("the line must name the shrunk list: %s", loss.line())
	}
}

func TestAConflictThatVanishedAfterTheReloginIsRestartEvidence(t *testing.T) {
	loss := saveTwoRuns(t, func() *runner.Record {
		dup := &runner.StepRecord{ID: "dup", Call: "ThingService/Create", Status: runner.StatusFailed, HTTPStatus: 200,
			Request: json.RawMessage(`{"name":"widget"}`), BodyRefs: map[string]string{"name": "${steps.create.request.name}"},
			Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-99","name":"widget"}`),
			Expect:   []chain.ExpectResult{{Path: "error.code", Rule: "equals", Want: "ALREADY_EXISTS", Got: "OK"}}}
		return restartRecord("", createdThing(), refusedFetch(), dup)
	})
	if loss == nil || loss.finding() {
		t.Fatalf("a create expected to conflict with data created before the refusal now succeeding is restart evidence: %v", loss)
	}
	if !strings.Contains(loss.line(), "dup") {
		t.Fatalf("the line must name the step whose conflict vanished: %s", loss.line())
	}
}

func TestTheSameRPCAcceptedAfterTheFreshLoginIsRestartEvidence(t *testing.T) {
	loss := saveTwoRuns(t, func() *runner.Record {
		later := &runner.StepRecord{ID: "fetch_later", Call: "ThingService/Fetch", Status: runner.StatusPassed, HTTPStatus: 200,
			Request:  json.RawMessage(`{"id":"th-4f2a9c"}`),
			Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)}
		return restartRecord("", createdThing(), refusedFetch(), later)
	})
	if loss == nil || loss.finding() {
		t.Fatalf("the refused rpc accepting the fresh login's token shows the refusal did not persist: %v", loss)
	}
}

func TestAnExpectedPassingNotFoundIsNotRestartEvidence(t *testing.T) {
	loss := saveTwoRuns(t, func() *runner.Record {
		missing := &runner.StepRecord{ID: "get_missing", Call: "ThingService/Get", Status: runner.StatusPassed, HTTPStatus: 200,
			Request: json.RawMessage(`{"id":"th-4f2a9czz"}`), BodyRefs: map[string]string{"id": "${create.id}zz"},
			Response: json.RawMessage(`{"error":{"code":"REJECTED","reason":"ThingNotFound"}}`),
			Expect:   []chain.ExpectResult{{Path: "error.reason", Rule: "equals", Want: "ThingNotFound", Got: "ThingNotFound", Passed: true}}}
		return restartRecord("", createdThing(), refusedFetch(), missing)
	})
	if loss == nil || !loss.finding() {
		t.Fatalf("a not-found the step expects is no evidence of lost data, so a repeated refusal is a finding: %v", loss)
	}
}

func TestARefusalOfAnotherKindIsNotTheSameWay(t *testing.T) {
	n := 0
	loss := saveTwoRuns(t, func() *runner.Record {
		n++
		refused := refusedFetch()
		if n == 1 {
			refused.HTTPStatus = 200
			refused.Response = json.RawMessage(`{"error":{"code":"UNAUTHENTICATED"}}`)
		}
		return restartRecord("", createdThing(), refused)
	})
	if loss == nil || loss.finding() {
		t.Fatalf("an in-band 200 refusal and a 401 are not refused the same way: %v", loss)
	}
}

func TestAPassedRunWithAReadResentSaysOnlyThat(t *testing.T) {
	read := &runner.StepRecord{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusPassed, HTTPStatus: 200,
		AuthRetry: runner.AuthRetryResent, Request: json.RawMessage(`{"id":"th-4f2a9c"}`),
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)}
	rec := restartRecord("20990101T000000Z-pass1", createdThing(), read)
	rec.Status = runner.StatusPassed
	loss := detectSessionLoss(rec)
	if loss == nil {
		t.Fatal("a read re-sent after a token accepted earlier was refused is still reported")
	}
	line := loss.line()
	if strings.Contains(line, "re-run") || strings.Contains(line, "restarted mid-run") {
		t.Fatalf("a passed run whose read was re-sent and accepted must say only that: %s", line)
	}
	if !strings.Contains(line, "re-sent") || !strings.Contains(line, "fetch") {
		t.Fatalf("the line must say the read was re-sent after a refused token: %s", line)
	}
}
