package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	root := t.TempDir()
	return store.New(filepath.Join(root, "runs"), filepath.Join(root, "safespots"))
}

func passingRun(id string) *runner.Record {
	return &runner.Record{
		RunID: id, Chain: "thing-flow", Target: "http://localhost", Status: runner.StatusPassed,
		Steps: []*runner.StepRecord{{
			Index: 1, ID: "create", Call: "ThingService/Create", Status: runner.StatusPassed,
			Response: json.RawMessage(`{"id":"thing-1"}`),
		}},
	}
}

func failedRun(id string) *runner.Record {
	rec := passingRun(id)
	rec.Status, rec.Steps[0].Status = runner.StatusFailed, runner.StatusFailed
	return rec
}

func humanConfirmation() store.Confirmation {
	return store.Confirmation{By: "reviewer", Acknowledged: true, Note: "checked in the admin UI"}
}

func mustSave(t *testing.T, s *store.Store, rec *runner.Record) string {
	t.Helper()
	path, err := s.SaveRun(rec)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPromoteNeedsAPersonAPassedRunAndSupersedeToReplace(t *testing.T) {
	s := newStore(t)
	for name, c := range map[string]store.Confirmation{"no confirmer": {Acknowledged: true}, "no acknowledgement": {By: "reviewer"}, "nothing at all": {}} {
		if _, _, err := s.Promote(passingRun("run-1"), c); !errors.Is(err, store.ErrNotConfirmed) || s.HasSafeSpot("thing-flow") {
			t.Fatalf("%s: want ErrNotConfirmed and no safe spot, got %v", name, err)
		}
	}
	if _, _, err := s.Promote(failedRun("run-1"), humanConfirmation()); !errors.Is(err, store.ErrRunNotPassed) {
		t.Fatalf("want ErrRunNotPassed, got %v", err)
	}
	first := passingRun("run-1")
	first.Build = "rc-3"
	if _, _, err := s.Promote(first, humanConfirmation()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Promote(passingRun("run-2"), humanConfirmation()); !errors.Is(err, store.ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if spot, err := s.LoadSafeSpot("thing-flow"); err != nil || spot.RunID != "run-1" || spot.Build != "rc-3" {
		t.Fatalf("the original safe spot survives with the build its run was stamped with: %+v %v", spot, err)
	}
	c := humanConfirmation()
	c.Supersede = true
	spot, _, err := s.Promote(passingRun("run-2"), c)
	if err != nil || spot.RunID != "run-2" || spot.Supersedes != "run-1" {
		t.Fatalf("want run-2 superseding run-1, got %+v %v", spot, err)
	}
	for _, id := range []string{"run-1", "run-3"} {
		if _, _, err := s.Promote(passingRun(id), c); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.SafeSpotsDir, "archive", "thing-flow"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, " ") != "run-1-2.json run-1.json run-2.json" {
		t.Fatalf("each archived spot is named by its run id and a run archived twice keeps both, got %v", names)
	}
}

func TestProposeThenApprove(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-1")
	mustSave(t, s, rec)
	if _, err := s.Propose(rec, store.ProposalInput{}); !errors.Is(err, store.ErrNoEvidence) {
		t.Fatalf("a proposal must say what was checked, got %v", err)
	}
	if _, err := s.Propose(failedRun("run-f"), store.ProposalInput{Checked: "x"}); !errors.Is(err, store.ErrRunNotPassed) || !strings.Contains(err.Error(), "create (failed)") || !strings.Contains(err.Error(), "run-f") {
		t.Fatalf("a failed run cannot be proposed and the refusal names the run and its failing step, got %v", err)
	}
	if _, _, err := s.Approve(rec.Chain, humanConfirmation()); !errors.Is(err, store.ErrNoProposal) {
		t.Fatalf("nothing to approve without a proposal, got %v", err)
	}
	p, err := s.Propose(rec, store.ProposalInput{Checked: "id is a fresh thing id"})
	if err != nil {
		t.Fatal(err)
	}
	if s.HasSafeSpot(rec.Chain) || p.ProposedBy != "agent" {
		t.Fatalf("proposing writes no safe spot and an unnamed proposer is the agent, got %q", p.ProposedBy)
	}
	if raw, err := os.ReadFile(p.Report); err != nil || !strings.Contains(string(raw), "id is a fresh thing id") || !strings.Contains(string(raw), `"id": "thing-1"`) {
		t.Fatalf("the report must carry the proposer's evidence and the responses: %v\n%s", err, raw)
	}
	if _, _, err := s.Approve(rec.Chain, store.Confirmation{By: "reviewer"}); !errors.Is(err, store.ErrNotConfirmed) {
		t.Fatalf("approval still needs a person's acknowledgement, got %v", err)
	}
	spot, _, err := s.Approve(rec.Chain, store.Confirmation{By: "reviewer", Acknowledged: true})
	if err != nil || spot.ConfirmedBy != "reviewer" || spot.ProposedBy != "agent" || spot.Note != "id is a fresh thing id" || s.HasProposal(rec.Chain) {
		t.Fatalf("the safe spot records approver and proposer and the proposal is cleared, got %+v %v", spot, err)
	}
}

func TestApproveRefusesAnyEditToWhatBecomesTheSafeSpot(t *testing.T) {
	for name, edit := range map[string]func(*runner.Record){
		"response":        func(r *runner.Record) { r.Steps[0].Response = []byte(`{"id":"thing-2"}`) },
		"volatile":        func(r *runner.Record) { r.Volatile = []string{"**"} },
		"step volatile":   func(r *runner.Record) { r.Steps[0].Volatile = []string{"**.id"} },
		"step status":     func(r *runner.Record) { r.Steps[0].Status = runner.StatusFailed },
		"request":         func(r *runner.Record) { r.Steps[0].Request = json.RawMessage(`{"name":"gadget"}`) },
		"target":          func(r *runner.Record) { r.Target = "http://elsewhere" },
		"transport_error": func(r *runner.Record) { r.Steps[0].Transport = &runner.TransportError{Code: "unavailable"} },
		"http_status":     func(r *runner.Record) { r.Steps[0].HTTPStatus = 503 },
		"vars":            func(r *runner.Record) { r.Vars = map[string]any{"tag": "t2"} },
		"build":           func(r *runner.Record) { r.Build = "other" },
	} {
		s := newStore(t)
		rec := passingRun("run-1")
		rec.Steps[0].Request = json.RawMessage(`{"name":"widget"}`)
		rec.Steps[0].HTTPStatus = 200
		rec.Vars = map[string]any{"tag": "t1"}
		mustSave(t, s, rec)
		if _, err := s.Propose(rec, store.ProposalInput{Checked: "checked"}); err != nil {
			t.Fatal(err)
		}
		edit(rec)
		mustSave(t, s, rec)
		if _, _, err := s.Approve(rec.Chain, humanConfirmation()); !errors.Is(err, store.ErrProposalChanged) || s.HasSafeSpot(rec.Chain) {
			t.Errorf("%s edited after proposing: approval must be refused, got %v", name, err)
		}
	}
}

func TestRunsLoadByIDLatestAndFileName(t *testing.T) {
	s := newStore(t)
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	first := &runner.Record{RunID: "20260923T100000Z-ffffffff", Chain: "thing-flow", StartedAt: base.Add(100 * time.Millisecond)}
	second := &runner.Record{RunID: "20260923T100000Z-00000000", Chain: "thing-flow", StartedAt: base.Add(700 * time.Millisecond)}
	mustSave(t, s, first)
	mustSave(t, s, second)
	if got, err := s.LatestRun("thing-flow"); err != nil || got.RunID != second.RunID {
		t.Fatalf("latest = %v %v, want %s: same-second ids are ordered by start time, not suffix", got, err, second.RunID)
	}
	if got, err := s.LoadRun("thing-flow", "latest"); err != nil || got.RunID != second.RunID {
		t.Fatalf("load latest: %v %v", got, err)
	}
	if ids, err := s.ListRuns("thing-flow"); err != nil || len(ids) != 2 || ids[0] != first.RunID {
		t.Fatalf("ListRuns = %v %v, want oldest first", ids, err)
	}
	if got, err := s.LoadRun("thing-flow", second.RunID+".json"); err != nil || got.RunID != second.RunID {
		t.Fatalf("a run id pasted with its file extension names the same run: %v", err)
	}
	if found, err := s.FindRun(second.RunID + ".json"); err != nil || len(found) != 1 {
		t.Fatalf("FindRun must accept the file name too: %v %d", err, len(found))
	}
	other := passingRun("run-1")
	other.Chain = "other-flow"
	raw, _ := os.ReadFile(mustSave(t, s, other))
	if err := os.WriteFile(filepath.Join(s.RunsDir, "thing-flow", "run-1.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRun("thing-flow", "run-1"); err == nil || !strings.Contains(err.Error(), "other-flow") {
		t.Fatalf("a run recorded for other-flow is not a run of thing-flow, got %v", err)
	}
	if _, err := s.LoadRun("other-flow", "run-1"); err != nil {
		t.Fatalf("its own chain still loads it: %v", err)
	}
}

func TestARunRecordEditedAfterShrtWroteItIsRefused(t *testing.T) {
	stripSeal := func(doc map[string]any) { delete(doc, "format"); delete(doc, "seal") }
	passed := func(doc map[string]any) {
		doc["status"] = runner.StatusPassed
		doc["steps"].([]any)[0].(map[string]any)["status"] = runner.StatusPassed
	}
	sealedOnly := func(r *runner.Record) {
		r.Steps[0].BodyRefs = map[string]string{"id": "${create.id}"}
		r.Steps[0].AuthPrincipal = "p-1"
	}
	for _, tc := range []struct {
		name  string
		rec   *runner.Record
		setup func(*runner.Record)
		edit  func(map[string]any)
		ok    bool
		says  string
	}{
		{name: "status flipped", rec: failedRun("run-edit"), edit: passed, says: "run-edit"},
		{name: "seal deleted", rec: failedRun("run-s"), edit: func(d map[string]any) { delete(d, "seal"); passed(d) }, says: "seal was removed"},
		{name: "format and seal removed", rec: passingRun("run-b"), setup: sealedOnly, edit: stripSeal},
		{name: "format 0", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { d["format"] = 0; delete(d, "seal") }},
		{name: "format null", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { d["format"] = nil; delete(d, "seal") }},
		{name: "format negative", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { d["format"] = -2 }},
		{name: "format fractional", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { d["format"] = 1.5 }},
		{name: "format text", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { d["format"] = "2" }},
		{name: "empty seal", rec: passingRun("run-b"), setup: sealedOnly, edit: func(d map[string]any) { delete(d, "format"); d["seal"] = "" }},
		{name: "predates seals", rec: passingRun("run-old"), edit: stripSeal, ok: true},
		{name: "stripped, dated after seals", rec: passingRun("run-stripped"), setup: func(r *runner.Record) { r.StartedAt = time.Date(2026, 9, 24, 22, 27, 47, 0, time.UTC) }, edit: stripSeal, says: "began sealing"},
		{name: "started before seals", rec: passingRun("run-older"), setup: func(r *runner.Record) { r.StartedAt = time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }, edit: stripSeal, ok: true},
		{name: "backdated, run id after seals", rec: passingRun("20260924T223900Z-1a2b3c4d"), setup: func(r *runner.Record) { r.StartedAt = time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }, edit: stripSeal, says: "20260924T223900Z-1a2b3c4d"},
		{name: "run id before seals", rec: passingRun("20260920T090000Z-1a2b3c4d"), setup: func(r *runner.Record) { r.StartedAt = time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }, edit: stripSeal, ok: true},
	} {
		s := newStore(t)
		var notes strings.Builder
		s.Notes = &notes
		if tc.setup != nil {
			tc.setup(tc.rec)
		}
		path := mustSave(t, s, tc.rec)
		raw, _ := os.ReadFile(path)
		doc := map[string]any{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		tc.edit(doc)
		edited, _ := json.Marshal(doc)
		if err := os.WriteFile(path, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := s.LoadRun(tc.rec.Chain, tc.rec.RunID)
		if tc.ok {
			if err != nil {
				t.Errorf("%s: a record with nothing only a sealing build writes predates seals: %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, store.ErrRunEdited) || !strings.Contains(err.Error(), tc.says) || strings.Contains(notes.String(), "predates") {
			t.Errorf("%s: want ErrRunEdited naming %q, not predating seals; got %v (notes %q)", tc.name, tc.says, err, notes.String())
		}
		loaded := &runner.Record{}
		if err := json.Unmarshal(edited, loaded); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Propose(loaded, store.ProposalInput{Checked: "looks right"}); !errors.Is(err, store.ErrRunEdited) {
			t.Errorf("%s: an edited record is not what ran, so it cannot be proposed; got %v", tc.name, err)
		}
	}
}

func TestAnUnsealedRunRecordIsReadWithOneNoteButCannotBeProposed(t *testing.T) {
	s := newStore(t)
	var notes strings.Builder
	s.Notes = &notes
	dir := filepath.Join(s.RunsDir, "thing-flow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"run-old-1", "run-old-2"} {
		raw, _ := json.Marshal(passingRun(id))
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := s.LoadRun("thing-flow", id)
		if err != nil {
			t.Fatalf("an unsealed record is still read, with a note: %v", err)
		}
		_, err = s.Propose(loaded, store.ProposalInput{Checked: "looks right"})
		if !errors.Is(err, store.ErrRunUnsealed) || !strings.Contains(strings.ToLower(err.Error()), "run the chain again") ||
			!strings.HasPrefix(err.Error(), "the run record predates sealed run records") || strings.Contains(err.Error(), "not the one shrt wrote") {
			t.Fatalf("a record with no seal is refused as predating seals, with a way out; got %v", err)
		}
	}
	if strings.Count(notes.String(), "\n") != 1 || !strings.Contains(notes.String(), "predates sealed run records") {
		t.Fatalf("one line must say the record predates seals, got %q", notes.String())
	}
	rec := passingRun("run-ok")
	rec.Vars = map[string]any{"n": 3, "big": json.Number("12345678901234567890"), "s": "<a&b>"}
	rec.Exports = map[string]any{"f": 1.5}
	mustSave(t, s, rec)
	loaded, err := s.LoadRun(rec.Chain, rec.RunID)
	if err != nil || loaded.Seal == "" {
		t.Fatalf("SaveRun must seal the record: %v", err)
	}
	if err := store.SealState(loaded); err != nil {
		t.Fatalf("an untouched record is sealed: %v", err)
	}
	if _, err := s.Propose(loaded, store.ProposalInput{Checked: "checked"}); err != nil {
		t.Fatalf("an untouched record must stay proposable after a round trip through disk: %v", err)
	}
}

func TestARecordWhoseValuesJSONRewritesStaysSealedOnLoad(t *testing.T) {
	s := newStore(t)
	rec := passingRun("run-awkward")
	rec.StartedAt = time.Date(2026, 9, 25, 16, 16, 18, 500000000, time.FixedZone("", 7*3600))
	rec.Vars = map[string]any{"huge": int64(1<<60 + 1), "f": float32(0.1), "list": []any{int8(3), "<b>"}}
	st := rec.Steps[0]
	st.Request = json.RawMessage("{ \"q\" : \"a<b>&c\u2028\" ,\n \"n\": 1.50 }")
	st.Exported = map[string]any{"id": uint64(1<<63 + 5), "at": time.Unix(0, 1).UTC()}
	st.Headers = map[string]string{}
	st.Expect = []chain.ExpectResult{{Path: "id", Rule: "equals", Want: int64(1<<53 + 1), Got: []string{"x"}, Passed: true}}
	mustSave(t, s, rec)
	loaded, err := s.LoadRun(rec.Chain, rec.RunID)
	if err != nil {
		t.Fatalf("a record SaveRun wrote loads: %v", err)
	}
	if err := store.SealState(loaded); err != nil {
		t.Fatalf("a record SaveRun wrote is sealed however JSON rewrites its values: %v", err)
	}
	if _, err := s.Propose(loaded, store.ProposalInput{Checked: "checked"}); err != nil {
		t.Fatalf("and stays proposable: %v", err)
	}
}

func TestAMissingBaselineSaysHowToCreateIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope")
	_, _, err := store.Ratchet(path, 7)
	for _, want := range []string{path, "does not exist", "echo 7 > " + path, "write 0"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("the error must say how to create the baseline (%q), got %v", want, err)
		}
	}
}
