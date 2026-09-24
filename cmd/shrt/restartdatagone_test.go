package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
)

func restartRecord(id string, steps ...*runner.StepRecord) *runner.Record {
	for i, st := range steps {
		st.Index = i + 1
		if st.AuthProfile == "" {
			st.AuthProfile = "default"
		}
	}
	return &runner.Record{RunID: id, Chain: "cli-thing-flow", Status: runner.StatusError, StartedAt: time.Now(), Steps: steps}
}

func createdThing() *runner.StepRecord {
	return &runner.StepRecord{ID: "create", Call: "ThingService/Create", Status: runner.StatusPassed, HTTPStatus: 200,
		Request:  json.RawMessage(`{"name":"widget"}`),
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)}
}

func refusedFetch() *runner.StepRecord {
	return &runner.StepRecord{ID: "fetch", Call: "ThingService/Fetch", Status: runner.StatusError, HTTPStatus: 401,
		AuthRetry: runner.AuthRetryNotResent, Request: json.RawMessage(`{"id":"th-4f2a9c"}`),
		Transport: &runner.TransportError{Code: "unauthenticated", Message: "invalid or expired token"},
		Response:  json.RawMessage(`{"code":"unauthenticated","message":"invalid or expired token"}`)}
}

func goneRead() *runner.StepRecord {
	return &runner.StepRecord{ID: "again", Call: "ThingService/Fetch", Status: runner.StatusFailed, HTTPStatus: 404,
		Request: json.RawMessage(`{"id":"th-4f2a9c"}`), BodyRefs: map[string]string{"id": "${create.id}"},
		Transport: &runner.TransportError{Code: "not_found", Message: "no thing th-4f2a9c"},
		Response:  json.RawMessage(`{"code":"not_found","message":"no thing th-4f2a9c"}`)}
}

func TestDataGoneAfterTheReloginKeepsARepeatedRefusalARestart(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20990101T000000Z-gone1", "20990101T000001Z-gone2"} {
		if _, err := e.store.SaveRun(restartRecord(id, createdThing(), refusedFetch(), goneRead())); err != nil {
			t.Fatal(err)
		}
	}
	rec, err := e.store.LoadRun("cli-thing-flow", "20990101T000001Z-gone2")
	if err != nil {
		t.Fatal(err)
	}
	loss := examineSessionLoss(e, rec)
	if loss == nil {
		t.Fatal("a token accepted earlier and then refused is a session loss")
	}
	if loss.finding() {
		t.Fatalf("data created before the refusal was gone after the re-login, which proves a restart: not a finding: %s", loss.line())
	}
	if !strings.Contains(loss.line(), "restarted mid-run") || !strings.Contains(loss.line(), "no thing th-4f2a9c") {
		t.Fatalf("the line must say restart and name the evidence that the data is gone: %s", loss.line())
	}
}

func TestAnUnansweredStepBeforeARepeatedRefusalKeepsItARestart(t *testing.T) {
	srv := newEchoNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	gateway := func() *runner.StepRecord {
		return &runner.StepRecord{ID: "peek", Call: "ThingService/Fetch", Status: runner.StatusError, HTTPStatus: 502,
			Request:   json.RawMessage(`{"id":"th-4f2a9c"}`),
			Transport: &runner.TransportError{Code: "http_502", Message: "bad gateway"}}
	}
	for _, id := range []string{"20990101T000000Z-gw1", "20990101T000001Z-gw2"} {
		if _, err := e.store.SaveRun(restartRecord(id, createdThing(), gateway(), refusedFetch())); err != nil {
			t.Fatal(err)
		}
	}
	rec, err := e.store.LoadRun("cli-thing-flow", "20990101T000001Z-gw2")
	if err != nil {
		t.Fatal(err)
	}
	if !runner.NotAnsweredByService(rec.Steps[1]) {
		t.Fatal("fixture: the gateway step must read as not answered by the service")
	}
	loss := examineSessionLoss(e, rec)
	if loss == nil || loss.finding() {
		t.Fatalf("a gateway answer before the refusal is restart evidence, not a finding: %v", loss)
	}
}

func TestAnAnsweredReadWithoutTheCreatedIDIsNotDataStillThere(t *testing.T) {
	emptyList := &runner.StepRecord{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusFailed, HTTPStatus: 200,
		Request: json.RawMessage(`{"id":"th-4f2a9c"}`), BodyRefs: map[string]string{"id": "${create.id}"},
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"","name":""}`)}
	rec := restartRecord("20990101T000000Z-empty1", createdThing(), refusedFetch(), emptyList)
	if got := readBackAfter(rec, 1); got != "" {
		t.Fatalf("step %s answered without the id created before the refusal, so it did not read that data back", got)
	}
	found := &runner.StepRecord{ID: "list", Call: "ThingService/Fetch", Status: runner.StatusPassed, HTTPStatus: 200,
		Request: json.RawMessage(`{"id":"th-4f2a9c"}`), BodyRefs: map[string]string{"id": "${create.id}"},
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-4f2a9c","name":"widget"}`)}
	rec = restartRecord("20990101T000000Z-found1", createdThing(), refusedFetch(), found)
	if got := readBackAfter(rec, 1); got != "list" {
		t.Fatalf("a read answering with the id created before the refusal read it back, got %q", got)
	}
}
