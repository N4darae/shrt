package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func TestApproveRefusesWhenTheChainChangedSinceTheProposal(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	path := ".shrt/chains/cli-thing-flow.yaml"
	original := strings.Replace(string(mustRead(t, path)), "      call: ThingService/Fetch\n", "      call: ThingService/Fetch\n      skip_auth: true\n", 1)
	writeFile(t, path, original)
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
	})
	edited := strings.Replace(original, "kind: KIND_A", "kind: KIND_B", 1)
	if edited == original {
		t.Fatal("fixture chain has no kind: KIND_A to edit")
	}
	writeFile(t, path, edited)
	var err error
	captureStdout(t, func() {
		err = runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"})
	})
	if err == nil {
		t.Fatal("approve wrote a safe spot for a chain that is not the one the proposal was run from")
	}
	for _, want := range []string{"refusing to approve cli-thing-flow", "step create: body kind", "propose that run"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "step fetch") {
		t.Fatalf("only create changed, the refusal must not name fetch:\n%v", err)
	}
	if _, serr := os.Stat(".shrt/safespots/cli-thing-flow.json"); serr == nil {
		t.Fatal("a refused approval must not write the safe spot")
	}
	writeFile(t, path, original)
	captureStdout(t, func() {
		err = runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"})
	})
	if err != nil {
		t.Fatalf("the chain is back as proposed, approve must succeed: %v", err)
	}
}

func TestStepDiffersReadsANoneAuthProfileAsSkipAuthOrALoginCall(t *testing.T) {
	c := &chain.Chain{Name: "c"}
	for _, s := range []*chain.Step{
		{ID: "no_token", Call: "ThingService/Fetch", SkipAuth: true},
		{ID: "login", Call: "AuthService/Login"},
	} {
		st := &runner.StepRecord{ID: s.ID, Call: s.Call, AuthProfile: runner.NoAuthProfile}
		if why := stepDiffers(st, s, c, nil); why != "" {
			t.Fatalf("%s ran with no auth profile, as the chain says, yet stepDiffers reports %q", s.ID, why)
		}
	}
	st := &runner.StepRecord{ID: "fetch", Call: "ThingService/Fetch", AuthProfile: "clerk"}
	if why := stepDiffers(st, &chain.Step{ID: "fetch", Call: "ThingService/Fetch"}, c, nil); !strings.Contains(why, "auth profile clerk -> default") {
		t.Fatalf("a step that ran as clerk and now runs as default differs, got %q", why)
	}
}

func TestApproverEmailNeedsADottedDomain(t *testing.T) {
	for _, bad := range []string{"a@b", "alice@localhost", "a@b.", "a@.b", "a@b..c"} {
		if _, err := approverEmail(bad); err == nil {
			t.Errorf("-by %q was accepted as an email", bad)
		}
	}
	for _, good := range []string{"lab-tester@example.test", "alice@example.com", "first.last@mail.example.co"} {
		if _, err := approverEmail(good); err != nil {
			t.Errorf("-by %q was refused: %v", good, err)
		}
	}
}

