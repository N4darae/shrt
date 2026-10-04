package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/N4darae/shrt/store"
)

const verTFixtureChain = `apiVersion: shrt/v1
name: cli-fixture-flow
vars:
    tag: first
    trace: t-1
    kind: KIND_A
    total: 300
volatile:
    - '**.trace_id'
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: ${vars.kind}
          meta:
              trace_id: ${vars.trace}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: total
            equals: ${vars.total}
`

const verTDriftIndependentChain = `apiVersion: shrt/v1
name: cli-drift-independent
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body: {id: "${create.id}"}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_again
      call: ThingService/Fetch
      body: {id: "${fetch.id}"}
      expect:
          - path: error.code
            equals: OK
    - id: other
      call: ThingService/Create
      body: {name: gadget}
      expect:
          - path: error.code
            equals: OK
`

const verTSkippedWriteChain = `apiVersion: shrt/v1
name: cli-drift-skipped-write
steps:
    - id: make
      call: ThingService/Create
      body: {name: widget}
      expect:
          - path: error.code
            equals: OK
    - id: confirm
      call: ThingService/Create
      body: {name: "confirm-${make.id}"}
      expect:
          - path: error.code
            equals: OK
    - id: stock
      call: ThingService/Fetch
      body: {id: stock}
      expect:
          - path: total
            equals: 7
`

const verTUnjudgedChain = `apiVersion: shrt/v1
name: cli-unjudged
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_other
      call: ThingService/Fetch
      body: {id: thing-fixed}
      expect:
          - path: error.code
            equals: OK
`

const verTThreeCreatesChain = `apiVersion: shrt/v1
name: cli-two
steps:
    - id: first
      call: ThingService/Create
      body: {name: one, kind: KIND_A}
    - id: second
      call: ThingService/Create
      body: {name: two, kind: KIND_A}
    - id: third
      call: ThingService/Create
      body: {name: three, kind: KIND_A}
`

const verTDropFirstChain = `apiVersion: shrt/v1
name: cli-drop-twice
steps:
    - id: fetch
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch2
      call: ThingService/Fetch
      body: {id: thing-10}
      expect:
          - path: error.code
            equals: OK
`

const verTDropMiddleChain = `apiVersion: shrt/v1
name: cli-drop-one
steps:
    - id: create
      call: ThingService/Create
      body: {name: widget, kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_a
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_b
      call: ThingService/Fetch
      body: {id: thing-9}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_c
      call: ThingService/Fetch
      body: {id: thing-1}
      expect:
          - path: error.code
            equals: OK
`

const verTRewiredChain = `apiVersion: shrt/v1
name: cli-rewired
steps:
    - id: create_a
      call: ThingService/Create
      body: {name: widget a}
      expect:
          - path: error.code
            equals: OK
    - id: create_b
      call: ThingService/Create
      body: {name: widget b}
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body: {id: "${create_a.id}"}
      expect:
          - path: error.code
            equals: OK
`

const verTGeneratedChain = `apiVersion: shrt/v1
name: cli-generated
steps:
    - id: create
      call: ThingService/Create
      body: {name: "m-${uuid}@example.test", kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: fetch_missing
      call: ThingService/Fetch
      body: {id: "${uuid}"}
      expect:
          - path: error.code
            equals: NOT_FOUND
    - id: fetch_prefixed
      call: ThingService/Fetch
      body: {id: "thing-${uuid}"}
      expect:
          - path: error.code
            equals: NOT_FOUND
`

const verTVarChain = `apiVersion: shrt/v1
name: cli-var-flow
vars:
    label: widget
steps:
    - id: create
      call: ThingService/Create
      body: {name: "${vars.label}", kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
`

const verTDupMidChain = `apiVersion: shrt/v1
name: cli-dupmid
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body: {name: "Widget-${vars.tag}", kind: KIND_A}
      expect:
          - path: error.code
            equals: OK
    - id: mid
      call: ThingService/Fetch
      body: {id: none}
      expect:
          - path: error.code
            equals: OK
    - id: dup
      call: ThingService/Create
      body: {name: "widget-${vars.tag}", kind: KIND_A}
      expect:
          - path: error.code
            equals: ALREADY_EXISTS
`

