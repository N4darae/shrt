package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func descriptorWithoutFetchTotal(t *testing.T) []byte {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(catalogtest.Descriptor(), fds); err != nil {
		t.Fatal(err)
	}
	for _, m := range fds.File[0].MessageType {
		if m.GetName() != "FetchResponse" {
			continue
		}
		kept := m.Field[:0]
		for _, f := range m.Field {
			if f.GetName() != "total" {
				kept = append(kept, f)
			}
		}
		m.Field = kept
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fixUndeclaredWorkspace(t *testing.T, total int, forgetValues bool) *fixThing {
	t.Helper()
	f := &fixThing{total: total}
	fixWorkspace(t, f)
	writeFile(t, ".shrt/descriptor.binpb", string(descriptorWithoutFetchTotal(t)))
	fixApprove(t, "cli-thing-flow")
	if forgetValues {
		e, err := loadEnv(false)
		if err != nil {
			t.Fatal(err)
		}
		spot, err := e.store.LoadSafeSpot("cli-thing-flow")
		if err != nil {
			t.Fatal(err)
		}
		held := false
		for _, st := range spot.Steps {
			held = held || len(st.Undeclared) > 0
			st.Undeclared = nil
		}
		if !held {
			t.Fatalf("the safe spot's run had total on the wire undeclared, so the record should hold its value")
		}
		spot.Digest = spot.ComputeDigest()
		out, err := json.MarshalIndent(spot, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(out))
	}
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.Descriptor()))
	return f
}

func TestCLIVerifyOfFieldsTheDescriptorDidNotDeclare(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after int
		forget, fails bool
		has, lacks    []string
	}{
		{"a field declared since and never sent is not drift", 0, 0, false, false, []string{"no drift"}, nil},
		{"a new field sent with a value is a change", 0, 7, false, true, []string{"unexpected total"}, nil},
		{"an undeclared field on the wire that is gone now is a change", 7, 0, false, true, []string{"total", "want=7"}, []string{"not on the wire (left at the proto3 default, the same bytes"}},
		{"gone now is a change even when the safe spot did not record its value", 7, 0, true, true, []string{"total"}, []string{"not on the wire (left at the proto3 default, the same bytes"}},
		{"declared now with the same value is no change", 7, 7, false, false, []string{"no drift"}, []string{"unexpected total"}},
		{"declared now with another value is a change", 7, 8, false, true, []string{"want=7", "got=8"}, nil},
		{"a value an older safe spot did not record is not compared", 7, 7, true, false, []string{"not compared", "fetch total"}, []string{"unexpected total"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixUndeclaredWorkspace(t, tc.before, tc.forget)
			f.set(func(f *fixThing) { f.total = tc.after })
			out, err := fixCmd(t, "verify", "cli-thing-flow")
			if (err != nil) != tc.fails {
				t.Fatalf("fails=%v, got %v\n%s", tc.fails, err, out)
			}
			for _, w := range tc.has {
				if !strings.Contains(out, w) {
					t.Errorf("want %q in:\n%s", w, out)
				}
			}
			for _, w := range tc.lacks {
				if strings.Contains(out, w) {
					t.Errorf("%q must not appear in:\n%s", w, out)
				}
			}
		})
	}
	t.Run("undeclared fields are printed once grouped by rpc", func(t *testing.T) {
		fixWorkspace(t, &fixThing{tier: "gold"}, uniqueNameChain)
		for i, quiet := range []bool{true, false} {
			args := []string{"cli-unique", "-var", "tag=g" + itoa(i)}
			if quiet {
				args = append(args, "-quiet")
			}
			out, err := fixCmd(t, "run", args...)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			want := "the backend sends fields the proto does not declare: update the proto/descriptor if you want them compared"
			if strings.Count(out, want) != 1 || !strings.Contains(out, "ThingService/Create -> tier") || !strings.Contains(out, "ThingService/Fetch -> tier") || strings.Contains(out, "rebuild the descriptor") {
				t.Fatalf("quiet=%v: one grouped advice line naming each rpc and its fields:\n%s", quiet, out)
			}
		}
	})
}

const fixSameCallChain = `apiVersion: shrt/v1
name: pair-flow
steps:
    - id: c1
      call: ThingService/Create
      body:
          name: alpha
          kind: KIND_A
          idempotency_key: ${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: c2
      call: ThingService/Create
      body:
          name: beta
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
          - path: name
            equals: alpha
    - id: f2
      call: ThingService/Fetch
      body:
          id: ${c2.id}
      expect:
          - path: error.code
            equals: OK
          - path: name
            equals: beta
`

const fixReadsChain = `apiVersion: shrt/v1
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
` + fixF3Step

const fixF3Step = `    - id: f3
      call: ThingService/Fetch
      body:
          id: ${c1.id}
      expect:
          - path: error.code
            equals: OK
`