func TestConfirmAllProposesPassingRunsAndApprovesThemAfterAYes(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	path := ".shrt/chains/cli-thing-flow.yaml"
	original := strings.Replace(string(mustRead(t, path)), "      call: ThingService/Fetch\n", "      call: ThingService/Fetch\n      skip_auth: true\n", 1)
	writeFile(t, path, original)
	writeFile(t, ".shrt/chains/cli-thing-again.yaml", strings.Replace(original, "name: cli-thing-flow", "name: cli-thing-again", 1))
	writeFile(t, ".shrt/chains/cli-thing-unrun.yaml", strings.Replace(original, "name: cli-thing-flow", "name: cli-thing-unrun", 1))
	captureStdout(t, func() {
		for _, c := range []string{"cli-thing-flow", "cli-thing-again"} {
			if err := runRun(ctx, []string{c, "-quiet"}); err != nil {
				t.Fatalf("shrt run %s: %v", c, err)
			}
		}
	})

	var err error
	out := captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-note", "fetch returns the created name"}) })
	if err != nil {
		t.Fatalf("confirm -all: %v\n%s", err, out)
	}
	for _, want := range []string{"proposed 2 chain(s), NOT safe spots yet; what the proposer checked: fetch returns the created name",
		"| cli-thing-flow | `", "| cli-thing-again | `", "| 2/2 | none |\n",
		"\n**Check before approving:**\n- `cli-thing-again`, `cli-thing-flow`: no earlier passing run to compare with\n",
		"skip     no run recorded: cli-thing-unrun", ".shrt/safespots/pending/<chain>.md",
		"only after the user says yes to every one:  shrt confirm -all -approve -by <their email>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm -all output lacks %q:\n%s", want, out)
		}
	}
	if _, serr := os.Stat(".shrt/safespots/cli-thing-flow.json"); serr == nil {
		t.Fatal("proposing writes no safe spot")
	}

	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-approve"}) })
	if err == nil {
		t.Fatalf("-all -approve without -by records no approver, so it is refused:\n%s", out)
	}
	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-approve", "-by", "alice@example.test"}) })
	if err != nil || strings.Count(out, "\n") != 2 || !strings.Contains(out, "approved cli-thing-again: safe spot from run ") {
		t.Fatalf("-all -approve approves each pending proposal in one line: %v\n%s", err, out)
	}
	for _, c := range []string{"cli-thing-flow", "cli-thing-again"} {
		if _, serr := os.Stat(".shrt/safespots/" + c + ".json"); serr != nil {
			t.Fatalf("approved %s but wrote no safe spot: %v", c, serr)
		}
	}

	out = captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "-note", "again"}) })
	if err != nil || !strings.Contains(out, "skip     safe spot unchanged: cli-thing-again, cli-thing-flow") || !strings.Contains(out, "nothing proposed") {
		t.Fatalf("a chain whose latest run is its safe spot is not proposed again: %v\n%s", err, out)
	}
	captureStdout(t, func() { err = runConfirm(ctx, []string{"-all", "cli-thing-flow", "-note", "x"}) })
	if err == nil {
		t.Fatal("-all names no chain")
	}
}

func TestTheProposalTableListsOnlyTheRowsWithSomethingToCheck(t *testing.T) {
	row := func(chain, check string) store.ProposalRow {
		return store.ProposalRow{Chain: chain, Run: "r-" + chain, Steps: "2/2", Refusals: "none", Check: check}
	}
	plain := proposalTable([]store.ProposalRow{row("a", "none"), row("b", "")})
	if strings.Contains(plain, "check") || strings.Contains(plain, "Check") || strings.Count(plain, "\n") != 4 {
		t.Fatalf("with nothing to check, the table has no check column and nothing under it:\n%s", plain)
	}
	mixed := proposalTable([]store.ProposalRow{row("a", "redacted `token`"), row("b", "none"), row("c", "redacted `token`"), row("d", "1 warning(s)")})
	want := "| chain | run | steps passed | refusals asserted |\n|---|---|---|---|\n" +
		"| a | `r-a` | 2/2 | none |\n| b | `r-b` | 2/2 | none |\n| c | `r-c` | 2/2 | none |\n| d | `r-d` | 2/2 | none |\n" +
		"\n**Check before approving:**\n- `a`, `c`: redacted `token`\n- `d`: 1 warning(s)\n"
	if mixed != want {
		t.Fatalf("the rows needing a check are listed after the table, grouped by what to check:\n%s\nwant:\n%s", mixed, want)
	}
}

func echoRecord(t *testing.T, id string, steps ...[3]any) *runner.Record {
	t.Helper()
	rec := &runner.Record{Chain: "echo", RunID: id, Status: runner.StatusPassed, Target: "http://t"}
	for i, s := range steps {
		req, err := json.Marshal(s[1])
		if err != nil {
			t.Fatal(err)
		}
		resp, err := json.Marshal(s[2])
		if err != nil {
			t.Fatal(err)
		}
		rec.Steps = append(rec.Steps, &runner.StepRecord{Index: i + 1, ID: s[0].(string), Call: "ThingService/Create",
			Status: runner.StatusPassed, Request: req, Response: resp})
	}
	return rec
}

func echoChain() *chain.Chain {
	return &chain.Chain{Name: "echo", Vars: map[string]any{"tag": "a"}, Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "widget ${vars.tag}"}},
		{ID: "refuse", Call: "ThingService/Create", Body: map[string]any{"name": "other"}},
	}}
}

