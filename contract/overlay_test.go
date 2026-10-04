package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func libraryFrom(t *testing.T, body string) *contract.Library {
	t.Helper()
	o := &contract.Overlay{}
	if err := yaml.Unmarshal([]byte(body), o); err != nil {
		t.Fatalf("parse overlay: %v", err)
	}
	o.APIVersion = contract.OverlayAPIVersion
	return contract.NewLibrary([]*contract.Overlay{o})
}

func thingPlan(t *testing.T, target string, lib *contract.Library, name string) *contract.Plan {
	t.Helper()
	plan, err := contract.BuildPlan(target, lib, catalogtest.New(), name)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

const thingOverlay = `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: create a thing
    required: [name]
    fields:
      idempotency_key:
        value: ${uuid}
    exports:
      id: the thing to fetch later
    status: draft
  shrt.test.v1.ThingService/Fetch:
    summary: read a thing back
    required: [id]
    fields:
      id:
        from: shrt.test.v1.ThingService/Create->id
    status: draft
`

func TestParseRefAcceptsAliasAndRejectsGarbage(t *testing.T) {
	ref, err := contract.ParseRef("pkg.Svc/Rpc@give->id_asset")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ref.RPC != "pkg.Svc/Rpc" || ref.Alias != "give" || ref.Path != "id_asset" {
		t.Fatalf("got %+v", ref)
	}
	if ref.Node() != "pkg.Svc/Rpc@give" {
		t.Fatalf("node = %q", ref.Node())
	}

	plain, err := contract.ParseRef("pkg.Svc/Rpc->id")
	if err != nil || plain.Alias != "" || plain.Node() != "pkg.Svc/Rpc" {
		t.Fatalf("plain ref wrong: %+v %v", plain, err)
	}
	for _, bad := range []string{"pkg.Svc/Rpc", "->id", "", "  ->  "} {
		if _, err := contract.ParseRef(bad); err == nil {
			t.Fatalf("%q should not parse", bad)
		}
	}
}

func TestLintAcceptsAValidOverlay(t *testing.T) {
	lib := libraryFrom(t, thingOverlay)
	if issues := contract.LintLibrary(lib, catalogtest.New()); len(issues) != 0 {
		t.Fatalf("want no issues, got %v", issues)
	}
}

func TestLintCatchesFabricatedNames(t *testing.T) {
	cases := map[string]string{
		"unknown rpc": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Vanish:
    summary: s
    status: draft
`,
		"unknown request field in required": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    required: [not_a_field]
    status: draft
`,
		"unknown request field in fields": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    fields:
      not_a_field: {note: x}
    status: draft
`,
		"unknown export path": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    exports: {not_a_field: x}
    status: draft
`,
		"from without a path": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: shrt.test.v1.ThingService/Create}
    status: draft
`,
		"from reading a field the source does not return": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: "shrt.test.v1.ThingService/Create->nope"}
    status: draft
`,
		"bad status": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    status: probably-fine
`,
		"verified without a run": `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    status: verified
`,
	}
	cat := catalogtest.New()
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			issues := contract.LintLibrary(libraryFrom(t, body), cat)
			for _, i := range issues {
				if i.Severity == contract.SeverityError {
					return
				}
			}
			t.Fatalf("want an error, got %v", issues)
		})
	}
}

func TestLintCatchesDependencyCycles(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    needs: [shrt.test.v1.ThingService/Fetch]
    status: draft
  shrt.test.v1.ThingService/Fetch:
    summary: s
    needs: [shrt.test.v1.ThingService/Create]
    status: draft
`)
	issues := contract.LintLibrary(lib, catalogtest.New())
	for _, i := range issues {
		if strings.Contains(i.Message, "cycle") {
			return
		}
	}
	t.Fatalf("a cycle must be reported, got %v", issues)
}

func TestPlanOrdersDependenciesBeforeTheTarget(t *testing.T) {
	plan := thingPlan(t, "ThingService/Fetch", libraryFrom(t, thingOverlay), "thing-fetch")
	if len(plan.Order) != 2 {
		t.Fatalf("want 2 steps, got %v", plan.Order)
	}
	if !strings.HasSuffix(plan.Order[0], "/Create") || !strings.HasSuffix(plan.Order[1], "/Fetch") {
		t.Fatalf("Create must precede Fetch, got %v", plan.Order)
	}
}

func TestPlanWiresReferencesToTheProducingStep(t *testing.T) {
	plan := thingPlan(t, "ThingService/Fetch", libraryFrom(t, thingOverlay), "thing-fetch")
	fetch, ok := plan.Chain.Step("fetch")
	if !ok {
		t.Fatalf("no fetch step in %v", plan.Chain.Steps)
	}
	if got := fetch.Body["id"]; got != "${create.id}" {
		t.Fatalf("id wired to %v, want ${create.id}", got)
	}
	create, _ := plan.Chain.Step("create")
	if got := create.Body["idempotency_key"]; got != "${uuid}" {
		t.Fatalf("literal value not applied, got %v", got)
	}
	if create.Export["create_id"] != "id" {
		t.Fatalf("export not derived from the contract, got %v", create.Export)
	}
}