const fixTopRead = `steps:
    - id: top
      call: ThingService/Fetch
      body:
          id: nothing
      expect:
          - path: error.code
            equals: OK
`

func fixMoveC2AfterF1(s string) string {
	c2, f1, f2 := strings.Index(s, "    - id: c2\n"), strings.Index(s, "    - id: f1\n"), strings.Index(s, "    - id: f2\n")
	moved := s[:c2] + s[f1:f2] + s[c2:f1] + s[f2:]
	return strings.Replace(moved, "            equals: beta\n", "            equals: beta\n          - path: id\n            not_empty: true\n", 1)
}

func fixDropC2AndF2(s string) string {
	c2 := s[strings.Index(s, "    - id: c2\n"):strings.Index(s, "    - id: f1\n")]
	return strings.Replace(strings.Replace(s, c2, "", 1), s[strings.Index(s, "    - id: f2\n"):], "", 1)
}

func TestCLIVerifyPairsRenamedStepsAndScopesChainChanges(t *testing.T) {
	regression := "regression"
	for _, tc := range []struct {
		name, chain, file string
		edit              func(string) string
		after             int
		verdict           string
		has, lacks        []string
	}{
		{"two renamed steps that share a call are renames", fixSameCallChain, "pair-flow",
			strings.NewReplacer("- id: f1\n", "- id: fa\n", "- id: f2\n", "- id: fb\n").Replace, 750, "",
			[]string{"f1 -> fa", "f2 -> fb"}, nil},
		{"a change at renamed same-call steps is judged under the new name", fixSameCallChain, "pair-flow",
			strings.NewReplacer("- id: f1\n", "- id: fa\n", "- id: f2\n", "- id: fb\n").Replace, 751, regression,
			[]string{"[fa, fb] changed total want=750 got=751"}, []string{"missing", "unexpected step", "chain change"}},
		{"swapping the ids of two same-call steps is two renames", fixSameCallChain, "pair-flow",
			strings.NewReplacer("- id: c1\n", "- id: c2\n", "- id: c2\n", "- id: c1\n", "${c1.id}", "${c2.id}", "${c2.id}", "${c1.id}").Replace, 750, "",
			[]string{"c1 -> c2", "c2 -> c1"}, []string{"moved", "regression:", "not renamed consistently"}},
		{"a step order move is a chain change even beside an expectation edit", fixSameCallChain, "pair-flow", fixMoveC2AfterF1, 750, "chain change", nil, nil},
		{"a removed write still explains the steps after it", fixSameCallChain, "pair-flow", fixDropC2AndF2, 751, "drift after a chain change", nil, nil},
		{"a renamed step is compared with its old self", "", "cli-thing-flow",
			strings.NewReplacer("- id: fetch\n", "- id: fetch_thing\n").Replace, 1, regression,
			[]string{"fetch -> fetch_thing", "total", "750"}, []string{"this run has no such step", "the safe spot has no such step", "chain change"}},
		{"deleting a trailing read explains no change elsewhere", fixReadsChain, "reads-flow",
			strings.NewReplacer(fixF3Step, "").Replace, 751, regression, nil, []string{"after a chain change, so they are not evidence"}},
		{"inserting a read and renaming a step explains no change elsewhere", fixReadsChain, "reads-flow",
			strings.NewReplacer("steps:\n", fixTopRead, "- id: f1\n", "- id: fa\n").Replace, 751, regression,
			[]string{"f1 -> fa", "[fa, f2"}, []string{"after a chain change, so they are not evidence"}},
		{"renaming a step and deleting another explains no change elsewhere", fixReadsChain, "reads-flow",
			strings.NewReplacer("- id: f1\n", "- id: fa\n", fixF3Step, "").Replace, 751, regression,
			[]string{"f1 -> fa", "[fa, f2"}, []string{"after a chain change, so they are not evidence"}},
		{"deleting a trailing read and renaming another explains no change elsewhere", fixReadsChain, "reads-flow",
			strings.NewReplacer(fixF3Step, "", "- id: f2\n", "- id: fb\n").Replace, 751, regression, nil, []string{"after a chain change, so they are not evidence"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fixThing{total: 750, fetchStored: true}
			if tc.chain == "" {
				fixWorkspace(t, f)
			} else {
				fixWorkspace(t, f, tc.chain)
			}
			fixApprove(t, tc.file)
			path := ".shrt/chains/" + tc.file + ".yaml"
			raw := string(mustRead(t, path))
			edited := tc.edit(raw)
			if edited == raw {
				t.Fatal("the edit did not apply")
			}
			writeFile(t, path, edited)
			f.set(func(f *fixThing) { f.total = tc.after })
			out, err := fixCmd(t, "verify", tc.file, "-quiet")
			switch {
			case tc.verdict == "" && err != nil:
				t.Fatalf("want no drift, got %v\n%s", err, out)
			case tc.verdict == regression && (exitCodeOf(err) != 1 || !strings.Contains(err.Error(), regression)):
				t.Fatalf("want a regression, got %v\n%s", err, out)
			case tc.verdict != "" && (err == nil || !strings.Contains(err.Error(), tc.verdict)):
				t.Fatalf("want %q, got %v\n%s", tc.verdict, err, out)
			case tc.verdict != regression && tc.verdict != "" && strings.Contains(err.Error(), regression):
				t.Fatalf("a chain change is not a regression: %v\n%s", err, out)
			}
			for _, w := range tc.has {
				if !strings.Contains(out, w) {
					t.Errorf("want %q in:\n%s", w, out)
				}
			}
			for _, w := range tc.lacks {
				if strings.Contains(out, w) {
					t.Errorf("%q must not appear in:\n%s", w, out)
				}
			}
		})
	}
}