func TestConfirmDoesNotWarnAboutAValueThatOnlyEchoesTheFixtureName(t *testing.T) {
	prev := echoRecord(t, "r1",
		[3]any{"create", map[string]any{"name": "widget first"}, map[string]any{"name": "widget first", "label": "made widget first"}},
		[3]any{"refuse", map[string]any{"name": "other"}, map[string]any{"message": "no stock for 5"}})
	rec := echoRecord(t, "r2",
		[3]any{"create", map[string]any{"name": "widget second"}, map[string]any{"name": "widget second", "label": "made widget second"}},
		[3]any{"refuse", map[string]any{"name": "other"}, map[string]any{"message": "no stock for 7"}})
	got, _ := unstableAgainstSpot(prev, rec, nil, nil, echoChain(), nil)
	if strings.Join(got, ",") != "refuse message: no stock for 5 -> no stock for 7" {
		t.Fatalf("verify masks a fixture echo, so confirm must not warn about it; only the real difference is unstable, got %v", got)
	}
}

func TestConfirmRenameFromCarriesAnIdenticalChainsSafeSpot(t *testing.T) {
	renameThingFlow(t, nil)
	out, err := confirmRename(t, "-by", "bob@example.test")
	if err != nil {
		t.Fatalf("an identical chain under a new name keeps its safe spot: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no new approval is needed") {
		t.Fatalf("the output must say why no approval is asked for:\n%s", out)
	}
	if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); !os.IsNotExist(err) {
		t.Fatalf("the old safe spot file must be moved, got %v", err)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	spot, err := e.store.LoadSafeSpot("cli-renamed")
	if err != nil {
		t.Fatal(err)
	}
	if spot.Chain != "cli-renamed" || spot.ConfirmedBy != "alice@example.test" || spot.DigestKind() != store.DigestCurrent {
		t.Fatalf("the approval provenance is kept and the digest re-sealed, got %+v", spot)
	}
	if len(spot.Renamed) != 1 || spot.Renamed[0].From != "cli-thing-flow" || spot.Renamed[0].By != "bob@example.test" {
		t.Fatalf("the rename is recorded, got %+v", spot.Renamed)
	}
	var verr error
	vout := captureStdout(t, func() { verr = runVerify(context.Background(), []string{"cli-renamed", "-quiet"}) })
	if verr != nil {
		t.Fatalf("verify of the renamed chain passes against the carried safe spot: %v\n%s", verr, vout)
	}
}

func TestCLIConfirmComparesOnlyWithAnEarlierRunOfTheSameTarget(t *testing.T) {
	first := newEchoNameBackend()
	t.Cleanup(first.Close)
	chdirToFreshCLIWorkspace(t, first.URL)
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	second := newEchoNameBackend()
	t.Cleanup(second.Close)
	cfg := string(mustRead(t, ".shrt/config.yaml"))
	writeFile(t, ".shrt/config.yaml", strings.Replace(cfg, first.URL, second.URL, 1))
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
	})
	var err error
	out := captureStdout(t, func() {
		err = runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "create echoes the name"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Compared with the earlier passing run") {
		t.Fatalf("the only earlier run hit another target, so it says nothing about fields that change every run here:\n%s", out)
	}
	if !strings.Contains(out, "against `"+second.URL+"`") {
		t.Fatalf("the summary must say no earlier run against this target exists:\n%s", out)
	}
}

func TestASecondProposalSaysWhichPendingProposalItReplaces(t *testing.T) {
	name := "widget"
	srv := newNamingBackend(&name)
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	propose := func() string {
		return captureStdout(t, func() {
			if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
				t.Fatalf("shrt run: %v", err)
			}
			if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "fetch returns the created name"}); err != nil {
				t.Fatalf("propose: %v", err)
			}
		})
	}
	first := propose()
	if strings.Contains(first, "replaces pending proposal") {
		t.Fatalf("nothing was pending yet:\n%s", first)
	}
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.store.LoadProposal("cli-thing-flow")
	if err != nil {
		t.Fatal(err)
	}
	second := propose()
	if !strings.Contains(second, "replaces pending proposal "+p.RunID) {
		t.Fatalf("a second proposal silently replaced the pending one of run %s:\n%s", p.RunID, second)
	}
}