func TestPlanGivesAnAliasedDependencyItsOwnStep(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: "shrt.test.v1.ThingService/Create@left->id"}
    needs: ["shrt.test.v1.ThingService/Create@right"]
    status: draft
`)
	plan := thingPlan(t, "ThingService/Fetch", lib, "two-creates")
	if len(plan.Chain.Steps) != 3 {
		t.Fatalf("two aliases of one rpc must become two steps, got %d", len(plan.Chain.Steps))
	}
	if _, ok := plan.Chain.Step("create_left"); !ok {
		t.Fatalf("missing create_left in %v", plan.Order)
	}
	if _, ok := plan.Chain.Step("create_right"); !ok {
		t.Fatalf("missing create_right in %v", plan.Order)
	}
	fetch, _ := plan.Chain.Step("fetch")
	if got := fetch.Body["id"]; got != "${create_left.id}" {
		t.Fatalf("aliased reference wired to %v", got)
	}
}

func TestPlannedChainLintsClean(t *testing.T) {
	cat := catalogtest.New()
	plan := thingPlan(t, "ThingService/Fetch", libraryFrom(t, thingOverlay), "thing-fetch")
	raw, err := plan.YAML()
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	loaded := &chain.Chain{}
	if err := yaml.Unmarshal(raw, loaded); err != nil {
		t.Fatalf("a planned chain must be loadable: %v\n%s", err, raw)
	}
	if err := loaded.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	for _, issue := range chain.Lint(loaded, cat) {
		t.Errorf("a planned chain must lint clean: [%s] %s", issue.Severity, issue.Message)
	}
}

func TestPlanYAMLKeepsProtoFieldOrder(t *testing.T) {
	plan := thingPlan(t, "ThingService/Create", libraryFrom(t, thingOverlay), "thing-create")
	raw, err := plan.YAML()
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	text := string(raw)
	at := -1
	for _, key := range []string{"name:", "kind:", "idempotency_key:"} {
		i := strings.Index(text, key)
		if i < 0 || i < at {
			t.Fatalf("fields are not in proto order:\n%s", text)
		}
		at = i
	}
}

func TestScaffoldOverlayProducesALoadableSkeleton(t *testing.T) {
	cat := catalogtest.New()
	raw := renderScaffold(t, "test", cat.Methods(), nil, cat)
	o := &contract.Overlay{}
	if err := yaml.Unmarshal(raw, o); err != nil {
		t.Fatalf("scaffold is not valid overlay YAML: %v\n%s", err, raw)
	}
	if len(o.RPCs) != len(cat.Methods()) {
		t.Fatalf("want every rpc scaffolded, got %d of %d", len(o.RPCs), len(cat.Methods()))
	}
	if !strings.Contains(string(raw), contract.TodoMarker) {
		t.Fatal("a scaffold should mark what still needs filling")
	}
	for rpc, c := range o.RPCs {
		if c.Status != contract.StatusDraft {
			t.Fatalf("%s scaffolded as %q, want draft", rpc, c.Status)
		}
	}
}

func TestScaffoldCarriesExistingCurationForward(t *testing.T) {
	cat := catalogtest.New()
	lib := libraryFrom(t, thingOverlay)
	raw := renderScaffold(t, "test", cat.Methods(), lib, cat)
	o := &contract.Overlay{}
	if err := yaml.Unmarshal(raw, o); err != nil {
		t.Fatal(err)
	}
	created := o.RPCs["shrt.test.v1.ThingService/Create"]
	if created == nil || created.Summary != "create a thing" {
		t.Fatalf("rescaffolding must not discard curation, got %+v", created)
	}
	if login := o.RPCs["shrt.test.v1.AuthService/Login"]; login == nil {
		t.Fatal("rescaffolding must still add rpcs that had no contract")
	}
}

func TestLintSaysAnEntryForARemovedRpcIsNotInTheDescriptor(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/GetThing:
    summary: s
    status: draft
`)
	issues := contract.LintLibrary(lib, catalogtest.New())
	if len(issues) != 1 {
		t.Fatalf("want one issue, got %v", issues)
	}
	msg := issues[0].Message
	if !strings.Contains(msg, "not in the descriptor (removed from the proto?)") || !strings.Contains(msg, "delete this entry") {
		t.Fatalf("an entry for an rpc the proto lost should say so and what to do: %s", msg)
	}
	if strings.Contains(msg, "did you mean") {
		t.Fatalf("GetThing is not close to Create or Fetch; no suggestion: %s", msg)
	}
}
