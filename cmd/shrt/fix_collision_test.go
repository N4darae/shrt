package main

import (
	"os"
	"strings"
	"testing"
)

const fixOtherChain = `apiVersion: shrt/v1
name: cli-other
vars:
    label: first
steps:
    - id: make
      call: ThingService/Create
      body:
          name: widget ${vars.label}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

const fixTwoFixtureChain = `apiVersion: shrt/v1
name: cli-twofix
vars:
    tag: first
    stag: s0
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
          meta:
              source: src-${vars.stag}
      expect:
          - path: error.code
            equals: OK
`

const fixGeneratedChain = `apiVersion: shrt/v1
name: cli-genunique
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget-${uuid}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

const fixUUIDChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag} ${uuid}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

const fixErredChain = `apiVersion: shrt/v1
name: cli-erred
vars:
    tag: base
steps:
    - id: create
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
      expect:
          - path: transport.code
            equals: internal
    - id: create_again
      call: ThingService/Create
      body:
          name: widget ${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

const fixTwoTagChain = `apiVersion: shrt/v1
name: cli-two
vars:
    tag: base
steps:
    - id: create
      call: ThingService/Create
      body:
          name: sku-${vars.tag}-
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: sku-${vars.tag}-2-
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`

const fixLiteralSourceChain = `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: NAME
          meta:
              source: fixed-source-ao3
      expect:
          - path: error.code
            equals: OK
`

func fixLiteralChain(name string) string {
	return `apiVersion: shrt/v1
name: cli-unique
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: '` + name + `'
          idempotency_key: key ${vars.tag}
          kind: KIND_A
      expect:
          - path: error.code
            equals: OK
`
}

func fixPairChain(first, second string) string {
	return `apiVersion: shrt/v1
name: cli-repeat
vars:
    tag: first
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}` + first + `
      expect:
          - path: error.code
            equals: OK
    - id: create_2
      call: ThingService/Create
      body:
          name: ` + second + `
      expect:
          - path: error.code
            equals: OK
`
}

const fixIdemLateChain = `apiVersion: shrt/v1
name: cli-idem-late
steps:
    - id: pre
      call: ThingService/Fetch
      body:
          id: pre
      expect:
          - path: error.code
            equals: OK
    - id: own
      call: ThingService/Create
      body:
          name: owner-${uuid}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body:
          name: ${own.id}
          idempotency_key: fixed-key-late-1
      expect:
          - path: error.code
            equals: OK
`

const fixIdemOwnedChain = `apiVersion: shrt/v1
name: cli-idem-owned
vars:
    tag: one
steps:
    - id: own
      call: ThingService/Create
      body:
          name: owner-${vars.tag}
      expect:
          - path: error.code
            equals: OK
    - id: create
      call: ThingService/Create
      body:
          name: ${own.id}
          idempotency_key: k-${vars.tag}
      expect:
          - path: error.code
            equals: OK
`

func fixIdemChain(key string) string {
	return `apiVersion: shrt/v1
name: cli-idem
vars:
    tag: a
    ik: key-one
steps:
    - id: create
      call: ThingService/Create
      body:
          name: w-${vars.tag}
          idempotency_key: ` + key + `
      expect:
          - path: error.code
            equals: OK
    - id: fetch
      call: ThingService/Fetch
      body:
          id: ${create.id}
      expect:
          - path: name
            equals: w-${vars.tag}