func TestCLISupersedeProposalListsExpectationEditsAndATargetChange(t *testing.T) {
	first := approvedThingFlow(t)
	path := ".shrt/chains/cli-thing-flow.yaml"
	raw := string(mustRead(t, path))
	edited := strings.Replace(raw, "          - path: name\n            equals: widget\n",
		"          - path: name\n            equals: widget\n          - path: id\n            not_empty: true\n", 1)
	if edited == raw {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, path, edited)
	second := newEchoNameBackend()
	t.Cleanup(second.Close)
	cfg := string(mustRead(t, ".shrt/config.yaml"))
	writeFile(t, ".shrt/config.yaml", strings.Replace(cfg, first.URL, second.URL, 1))
	ctx := context.Background()
	var err error
	out := captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		err = runConfirm(ctx, []string{"cli-thing-flow", "-supersede", "-note", "id is now asserted"})
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := out[strings.Index(out, "Safe spot proposal"):]
	if !strings.Contains(summary, "chain expect") || !strings.Contains(summary, "absent -> id not_empty true") {
		t.Fatalf("the approver signs off on the added expectation, so the proposal lists it:\n%s", summary)
	}
	if !strings.Contains(summary, "target base_url "+first.URL+" -> "+second.URL) {
		t.Fatalf("the approver signs off on the new target, so the proposal lists it:\n%s", summary)
	}
}

func TestSupersedeDoesNotListFixtureEchoesAsDifferences(t *testing.T) {
	srv := newUniqueNameBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-unique.yaml", uniqueNameChain)
	ctx := context.Background()
	approveUniqueChain(t, ctx)
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-unique", "-quiet", "-var", "tag=second"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-unique", "-supersede", "-note", "same backend, fresh tag"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
	})
	e, err := loadEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.store.LoadProposal("cli-unique")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Replaced) != 0 {
		t.Fatalf("a request and response differing only in the fixture name is what verify masks, not a difference to sign off: %+v", p.Replaced)
	}
}

func TestSupersedeDoesNotCallAnIntendedChangeUnstableAgainstARunFromBeforeIt(t *testing.T) {
	spotRun := echoRecord(t, "r0",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "seq": "0"}})
	prev := echoRecord(t, "r1",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "note": "", "seq": "1"}},
		[3]any{"get_customer", map[string]any{"name": "other"}, map[string]any{"name": "other"}})
	rec := echoRecord(t, "r2",
		[3]any{"create", map[string]any{"name": "other"}, map[string]any{"name": "other", "note": "gift wrap", "seq": "2"}})
	spot := &store.SafeSpot{Chain: "echo", RunID: "r0", Steps: spotRun.Steps}
	unsent := func(_, path string, v any) bool { return path == "note" && v == "" }
	unstable, carried := unstableAgainstSpot(prev, rec, spot, nil, nil, unsent)
	if strings.Join(unstable, ",") != "create seq: 1 -> 2" {
		t.Fatalf("only a field that differs between two runs of the same backend state is unstable; a step absent in one run is not, got %v", unstable)
	}
	if strings.Join(carried, ",") != `create note: "" -> gift wrap` {
		t.Fatalf("a field where the earlier run still holds the replaced safe spot's value is the intended change, got %v", carried)
	}
}

func TestSupersedeNamesTheStepOfAStepLevelVolatile(t *testing.T) {
	rec := &runner.Record{Chain: "c", Volatile: []string{"**.trace"}, Steps: []*runner.StepRecord{
		{ID: "create"},
		{ID: "confirm_too_big", Volatile: []string{"status.message"}},
	}}
	c := &chain.Chain{Name: "c", Volatile: []string{"**.trace"}, Steps: []*chain.Step{
		{ID: "create"}, {ID: "confirm_too_big", Volatile: []string{"status.message"}},
	}}
	for _, current := range []*chain.Chain{c, nil} {
		got := []string{}
		for _, d := range unapprovedVolatileDiffers([]string{"status.message", "**.trace"}, rec, current) {
			got = append(got, d.Step+" "+d.Path)
		}
		if strings.Join(got, ",") != "confirm_too_big status.message,- **.trace" {
			t.Fatalf("a step-level volatile is labelled with its step, a chain-wide one with -, got %v", got)
		}
	}
}