func TestARenamedStepKeepsItsHistoryInSupersedeAndDiff(t *testing.T) {
	f := &fixThing{total: 750}
	fixWorkspace(t, f)
	fixApprove(t, "cli-thing-flow")
	first := runIDs(t)[0]
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "- id: fetch\n", "- id: fetch_thing\n", 1))
	f.set(func(f *fixThing) { f.total = 1 })
	out := captureStdout(t, func() {
		if _, err := fixCmd(t, "run", "cli-thing-flow", "-quiet"); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(t.Context(), []string{"cli-thing-flow", "-supersede", "-note", "renamed"}); err != nil {
			t.Fatalf("supersede: %v", err)
		}
	})
	if !strings.Contains(out, "renamed from fetch") || !strings.Contains(out, "750 -> 1") || strings.Contains(out, "-> absent") || strings.Contains(out, "absent ->") {
		t.Errorf("the supersede review names the rename and shows the renamed step's change:\n%s", out)
	}
	out, err := fixCmd(t, "diff", "cli-thing-flow", first, "latest")
	if exitCodeOf(err) != 1 || !strings.Contains(out, "fetch -> fetch_thing") || !strings.Contains(out, "total") || strings.Contains(out, "not reached in B") {
		t.Errorf("diff pairs the renamed step and shows its change: %v\n%s", err, out)
	}
}

func TestAFreshTokenRefusalRepeatsAcrossAStepRename(t *testing.T) {
	ctx, e, base := approvedThingFlowRun(t)
	saveResentAndRefusedAtFetch(t, e, base, "20990101T000000Z-resent1")
	renamed := copyRun(t, base, "tmp")
	for _, st := range renamed.Steps {
		if st.ID == "fetch" {
			st.ID = "fetch_thing"
		}
	}
	saveResentAndRefusedAtFetch(t, e, renamed, "20990101T000001Z-resent2")
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", strings.Replace(string(mustRead(t, ".shrt/chains/cli-thing-flow.yaml")), "- id: fetch\n", "- id: fetch_thing\n", 1))
	var err error
	out := captureStdout(t, func() { err = runVerify(ctx, []string{"cli-thing-flow", "-quiet", "-run", "20990101T000001Z-resent2"}) })
	if exitCodeOf(err) != 1 || !strings.Contains(out, "FINDING: ") || !strings.Contains(err.Error(), "20990101T000000Z-resent1") {
		t.Fatalf("the earlier run was refused the same way at the same step under its old name: a finding naming it, exit 1: %v\n%s", err, out)
	}
}

const fixExpectVarChain = `apiVersion: shrt/v1
name: cli-expect-var
vars:
    tag: first
    total: 300
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: error.code
            equals: OK
          - path: total
            equals: ${vars.total}
`

const fixFreshTagChain = `apiVersion: shrt/v1
name: cli-fresh-tag
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: total
            equals: 300
`