func TestVerifyScenarios(t *testing.T) {
	const thing = ".shrt/chains/cli-thing-flow.yaml"
	for _, sc := range []struct {
		name  string
		setup func(t *testing.T) []verTCheck
	}{
		{"descriptor drift is no verdict unless a declared field changed too", func(t *testing.T) []verTCheck {
			name, extra := "widget", false
			driftWorkspace(t, &name, &extra)
			v := []string{"verify", "cli-thing-flow", "-quiet"}
			return []verTCheck{
				{args: v, set: func() { extra = true }, code: 3,
					has: []string{"could not verify cli-thing-flow: the response at fetch does not match the descriptor", `unknown field "traceHint"`, "shrt catalog build", "validate_output"},
					not: []string{"regression"}},
				{args: v, set: func() { name = "gadget" }, code: 1,
					has: []string{"ERR: regression", "fetch name", "widget", "gadget", `unknown field "traceHint"`}, not: []string{"could not verify"}},
				{args: v, set: func() { name = "widget"; verTRebuild(t, true)() }, code: 3,
					has: []string{"the proto does not declare"}, not: []string{"catalog build"}},
			}
		}},
		{"a body the proto cannot hold is a regression only against a current descriptor", func(t *testing.T) []verTCheck {
			var name any = "widget"
			next := 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
					next++
					verTWrite(w, verTOK("id", "thing-"+itoa(next)))
					return
				}
				verTWrite(w, verTOK("id", body["id"], "name", name))
			}, true)
			fixApprove(t, "cli-thing-flow")
			v := []string{"verify", "cli-thing-flow", "-quiet"}
			return []verTCheck{
				{args: v, set: func() { name = 7; verTRebuild(t, false)() }, code: 3, has: []string{"shrt catalog build"}},
				{args: v, set: verTRebuild(t, true), code: 1, has: []string{"ERR: regression", "fetch", "name", "7"}, not: []string{"not a verdict", "could not verify"}},
			}
		}},
		{"descriptor drift leaves an independent later step judged", func(t *testing.T) []verTCheck {
			broken, next := false, 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
					next++
					name := body["name"]
					if broken && name == "gadget" {
						name = "gizmo"
					}
					verTWrite(w, verTOK("id", "thing-"+itoa(next), "name", name))
					return
				}
				out := verTOK("id", body["id"], "name", "widget")
				if broken {
					out["traceHint"] = "t-1"
				}
				verTWrite(w, out)
			}, true, verTDriftIndependentChain)
			fixApprove(t, "cli-drift-independent")
			return []verTCheck{{args: []string{"verify", "cli-drift-independent", "-quiet"}, set: func() { broken = true }, code: 1,
				has: []string{"ERR: regression", "other name"}, not: []string{"ERR: regression: fetch_again"}}}
		}},
		{"descriptor drift before a skipped write is no regression", func(t *testing.T) []verTCheck {
			broken, qty := false, 10
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if r.URL.Path == "/shrt.test.v1.ThingService/Fetch" {
					verTWrite(w, verTOK("id", body["id"], "total", qty))
					return
				}
				out := verTOK("id", "thing-1", "name", body["name"])
				if body["name"] == "widget" {
					qty = 10
					if broken {
						out["traceHint"] = "t-1"
					}
				} else {
					qty -= 3
				}
				verTWrite(w, out)
			}, true, verTSkippedWriteChain)
			fixApprove(t, "cli-drift-skipped-write")
			return []verTCheck{{args: []string{"verify", "cli-drift-skipped-write", "-quiet"}, set: func() { broken = true }, code: 3,
				has: []string{"confirm"}, not: []string{"regression"}}}
		}},
		{"the changes at a step not judged for descriptor drift fold into one line", func(t *testing.T) []verTCheck {
			drift := false
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
					out := verTOK("id", "thing-1", "name", "widget", "total", 3)
					if drift {
						out["total"], out["name"] = "three", "gizmo"
					}
					verTWrite(w, out)
					return
				}
				name := "widget"
				if drift {
					name = "gadget"
				}
				verTWrite(w, verTOK("id", "thing-fixed", "name", name))
			}, true, verTUnjudgedChain)
			fixApprove(t, "cli-unjudged")
			return []verTCheck{
				{args: []string{"verify", "cli-unjudged", "-quiet"}, set: func() { drift = true }, code: 1,
					has: []string{"[create] not_judged", "shrt catalog build", "[fetch_other] changed    name"}, not: []string{"[create] changed", "[create] status"}},
				{args: []string{"verify", "cli-unjudged", "-quiet", "-v"}, code: 1, has: []string{"[create] changed"}},
			}
		}},
		{"a gateway answer is not an answer from the service", func(t *testing.T) []verTCheck {
			var down atomic.Bool
			next := 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if down.Load() {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusBadGateway)
					_, _ = w.Write([]byte("<html>gateway</html>"))
					return
				}
				next++
				verTWrite(w, verTOK("id", "thing-"+itoa(next), "name", body["name"]))
			}, false, verTThreeCreatesChain)
			fixApprove(t, "cli-two")
			return []verTCheck{{args: []string{"verify", "cli-two", "-quiet"}, set: func() { down.Store(true) }, code: 3,
				has: []string{"nothing after it got an answer"}, not: []string{"got an answer were compared"}}}
		}},
		{"an rpc dropped in two runs while later steps answer is a finding", func(t *testing.T) []verTCheck {
			var drop, dropAll atomic.Bool
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				if drop.Load() && (r.URL.Path == "/shrt.test.v1.ThingService/Fetch" || dropAll.Load()) {
					hangUp(w)
					return
				}
				verTWrite(w, verTOK("id", "thing-9", "name", "widget"))
			}, false, dropChain)
			fixApprove(t, "cli-drop")
			v := []string{"verify", "cli-drop", "-quiet"}
			return []verTCheck{
				{args: v, set: func() { drop.Store(true) }, code: 3, not: []string{"FINDING"}},
				{args: v, code: 1, has: []string{"FINDING: ", "ThingService/Fetch", "fails this rpc every time while answering others"}},
				{args: v, set: func() { dropAll.Store(true) }, code: 3, not: []string{"FINDING"}},
			}
		}},
		{"a step dropped in two runs while its rpc answers other steps is a finding about that step", func(t *testing.T) []verTCheck {
			var drop atomic.Bool
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if drop.Load() && body["id"] == "thing-9" {
					hangUp(w)
					return
				}
				verTWrite(w, verTOK("id", body["id"], "name", "widget"))
			}, false, verTDropFirstChain, verTDropMiddleChain)
			fixApprove(t, "cli-drop-twice")
			fixApprove(t, "cli-drop-one")
			first, middle := []string{"verify", "cli-drop-twice", "-quiet"}, []string{"verify", "cli-drop-one", "-quiet"}
			return []verTCheck{
				{args: first, set: func() { drop.Store(true) }, code: 3, has: []string{"looks intermittent"}, not: []string{"FINDING"}},
				{args: first, code: 1, has: []string{"FINDING: ", "step 1 fetch", "thing-9"}, not: []string{"fails this rpc every time"}},
				{args: middle, code: 3, has: []string{"looks intermittent", "fetch_b"}, not: []string{"FINDING", "check the backend is up", "stopped or crashed"}},
				{args: middle, code: 1, has: []string{"FINDING: ", "fetch_b", "thing-9", "fetch_a"}, not: []string{"fails this rpc every time"}},
			}
		}},
		{"chain edits are chain changes, not regressions or input changes", func(t *testing.T) []verTCheck {
			names := map[string]any{}
			next := 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
					next++
					id := "thing-" + itoa(next)
					names[id] = body["name"]
					verTWrite(w, verTOK("id", id, "name", body["name"]))
					return
				}
				id, _ := body["id"].(string)
				if name, ok := names[id]; ok {
					verTWrite(w, verTOK("id", id, "name", name))
					return
				}
				verTWrite(w, verTOK("id", id, "name", "widget"))
			}, false, verTRewiredChain)
			fixApprove(t, "cli-rewired")
			fixApprove(t, "cli-thing-flow")
			added := "          - path: name\n            equals: widget\n          - path: id\n            not_empty: true\n"
			v := []string{"verify", "cli-thing-flow", "-quiet", "-save=false"}
			return []verTCheck{
				{args: []string{"verify", "cli-rewired", "-quiet"}, set: verTEdit(t, ".shrt/chains/cli-rewired.yaml", "${create_a.id}", "${create_b.id}"), code: 1,
					has: []string{"chain differs from the confirmed run at fetch body.id (${create_a.id} -> ${create_b.id})", "ERR: drift after a chain change"},
					then: func(t *testing.T, _ string) {
						if !strings.Contains(string(mustRead(t, ".shrt/safespots/cli-rewired.json")), `"body_refs"`) {
							t.Error("the safe spot keeps the body references it was built from")
						}
					}},
				{args: v, set: verTEdit(t, thing, "          - path: name\n            equals: widget\n", added), code: 0,
					has: []string{"chain differs from the confirmed run at fetch expect", "the chain changed since it was confirmed"}, not: []string{"input changed"}},
				{args: v, set: func() {
					raw := string(mustRead(t, thing))
					cut := strings.LastIndex(raw, "not_empty: true")
					writeFile(t, thing, raw[:cut]+"equals: nope"+raw[cut+len("not_empty: true"):])
				}, code: 1, has: []string{"ERR: drift after a chain change"}, not: []string{"input changed"}},
				{args: v, set: verTEdit(t, thing, "not_empty: true", "equals: nope"), code: 1, has: []string{"ERR: drift after a chain change"}, not: []string{"ERR: regression"}},
			}
		}},
		{"fixture names, generated values and expectation vars do not excuse a regression", func(t *testing.T) []verTCheck {
			regressed := false
			srv := newTotalBackend(&regressed)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/cli-fixture-flow.yaml", verTFixtureChain)
			fixApprove(t, "cli-fixture-flow")
			v := func(args ...string) []string {
				return append([]string{"verify", "cli-fixture-flow", "-quiet"}, args...)
			}
			regression := verTCheck{code: 1, has: []string{"ERR: regression", "total"}, not: []string{"with different input"}}
			rows := []verTCheck{{args: v("-var", "tag=second", "-var", "trace=t-2"), code: 0, has: []string{"no drift"}}}
			for i, args := range [][]string{{"-var", "tag=third", "-var", "trace=t-3"}, {"-var", "total=999"}, {"-var", "tag=fourth", "-var", "total=999"}} {
				c := regression
				c.args = v(args...)
				if i == 0 {
					c.set = func() { regressed = true }
				}
				rows = append(rows, c)
			}
			return append(rows,
				verTCheck{args: []string{"verify", "cli-fixture-flow", "-var", "tag=second"}, code: 1, has: []string{"not counted: ", "(-masked lists them)"}, not: []string{"create name"}},
				verTCheck{args: []string{"verify", "cli-fixture-flow", "-var", "tag=third", "-masked"}, code: 1,
					has: []string{"request values differing only in a fixture name or under a volatile path, not compared:\n  create name ("}},
				verTCheck{args: v("-var", "kind=KIND_B", "-var", "tag=fifth"), set: func() { regressed = false }, code: 1,
					has: []string{"ERR: drift with different input", "kind=KIND_B"}, not: []string{"tag=fifth"}},
				verTCheck{args: v("-var", "tag=edited"), set: verTEdit(t, ".shrt/chains/cli-fixture-flow.yaml", "equals: ${vars.total}", "equals: 301"), code: 1,
					has: []string{"chain differs from the confirmed run at fetch expect", "ERR: drift after a chain change"}, not: []string{"the chain file is not what differs", "ERR: regression"}},
				verTCheck{args: v("-var", "tag=edited2", "-var", "kind=KIND_B"), code: 1,
					has: []string{"kind=KIND_B", "fetch expect"}, not: []string{"the chain file is not what differs"}},
			)
		}},
		{"echoes of generated values are masked like fixture names", func(t *testing.T) []verTCheck {
			stale := ""
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				if r.URL.Path == "/shrt.test.v1.ThingService/Create" {
					name := body["name"]
					if stale != "" {
						name = stale
					}
					verTWrite(w, verTOK("id", "thing-1", "name", name))
					return
				}
				verTWrite(w, map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "no thing " + body["id"].(string)}})
			}, false, verTGeneratedChain)
			fixApprove(t, "cli-generated")
			v := []string{"verify", "cli-generated", "-quiet"}
			return []verTCheck{
				{args: v, code: 0, has: []string{"no drift"}},
				{args: v, set: func() {
					spot := &store.SafeSpot{}
					if err := json.Unmarshal(mustRead(t, ".shrt/safespots/cli-generated.json"), spot); err != nil {
						t.Fatal(err)
					}
					var resp map[string]any
					_ = json.Unmarshal(spot.Steps[0].Response, &resp)
					stale, _ = resp["name"].(string)
				}, code: 1, has: []string{"create", "name"}},
			}
		}},
		{"a changed request is named before the response changes and is no regression", func(t *testing.T) []verTCheck {
			approvedThingFlow(t)
			const differs = "request differs from the confirmed run at create name (widget -> gadget)"
			return []verTCheck{
				{args: []string{"verify", "cli-thing-flow", "-quiet"}, set: verTEdit(t, thing, "name: widget\n          kind", "name: gadget\n          kind"), code: 1,
					has: []string{differs}, not: []string{"idempotency_key", "ERR: regression"},
					then: func(t *testing.T, out string) {
						if strings.Index(out, differs) > strings.Index(out, "[create]") {
							t.Errorf("the request differences come before the response changes:\n%s", out)
						}
					}},
				{args: []string{"run", "cli-thing-flow", "-quiet"}, code: 0},
				{args: []string{"confirm", "cli-thing-flow", "-supersede", "-note", "renamed to gadget"}, code: 0,
					has: []string{"request name widget -> gadget", "response name widget -> gadget"}},
			}
		}},
		{"a var, not the chain, is blamed when only a var changed the input", func(t *testing.T) []verTCheck {
			srv := newEchoNameBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/cli-var-flow.yaml", verTVarChain)
			fixApprove(t, "cli-var-flow")
			bad := []string{"its input changed since it was confirmed", "Restore the chain's input"}
			return []verTCheck{
				{args: []string{"verify", "cli-var-flow", "-quiet", "-var", "label=gadget"}, code: 1, has: []string{"label=gadget"}, not: bad},
				{argsFn: func() []string {
					entries, _ := os.ReadDir(".shrt/runs/cli-var-flow")
					for _, e := range entries {
						if strings.Contains(string(mustRead(t, ".shrt/runs/cli-var-flow/"+e.Name())), `"label": "gadget"`) {
							return []string{"verify", "cli-var-flow", "-quiet", "-run", strings.TrimSuffix(e.Name(), ".json")}
						}
					}
					t.Fatal("the -var run was not recorded")
					return nil
				}, code: 1, has: []string{"label=gadget"}, not: bad},
			}
		}},
		{"a var the confirmed run set and this one does not is blamed", func(t *testing.T) []verTCheck {
			srv := newEchoNameBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			writeFile(t, ".shrt/chains/cli-var-flow.yaml", verTVarChain)
			fixApprove(t, "cli-var-flow", "-var", "label=k-b1")
			return []verTCheck{{args: []string{"verify", "cli-var-flow", "-quiet"}, code: 1,
				has: []string{"label=widget, confirmed with k-b1", "-var label=k-b1"},
				not: []string{"its input changed since it was confirmed", "Restore the chain's input", "the chain file changed"}}}
		}},
		{"removing a middle step keeps the fixture echo masked at later steps", func(t *testing.T) []verTCheck {
			seen, next := map[string]bool{}, 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				body := verTBody(r)
				name, _ := body["name"].(string)
				if r.URL.Path == "/shrt.test.v1.ThingService/Fetch" {
					verTWrite(w, verTOK("id", body["id"]))
				} else if seen[strings.ToLower(name)] {
					verTWrite(w, map[string]any{"error": map[string]any{"code": "ALREADY_EXISTS", "message": "name " + name + " already exists"}})
				} else {
					seen[strings.ToLower(name)] = true
					next++
					verTWrite(w, verTOK("id", "thing-"+itoa(next)))
				}
			}, false, verTDupMidChain)
			fixApprove(t, "cli-dupmid")
			noEcho := func(t *testing.T, out string) {
				if listed, _, _ := strings.Cut(out, "values echoing a fixture name, not compared:"); strings.Contains(listed, "error.message") {
					t.Errorf("the refusal message only echoes the fixture name:\n%s", out)
				}
			}
			return []verTCheck{
				{args: []string{"verify", "cli-dupmid", "-quiet", "-masked", "-var", "tag=fresh2"}, code: 1,
					set: verTEdit(t, ".shrt/chains/cli-dupmid.yaml", "    - id: mid\n      call: ThingService/Fetch\n      body: {id: none}\n      expect:\n          - path: error.code\n            equals: OK\n", ""),
					has: []string{"1 change(s)", "dup name"}, then: noEcho},
				{args: []string{"run", "cli-dupmid", "-quiet", "-var", "tag=fresh3"}, code: 0},
				{args: []string{"confirm", "cli-dupmid", "-supersede", "-note", "mid removed"}, code: 0, then: noEcho},
			}
		}},
		{"verify -quiet still prints step warnings", func(t *testing.T) []verTCheck {
			var extra atomic.Bool
			next := 0
			verTServe(t, func(w http.ResponseWriter, r *http.Request) {
				next++
				out := verTOK("id", "thing-"+itoa(next), "name", "widget")
				if extra.Load() {
					out["tier"] = "gold"
				}
				verTWrite(w, out)
			}, false)
			fixApprove(t, "cli-thing-flow")
			return []verTCheck{{args: []string{"verify", "cli-thing-flow", "-quiet"}, set: func() { extra.Store(true) }, code: 0, has: []string{"warning", "tier"}}}
		}},
		{"a latency regression warns and fails only when configured", func(t *testing.T) []verTCheck {
			var delay atomic.Int64
			srv := newSlowFetchBackend(&delay)
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			fixApprove(t, "cli-thing-flow")
			cfg := string(mustRead(t, ".shrt/config.yaml"))
			v := []string{"verify", "cli-thing-flow", "-quiet"}
			return []verTCheck{
				{args: append(v, "-latency"), set: func() { writeFile(t, ".shrt/config.yaml", cfg+"latency:\n    floor_ms: 100\n"); delay.Store(150) }, code: 0,
					has: []string{"LATENCY: Fetch at step fetch", "re-sent 2 more time(s)", "latency per step"}},
				{args: v, set: func() { writeFile(t, ".shrt/config.yaml", cfg+"latency:\n    floor_ms: 100\n    fail: true\n") }, code: 1, has: []string{"latency regression"}},
				{args: v, set: func() { delay.Store(0) }, code: 0, not: []string{"LATENCY"}},
			}
		}},
		{"verify refuses what the approval does not cover", func(t *testing.T) []verTCheck {
			approvedThingFlow(t)
			const spot = ".shrt/safespots/cli-thing-flow.json"
			orig := string(mustRead(t, spot))
			restore := func() { writeFile(t, spot, orig) }
			v := []string{"verify", "cli-thing-flow", "-quiet", "-save=false"}
			return []verTCheck{
				{args: v, set: verTEdit(t, spot, `"name": "widget"`, `"name": "gadget"`), code: 1, has: []string{"does not match", "-supersede"}},
				{args: v, set: func() {
					restore()
					verTEdit(t, spot, `"confirmed_by": "alice@example.test"`, `"confirmed_by": "mallory@example.test"`)()
				}, code: 1, has: []string{"does not match"}},
				{args: []string{"verify", "cli-thing-flow", "-save=false"}, set: func() {
					s := &store.SafeSpot{}
					if err := json.Unmarshal([]byte(orig), s); err != nil {
						t.Fatal(err)
					}
					s.Digest = s.ContentDigest()
					raw, _ := json.MarshalIndent(s, "", "  ")
					writeFile(t, spot, string(raw))
				}, code: 0, has: []string{"older kind", "confirmed_by"}},
				{args: []string{"verify", "cli-thing-flow", "-quiet"}, set: func() {
					restore()
					verTEdit(t, thing, "name: cli-thing-flow\n", "name: cli-thing-flow\nvolatile:\n    - '**'\n")()
				}, code: 1, has: []string{"did not approve: **", "not counted: "}},
			}
		}},
		{"verify without an approved safe spot says what to do", func(t *testing.T) []verTCheck {
			srv := newFakeCLIBackend()
			t.Cleanup(srv.Close)
			chdirToFreshCLIWorkspace(t, srv.URL)
			v := []string{"verify", "cli-thing-flow", "-quiet"}
			return []verTCheck{
				{args: v, code: 1, has: []string{"ERR: chain cli-thing-flow has no safe spot:", ".shrt/safespots/cli-thing-flow.json"}, not: []string{"file does not exist"}},
				{args: []string{"run", "cli-thing-flow", "-quiet"}, code: 0},
				{args: []string{"confirm", "cli-thing-flow", "-note", "fetch returns the created name"}, code: 0},
				{args: v, code: 1, has: []string{"shrt confirm cli-thing-flow -approve -by <their email>"}, not: []string{"<name>"}},
			}
		}},
		{"a replay that never reached the backend could not verify", func(t *testing.T) []verTCheck {
			srv := approvedThingFlow(t)
			return []verTCheck{{args: []string{"verify", "cli-thing-flow", "-quiet", "-save=false"}, set: srv.Close, code: 3,
				has: []string{"could not verify"}, not: []string{"ERR: regression", "change(s) vs safe spot", "want=passed"}}}
		}},
		{"verify -run latest names the run it diffed", func(t *testing.T) []verTCheck {
			approvedThingFlow(t)
			e, err := loadEnv(false)
			if err != nil {
				t.Fatal(err)
			}
			latest := func() string {
				rec, err := e.store.LatestRun("cli-thing-flow")
				if err != nil {
					t.Fatal(err)
				}
				return rec.RunID
			}
			own := latest()
			v := []string{"verify", "cli-thing-flow", "-quiet", "-run", "latest"}
			return []verTCheck{
				{args: v, code: 0, has: []string{"-run latest is run " + own, "IS the safe spot's own run"}},
				{args: []string{"run", "cli-thing-flow", "-quiet"}, code: 0},
				{args: v, code: 0, not: []string{"IS the safe spot's own run"}, then: func(t *testing.T, out string) {
					if !strings.Contains(out, "-run latest is run "+latest()) {
						t.Errorf("verify -run latest names the newest run:\n%s", out)
					}
				}},
			}
		}},
		{"principal checking off keeps drift from being called a regression", func(t *testing.T) []verTCheck {
			approvedThingFlow(t)
			e, err := loadEnv(true)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := e.store.LatestRun("cli-thing-flow")
			if err != nil {
				t.Fatal(err)
			}
			const path = ".shrt/safespots/cli-thing-flow.json"
			spot := map[string]any{}
			if err := json.Unmarshal(mustRead(t, path), &spot); err != nil {
				t.Fatal(err)
			}
			for _, s := range spot["steps"].([]any) {
				step := s.(map[string]any)
				step["auth_profile"] = "default"
				delete(step, "auth_principal")
			}
			raw, _ := json.MarshalIndent(spot, "", "  ")
			writeFile(t, path, string(raw))
			resealSafeSpot(t, path)
			rec.RunID = "20990101T000000Z-0badc0de"
			for _, s := range rec.Steps {
				s.AuthProfile, s.AuthPrincipal = "default", "cccc3333dddd4444"
			}
			fetch := rec.Steps[len(rec.Steps)-1]
			fetch.Response = json.RawMessage(strings.Replace(string(fetch.Response), `"widget"`, `"gadget"`, 1))
			if _, err := e.store.SaveRun(rec); err != nil {
				t.Fatal(err)
			}
			return []verTCheck{{args: []string{"verify", "cli-thing-flow", "-run", rec.RunID}, code: 1,
				has: []string{"principal checking is off", "-supersede"}, not: []string{"ERR: regression"}}}
		}},
	} {
		t.Run(sc.name, func(t *testing.T) { verTRun(t, sc.setup(t)) })
	}
}