func TestConfirmRenameFromRefusesAnythingButAPureRename(t *testing.T) {
	sameAs := func(from, to string) func(t *testing.T) {
		return func(t *testing.T) {
			writeFile(t, to, strings.Replace(string(mustRead(t, from)), "name: cli-renamed\n", "name: cli-thing-flow\n", 1))
		}
	}
	for _, c := range []struct {
		name string
		edit func(string) string
		prep func(t *testing.T)
		args []string
		want []string
		not  string
	}{
		{name: "a body change", edit: func(s string) string { return strings.Replace(s, "name: widget\n", "name: gadget\n", 1) },
			want: []string{"is not cli-thing-flow renamed", "body name"}},
		{name: "a renamed step", edit: func(s string) string { return strings.Replace(s, "    - id: fetch\n", "    - id: fetch_again\n", 1) },
			want: []string{"2 place(s)", "fetch step", "fetch_again step"}},
		{name: "allow_fail added", edit: func(s string) string {
			return strings.Replace(s, "    - id: fetch\n", "    - id: fetch\n      allow_fail: true\n", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "export added", edit: func(s string) string {
			return strings.Replace(s, "          thing_id: id\n", "          thing_id: id\n          thing_name: name\n", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "literal turned into a var", edit: func(s string) string {
			s = strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\nvars:\n    n: widget\n", 1)
			return strings.Replace(s, "          name: widget\n", "          name: ${vars.n}\n", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "uuid turned into a fixed var", edit: func(s string) string {
			s = strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\nvars:\n    k: fixed-key\n", 1)
			return strings.Replace(s, "idempotency_key: ${uuid}", "idempotency_key: ${vars.k}", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "expectation widened", edit: func(s string) string {
			return strings.Replace(s, "          - path: name\n            equals: widget\n", "          - path: name\n            not_empty: true\n", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "description changed", edit: func(s string) string {
			return strings.Replace(s, "name: cli-renamed\n", "name: cli-renamed\ndescription: now different\n", 1)
		}, want: []string{"is not cli-thing-flow renamed"}},
		{name: "no -by", args: []string{}, want: []string{"-by"}},
		{name: "the new chain has a safe spot", prep: func(t *testing.T) {
			writeFile(t, ".shrt/safespots/cli-renamed.json", string(mustRead(t, ".shrt/safespots/cli-thing-flow.json")))
		}, want: []string{"already has a safe spot"}},
		{name: "another file still claims the old name", prep: sameAs(".shrt/chains/cli-renamed.yaml", ".shrt/chains/junk.yaml"),
			want: []string{"junk.yaml"}, not: "cli-thing-flow.yaml"},
		{name: "the old chain still exists", prep: sameAs(".shrt/chains/cli-renamed.yaml", ".shrt/chains/cli-thing-flow.yaml"),
			want: []string{"still exists"}},
		{name: "a safe spot without a chain digest", prep: func(t *testing.T) {
			e, err := loadEnv(false)
			if err != nil {
				t.Fatal(err)
			}
			spot, err := e.store.LoadSafeSpot("cli-thing-flow")
			if err != nil || spot.ChainDigest == "" {
				t.Fatalf("an approved safe spot records its chain digest: %v", err)
			}
			spot.ChainDigest = ""
			spot.Digest = spot.ComputeDigest()
			raw, _ := json.MarshalIndent(spot, "", "  ")
			writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(raw))
			if spot.DigestKind() != store.DigestCurrent {
				t.Fatalf("still sealed, got %s", spot.DigestKind())
			}
		}, want: []string{"does not record the chain it was confirmed with"}, not: "were not compared"},
	} {
		t.Run(c.name, func(t *testing.T) {
			renameThingFlow(t, c.edit)
			if c.prep != nil {
				c.prep(t)
			}
			args := c.args
			if args == nil {
				args = []string{"-by", "bob@example.test"}
			}
			out, err := confirmRename(t, args...)
			if err == nil {
				t.Fatalf("not a pure rename, must be refused:\n%s", out)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("want %q in %v", w, err)
				}
			}
			if c.not != "" && strings.Contains(err.Error()+out, c.not) {
				t.Fatalf("unwanted %q in %v\n%s", c.not, err, out)
			}
			if _, err := os.Stat(".shrt/safespots/cli-thing-flow.json"); err != nil {
				t.Fatalf("a refused rename leaves the safe spot where it was: %v", err)
			}
		})
	}
}