`
}

type fixStep struct {
	cmd        string
	args       []string
	do         func(*fixThing)
	want       string
	code       int
	has, lacks []string
	latestRun  string
}

func fixVerdict(out string, err error) string {
	if err == nil {
		return "pass"
	}
	text := out + "\n" + err.Error()
	switch {
	case strings.Contains(out, "FINDING"):
		return "finding"
	case strings.HasPrefix(err.Error(), "regression"):
		return "regression"
	case strings.Contains(text, "CHAIN DEFECT") || strings.Contains(text, "collides with itself"):
		return "defect"
	case strings.Contains(text, "fixture reused"):
		return "reused"
	case strings.Contains(text, "fixture collision"):
		return "collision"
	}
	return "failed"
}

func fixRun(args ...string) fixStep    { return fixStep{cmd: "run", args: args, code: -1} }
func fixVerify(args ...string) fixStep { return fixStep{cmd: "verify", args: args, code: -1} }

func (s fixStep) is(want string, code int, has ...string) fixStep {
	s.want, s.code, s.has = want, code, has
	return s
}

func (s fixStep) not(lacks ...string) fixStep {
	s.lacks = lacks
	return s
}

func fixDo(do func(*fixThing)) fixStep { return fixStep{do: do} }

func fixApproveStep(args ...string) fixStep { return fixStep{cmd: "approve", args: args} }

func fixForeign(name string) fixStep { return fixStep{cmd: "foreign", args: []string{name}} }

func fixRename(file string, pairs ...string) fixStep {
	return fixStep{cmd: "edit", args: append([]string{file}, pairs...)}
}

func TestFixtureCollisionReuseAndChainDefectVerdicts(t *testing.T) {
	uniq := func() *fixThing { return &fixThing{unique: "name"} }
	registered := func() *fixThing {
		return &fixThing{unique: "name", dupCode: "REJECTED", dupMsg: "name %s is already registered"}
	}
	refuseAfter := func(n int, msg string) func() *fixThing {
		return func() *fixThing { return &fixThing{refuseAfter: n, refuseMsg: msg} }
	}
	idem := func() *fixThing { return &fixThing{idem: true} }
	refuse := fixDo(func(f *fixThing) { f.refuse = true })
	uv := func(tag string) fixStep { return fixVerify("cli-unique", "-quiet", "-var", "tag="+tag) }
	ur := func(tag string) fixStep { return fixRun("cli-unique", "-quiet", "-var", "tag="+tag) }
	inRunWrong := []string{"fixture collision", "created by something else", "points at the backend", "re-run with a fresh value"}
	literalWrong := []string{"fixture collision", "built from tag", "collides with itself", "CHAIN DEFECT"}
	for _, tc := range []struct {
		name   string
		shop   func() *fixThing
		chains []string
		steps  []fixStep
	}{
		{"a conflict with a record another client created is a fixture collision", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), fixForeign("widget foreign7"),
			uv("foreign7").is("collision", 3, "-var tag=").not("fixture reused"),
		}},
		{"run names the collision and a fresh -var", uniq, []string{uniqueNameChain}, []fixStep{
			fixForeign("widget foreign8"), ur("foreign8").is("collision", -1, "shrt run cli-unique -var tag="),
		}},
		{"an earlier refused run did not use the fixture", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), fixForeign("widget dup"), ur("dup"),
			uv("dup").is("collision", 3).not("fixture reused"),
		}},
		{"a value this chain's own earlier run sent is a reused fixture", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), uv("same1").is("pass", 0),
			uv("same1").is("reused", 3, "tag=same1", "-var tag=").not("regression"),
		}},
		{"a reused fixture is found in a run that named the step otherwise", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), uv("same1").is("pass", 0),
			fixRename(".shrt/chains/cli-unique.yaml", "- id: create\n", "- id: make_thing\n", "${create.id}", "${make_thing.id}"),
			uv("same1").is("reused", 3).not("no recorded run of this chain used that value"),
		}},
		{"a value another chain created is a reused fixture", uniq, []string{uniqueNameChain, fixOtherChain}, []fixStep{
			fixApproveStep("cli-unique"),
			fixRun("cli-other", "-quiet", "-var", "label=shared1").is("pass", 0),
			uv("shared1").is("reused", 3, "cli-other").not("FINDING"),
			fixRun("cli-other", "-quiet", "-var", "label=shared2").is("pass", 0),
			uv("shared2").is("reused", 3, "cli-other").not("FINDING"),
		}},
		{"a value a run sent with an unknown outcome is a reused fixture", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), fixDo(func(f *fixThing) { f.drop = true }), uv("dropped1"), uv("dropped2"),
			fixDo(func(f *fixThing) { f.drop = false }),
			uv("dropped1").is("reused", 3, "whether the call took effect is unknown").not("FINDING", "something else"),
			uv("dropped2").is("reused", 3, "whether the call took effect is unknown").not("FINDING", "something else"),
		}},
		{"only the var of the conflicting field is blamed", func() *fixThing {
			return &fixThing{unique: "source", dupCode: "SOURCE_TAKEN", dupMsg: "already in use"}
		}, []string{fixTwoFixtureChain}, []fixStep{
			fixApproveStep("cli-twofix", "-var", "tag=A2", "-var", "stag=used1"),
			fixDo(func(f *fixThing) { f.seen["src-FRESH365"] = true }),
			fixVerify("cli-twofix", "-quiet", "-var", "tag=A2", "-var", "stag=FRESH365").is("collision", 3, "stag=FRESH365").not("fixture reused"),
		}},
		{"a collision on a purely generated value says a plain re-run is fresh", func() *fixThing { return &fixThing{} }, []string{fixGeneratedChain}, []fixStep{
			fixApproveStep("cli-genunique"), refuse,
			fixRun("cli-genunique", "-quiet").is("collision", -1, "lain re-run generates a fresh value: shrt run cli-genunique").not(" \n"),
		}},
		{"a verify collision on a purely generated value says a plain re-run is fresh", func() *fixThing { return &fixThing{} }, []string{fixGeneratedChain}, []fixStep{
			fixApproveStep("cli-genunique"), refuse,
			fixVerify("cli-genunique", "-quiet").is("collision", -1, "lain re-run generates a fresh value: shrt verify cli-genunique").not(" \n"),
		}},
		{"two fresh var values refused in a row stay a fixture collision", uniq, []string{uniqueNameChain}, []fixStep{
			fixApproveStep("cli-unique"), refuse, uv("fresh1").is("collision", 3),
			uv("fresh2").is("collision", 3, "tag=fresh1", "tag=fresh2", "points at the backend unless another client uses the same values").not("FINDING"),
		}},
		{"a conflict on two uuid-built values in a row is a finding", uniq, []string{fixUUIDChain}, []fixStep{
			fixApproveStep("cli-unique"), refuse, uv("fresh1").is("collision", 3), uv("fresh2").is("finding", 1),
		}},
		{"a conflict with what an earlier step of the run stored behind a server error is no fixture collision", func() *fixThing {
			return &fixThing{unique: "name", failStored: true}
		}, []string{fixErredChain}, []fixStep{
			fixRun("cli-erred", "-quiet", "-var", "tag=e1").is("failed", 1, "step 1 create of this run sent that value and got a server error", "this run's own record").
				not("fixture collision", "created by something else", "re-run with a fresh value"),
		}},
		{"a collision with another tag of the same chain names the run and step", uniq, []string{fixTwoTagChain}, []fixStep{
			fixRun("cli-two", "-quiet", "-var", "tag=x").is("pass", 0),
			fixRun("cli-two", "-quiet", "-var", "tag=x-2").is("", 1, `already sent that value at step "create_2" with tag=x`).not("no recorded run of this chain used that value"),
		}},
		{"a literal collision is a chain defect", uniq, []string{fixLiteralChain("fixed-widget")}, []fixStep{
			fixApproveStep("cli-unique"),
			uv("l9").is("defect", 1, "collides with itself", "name is the literal fixed-widget", "name: name-${vars.tag}", "CHAIN DEFECT").not("fixture collision"),
			ur("two").is("defect", -1).not("fixture collision"),
		}},
		{"a one-character literal that collides is a chain defect, labelled for the gate and under -json", registered, []string{fixLiteralChain("@")}, []fixStep{
			ur("one"), ur("two").is("defect", 1, "name is the literal @", "\n  CHAIN DEFECT: the chain collides with itself"),
			fixRun("cli-unique", "-json", "-var", "tag=three").is("defect", 1, "chain defect in cli-unique: the chain collides with itself"),
		}},
		{"a literal accepted again only after a backend reset still collides with itself", registered, []string{fixLiteralChain("fixed-widget")}, []fixStep{
			ur("a1"), ur("a2"), fixDo(func(f *fixThing) { f.seen = map[string]bool{} }), ur("a3"), ur("a4").is("defect", -1),
		}},
		{"a literal earlier runs sent and had accepted is not a self-collision", refuseAfter(3, "duplicate record"),
			[]string{strings.Replace(fixLiteralSourceChain, "NAME", "n-${uuid}", 1)}, []fixStep{
				fixApproveStep("cli-unique"), fixRun("cli-unique", "-quiet").is("pass", 0), fixRun("cli-unique", "-quiet").is("pass", 0),
				fixVerify("cli-unique", "-quiet").is("regression", 1).not("CHAIN DEFECT", "collides with itself"),
			}},
		{"a refusal quoting a literal earlier runs had accepted is a regression, not a var collision",
			refuseAfter(3, "SourceTaken: source fixed-source-ao3 already exists"),
			[]string{strings.Replace(fixLiteralSourceChain, "NAME", "n-${vars.tag}", 1)}, []fixStep{
				fixApproveStep("cli-unique"), ur("a2").is("pass", 0), ur("a3").is("pass", 0),
				uv("m4").is("regression", 1).not(literalWrong...), uv("m5").is("regression", 1).not(literalWrong...),
			}},
		{"a conflict naming no field is not blamed on a uuid field", refuseAfter(1, "duplicate record"),
			[]string{strings.Replace(fixLiteralSourceChain, "NAME", "n-${uuid}", 1)}, []fixStep{
				fixApproveStep("cli-unique"),
				fixVerify("cli-unique", "-quiet").is("defect", -1, "meta.source is the literal fixed-source-ao3").not("on a field built from ${uuid}", "FINDING"),
				fixVerify("cli-unique", "-quiet").is("defect", -1, "meta.source is the literal fixed-source-ao3").not("on a field built from ${uuid}", "FINDING"),
			}},
		{"a collision with an earlier step of the same run is a chain defect", uniq,
			[]string{strings.Replace(fixPairChain("\n          kind: KIND_A", "w-${vars.tag}\n          kind: KIND_A"), "cli-repeat", "cli-twice", 1)}, []fixStep{
				fixRun("cli-twice", "-quiet", "-var", "tag=f1").is("defect", 1, "collides with itself", `step "create"`, "w-f1", "a fresh -var does not help").not(inRunWrong...),
				fixRun("cli-twice", "-quiet", "-var", "tag=f2").is("defect", 1, "collides with itself", `step "create"`, "w-f2", "a fresh -var does not help").not(inRunWrong...),
			}},
		{"an in-run repeat of a deliberate reference the safe spot had accepted is a regression",
			func() *fixThing {
				return &fixThing{refuseEven: true, refuseMsg: "DuplicateKey: idempotency key already exists"}
			},
			[]string{fixPairChain("\n          idempotency_key: ${uuid}", "w2-${vars.tag}\n          idempotency_key: ${steps.create.request.idempotency_key}")}, []fixStep{
				fixApproveStep("cli-repeat"),
				fixVerify("cli-repeat", "-quiet", "-var", "tag=v2").is("regression", 1).not("CHAIN DEFECT", "collides with itself"),
			}},
		{"an in-run repeat of the same value the safe spot had accepted is a regression",
			func() *fixThing { return &fixThing{refuseEven: true, refuseMsg: "NameTaken: name already exists"} },
			[]string{fixPairChain("", "w-${vars.tag}")}, []fixStep{
				fixApproveStep("cli-repeat"),
				fixVerify("cli-repeat", "-quiet", "-var", "tag=v2").is("regression", 1).not("CHAIN DEFECT", "collides with itself"),
			}},
		{"a literal key replay after an earlier regression is named in a note", func() *fixThing { return &fixThing{idem: true, fetchName: "before"} },
			[]string{fixIdemLateChain}, []fixStep{
				fixApproveStep("cli-idem-late"), fixDo(func(f *fixThing) { f.fetchName = "after" }),
				fixVerify("cli-idem-late", "-quiet").is("regression", 1, `note: step "create"`, "idempotent replay", "fixed-key-late-1"),
			}},
		{"a key an earlier recorded run sent is a fixture reuse, not a regression", idem, []string{fixIdemOwnedChain}, []fixStep{
			fixApproveStep("cli-idem-owned"), fixRun("cli-idem-owned", "-quiet", "-var", "tag=two").is("pass", 0),
			func() fixStep {
				s := fixVerify("cli-idem-owned", "-quiet", "-var", "tag=two").is("reused", 3, "k-two", "-var tag=").not("confirmed run")
				s.latestRun = "cli-idem-owned"
				return s
			}(),
		}},
		{"a literal idempotency key replay is a chain defect, not a regression", idem, []string{fixIdemChain("fixed-key-ao-1")}, []fixStep{
			fixApproveStep("cli-idem"),
			fixVerify("cli-idem", "-quiet", "-var", "tag=b").is("defect", 1, "idempotency_key", "fixed-key-ao-1", "${uuid}", "CHAIN DEFECT: "),
		}},
		{"a var idempotency key reused from the confirmed run is a fixture reuse", idem, []string{fixIdemChain("${vars.ik}")}, []fixStep{
			fixApproveStep("cli-idem"),
			fixVerify("cli-idem", "-quiet", "-var", "tag=b").is("reused", 3, "-var ik="),
			fixVerify("cli-idem", "-quiet", "-var", "tag=c", "-var", "ik=key-two").not("fixture reused"),
		}},
		{"a dropped rpc is a finding in the first run after its step was renamed", func() *fixThing { return &fixThing{} }, []string{dropChain}, []fixStep{
			fixApproveStep("cli-drop"), fixDo(func(f *fixThing) { f.dropFetch = true }),
			fixVerify("cli-drop", "-quiet").is("", 3),
			fixRename(".shrt/chains/cli-drop.yaml", "- id: fetch\n", "- id: fetch_thing\n"),
			fixVerify("cli-drop", "-quiet").is("finding", 1, "fetch_thing", "fails this rpc every time while answering others"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shop := tc.shop()
			srv := fixWorkspace(t, shop, tc.chains...)
			for i, s := range tc.steps {
				switch s.cmd {
				case "":
					shop.set(s.do)
					continue
				case "approve":
					fixApprove(t, s.args[0], s.args[1:]...)
					continue
				case "foreign":
					createForeignThing(t, srv.URL, s.args[0])
					continue
				case "edit":
					raw, err := os.ReadFile(s.args[0])
					if err != nil {
						t.Fatal(err)
					}
					writeFile(t, s.args[0], strings.NewReplacer(s.args[1:]...).Replace(string(raw)))
					continue
				}
				has := s.has
				if s.latestRun != "" {
					has = append(has, latestRunID(t, s.latestRun))
				}
				out, err := fixCmd(t, s.cmd, s.args...)
				text := out
				if err != nil {
					text += "\n" + err.Error()
				}
				if got := fixVerdict(out, err); s.want != "" && got != s.want {
					t.Fatalf("step %d %s %v: verdict %s, want %s\n%s", i, s.cmd, s.args, got, s.want, text)
				}
				if s.code >= 0 && exitCodeOf(err) != s.code {
					t.Fatalf("step %d %s %v: exit %d, want %d\n%s", i, s.cmd, s.args, exitCodeOf(err), s.code, text)
				}
				for _, w := range has {
					if !strings.Contains(text, w) {
						t.Errorf("step %d %s %v: want %q in:\n%s", i, s.cmd, s.args, w, text)
					}
				}
				for _, w := range s.lacks {
					if strings.Contains(text, w) {
						t.Errorf("step %d %s %v: %q must not appear in:\n%s", i, s.cmd, s.args, w, text)
					}
				}
			}
		})
	}
}