func TestCLIVerifyNamesTheInputThatChanged(t *testing.T) {
	t.Run("an expectation value a var changed", func(t *testing.T) {
		regressed := false
		srv := newTotalBackend(&regressed)
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		writeFile(t, ".shrt/chains/cli-expect-var.yaml", fixExpectVarChain)
		fixApprove(t, "cli-expect-var")
		out, err := fixCmd(t, "verify", "cli-expect-var", "-quiet", "-var", "tag=second", "-var", "total=4")
		if err == nil || !strings.Contains(out, "expectation differs from the confirmed run at fetch: total equals 300 -> 4 (${vars.total})") ||
			strings.HasPrefix(err.Error(), "regression") || !strings.Contains(err.Error(), "total=4, confirmed with 300") {
			t.Errorf("the var changed the expectation, not the backend: %v\n%s", err, out)
		}
		if out, err := fixCmd(t, "verify", "cli-expect-var", "-quiet", "-var", "tag=third"); err != nil || strings.Contains(out, "expectation differs") {
			t.Errorf("a fresh fixture tag changes no expectation value: %v\n%s", err, out)
		}
	})
	t.Run("an undeclared tag is fresh every run and verify still compares the runs", func(t *testing.T) {
		regressed := false
		srv := newTotalBackend(&regressed)
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		writeFile(t, ".shrt/chains/cli-fresh-tag.yaml", fixFreshTagChain)
		e, err := loadEnv(true)
		if err != nil {
			t.Fatal(err)
		}
		tags := map[string]bool{}
		for i := range 2 {
			if _, err := fixCmd(t, "run", "cli-fresh-tag", "-quiet"); err != nil {
				t.Fatalf("run %d with no -var: %v", i, err)
			}
			rec, err := e.store.LatestRun("cli-fresh-tag")
			if err != nil {
				t.Fatal(err)
			}
			tag, _ := rec.Vars["tag"].(string)
			if tag == "" || tags[tag] {
				t.Fatalf("run %d: each run records a tag of its own, got %v after %v", i, rec.Vars, tags)
			}
			tags[tag] = true
		}
		captureStdout(t, func() {
			if err := runConfirm(t.Context(), []string{"cli-fresh-tag", "-note", "total 300"}); err != nil {
				t.Fatalf("propose: %v", err)
			}
			if err := runConfirm(t.Context(), []string{"cli-fresh-tag", "-approve", "-by", "alice@example.test"}); err != nil {
				t.Fatalf("approve: %v", err)
			}
		})
		if out, err := fixCmd(t, "verify", "cli-fresh-tag", "-quiet"); err != nil || !strings.Contains(out, "no drift") {
			t.Fatalf("a fresh tag only renames fixtures: %v\n%s", err, out)
		}
		regressed = true
		if out, err := fixCmd(t, "verify", "cli-fresh-tag", "-quiet"); err == nil || !strings.Contains(err.Error(), "regression") || strings.Contains(out, "drift with different input") {
			t.Fatalf("with fresh tags a changed total is still a regression: %v\n%s", err, out)
		}
	})
	t.Run("a changed header input", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			name := "widget"
			if r.Header.Get("X-Tenant-Key") != "alpha" || r.Header.Get("X-Region") != "us" {
				name = "other"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
		}))
		t.Cleanup(srv.Close)
		chdirToFreshCLIWorkspace(t, srv.URL)
		writeFile(t, ".shrt/chains/cli-headers.yaml", `apiVersion: shrt/v1
name: cli-headers
vars:
    key: alpha
steps:
    - id: fetch
      call: ThingService/Fetch
      headers:
          X-Tenant-Key: ${vars.key}
          X-Region: ${env.SHRT_TEST_REGION}
      body:
          id: thing-1
      expect:
          - path: error.code
            equals: OK
`)
		t.Setenv("SHRT_TEST_REGION", "us")
		fixApprove(t, "cli-headers")
		for _, tc := range []struct {
			region, header string
			args           []string
		}{
			{"us", "headers.X-Tenant-Key", []string{"-var", "key=beta"}},
			{"eu", "headers.X-Region", nil},
		} {
			t.Setenv("SHRT_TEST_REGION", tc.region)
			out, err := fixCmd(t, "verify", append([]string{"cli-headers", "-quiet"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), "drift with different input") || !strings.Contains(out, "request differs from the confirmed run at fetch "+tc.header) {
				t.Fatalf("%s: the header sent differs, so the drift is with different input: %v\n%s", tc.header, err, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "headers.X-Tenant-Key") && (strings.Contains(line, "beta") || strings.Contains(line, "alpha")) {
					t.Fatalf("a credential-named header's value must not be printed: %s", line)
				}
			}
		}
	})
	t.Run("a step volatile pattern masks only its own step", func(t *testing.T) {
		fixWorkspace(t, &fixThing{}, `apiVersion: shrt/v1
name: cli-scoped
vars:
    label: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: ${vars.label}
      expect:
          - path: error.code
            equals: OK
    - id: create_masked
      call: ThingService/Create
      body:
          name: ${vars.label}
      volatile:
          - name
      expect:
          - path: error.code
            equals: OK
`)
		fixApprove(t, "cli-scoped")
		out, _ := fixCmd(t, "verify", "cli-scoped", "-masked", "-var", "label=second")
		at := strings.Index(out, "values echoing a fixture name")
		if strings.Contains(out, "create name (first -> second) hidden by name") || !strings.Contains(out, "create_masked name (first -> second) hidden by name") ||
			at < 0 || !strings.Contains(out[at:], "create name (first -> second)") {
			t.Fatalf("create_masked's volatile pattern masks only create_masked; create's name is listed as a fixture echo:\n%s", out)
		}
	})
}
