package contract_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/contract"
)

func errorsOf(issues []contract.Issue) []contract.Issue {
	out := []contract.Issue{}
	for _, i := range issues {
		if i.Severity == contract.SeverityError {
			out = append(out, i)
		}
	}
	return out
}

func TestPlanFlagsAnUnfilledEnumAsRemainingWork(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    required: [name, kind]
    status: draft
`)
	plan, err := contract.BuildPlan("ThingService/Create", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	joined := strings.Join(plan.Notes, "\n")
	if !strings.Contains(joined, "kind") {
		t.Fatalf("the zero enum value is not a usable value and must be reported, got:\n%s", joined)
	}
	if !strings.Contains(joined, "name") {
		t.Fatalf("an empty string must still be reported, got:\n%s", joined)
	}
}

func TestBlankDetectionCoversZeroValuedScalars(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.AuthService/Login:
    summary: s
    required: [username]
    fields:
      username:
        value: "0"
    status: draft
`)
	plan, err := contract.BuildPlan("AuthService/Login", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !strings.Contains(strings.Join(plan.Notes, "\n"), "username") {
		t.Fatalf(`a value of "0" is the int64 scaffold placeholder, not a real value: %v`, plan.Notes)
	}
}

func TestAliasOverridesGiveEachInstanceItsOwnBody(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    aliases:
      left:
        note: the left one
        fields:
          name:
            value: left-thing
      right:
        fields:
          name:
            value: right-thing
    status: draft
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: "shrt.test.v1.ThingService/Create@left->id"}
    needs: ["shrt.test.v1.ThingService/Create@right"]
    status: draft
`)
	plan, err := contract.BuildPlan("ThingService/Fetch", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	left, ok := plan.Chain.Step("create_left")
	if !ok {
		t.Fatalf("no create_left in %v", plan.Order)
	}
	right, _ := plan.Chain.Step("create_right")
	if left.Body["name"] != "left-thing" || right.Body["name"] != "right-thing" {
		t.Fatalf("alias overrides not applied: left=%v right=%v", left.Body["name"], right.Body["name"])
	}
}

func TestUndeclaredAliasIsWarnedNotSilent(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    status: draft
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: "shrt.test.v1.ThingService/Create@ghost->id"}
    status: draft
`)
	issues := contract.LintLibrary(lib, catalogtest.New())
	for _, i := range issues {
		if strings.Contains(i.Message, "ghost") && i.Severity == contract.SeverityWarn {
			return
		}
	}
	t.Fatalf("an alias nobody declared must be warned about, got %v", issues)
}

func TestBeforePullsAPrerequisiteIntoAnotherDomainsPlan(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.AuthService/Login:
    summary: the prerequisite, whose output nothing consumes
    before: [shrt.test.v1.ThingService/Create]
    status: draft
  shrt.test.v1.ThingService/Create:
    summary: s
    status: draft
`)
	plan, err := contract.BuildPlan("ThingService/Create", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Order) != 2 {
		t.Fatalf("before must pull the declaring rpc into the plan, got %v", plan.Order)
	}
	if !strings.HasSuffix(plan.Order[0], "/Login") {
		t.Fatalf("the declaring rpc must be ordered first, got %v", plan.Order)
	}
}

func TestBeforeCannotCreateAnUndetectedCycle(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.AuthService/Login:
    summary: s
    before: [shrt.test.v1.ThingService/Create]
    status: draft
  shrt.test.v1.ThingService/Create:
    summary: s
    needs: [shrt.test.v1.AuthService/Login]
    before: [shrt.test.v1.AuthService/Login]
    status: draft
`)
	issues := contract.LintLibrary(lib, catalogtest.New())
	for _, i := range issues {
		if strings.Contains(i.Message, "cycle") {
			return
		}
	}
	t.Fatalf("a cycle through before must be reported, got %v", issues)
}

func TestOneOfRejectsTwoArmsCarryingAValue(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    fields:
      name:
        oneof: owner
        value: a
      idempotency_key:
        oneof: owner
        value: b
    status: draft
`)
	issues := errorsOf(contract.LintLibrary(lib, catalogtest.New()))
	for _, i := range issues {
		if strings.Contains(i.Message, "oneof") {
			return
		}
	}
	t.Fatalf("two armed oneof members must be an error, got %v", issues)
}

func TestOneOfAcceptsExactlyOneArm(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    fields:
      name:
        oneof: owner
        value: a
      idempotency_key:
        oneof: owner
        note: leave empty when name is set
    status: draft
`)
	if issues := errorsOf(contract.LintLibrary(lib, catalogtest.New())); len(issues) != 0 {
		t.Fatalf("one armed member is valid, got %v", issues)
	}
}

func TestFailureWithoutAnAppCodeIsValid(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    failures:
      - connect_code: invalid_argument
        reason: NameEmpty
        field: name
        when: name is empty
    status: draft
`)
	if issues := errorsOf(contract.LintLibrary(lib, catalogtest.New())); len(issues) != 0 {
		t.Fatalf("a shape error carries no app code and must still be expressible, got %v", issues)
	}
}

func TestFailureNamingNothingIsAnError(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    failures:
      - when: something happens
    status: draft
`)
	if len(errorsOf(contract.LintLibrary(lib, catalogtest.New()))) == 0 {
		t.Fatal("a failure with no code, connect_code or reason must be rejected")
	}
}

func TestDomainFailuresAreInheritedByEveryRPC(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
failures:
  - code: 1603
    reason: PermissionNotGranted
    when: the caller lacks the role
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    failures:
      - code: 1001
        reason: Local
    status: draft
`)
	all := lib.AllFailures("shrt.test.v1.ThingService/Create")
	if len(all) != 2 {
		t.Fatalf("want the domain failure plus the local one, got %v", all)
	}
	if all[0].Code != 1603 {
		t.Fatalf("domain failures should come first, got %v", all)
	}
	if len(lib.InheritedFailures("shrt.test.v1.ThingService/Create")) != 1 {
		t.Fatal("inherited failures must be distinguishable from local ones")
	}
}

func TestCheckedByRejectsAnUnknownValue(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    fields:
      name:
        checked_by: vibes
    status: draft
`)
	if len(errorsOf(contract.LintLibrary(lib, catalogtest.New()))) == 0 {
		t.Fatal("checked_by must be one of the known values")
	}
}

func TestNestedFieldPathsAreAccepted(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    required: [meta.source]
    fields:
      meta.source:
        value: shrt-pilot
      meta.trace_id:
        note: optional
    status: draft
`)
	if issues := errorsOf(contract.LintLibrary(lib, catalogtest.New())); len(issues) != 0 {
		t.Fatalf("a dotted path into a nested message must be accepted, got %v", issues)
	}
}

func TestNestedFieldPathIsRejectedWhenItDoesNotExist(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    fields:
      meta.nope:
        value: x
    status: draft
`)
	if len(errorsOf(contract.LintLibrary(lib, catalogtest.New()))) == 0 {
		t.Fatal("a nested path that does not exist must still be caught")
	}
}

func TestPlanWritesNestedFieldValuesIntoTheBody(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    required: [meta.source]
    fields:
      meta.source:
        value: shrt-pilot
    status: draft
`)
	plan, err := contract.BuildPlan("ThingService/Create", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	step, _ := plan.Chain.Step("create")
	meta, ok := step.Body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta should be a nested object, got %#v", step.Body["meta"])
	}
	if meta["source"] != "shrt-pilot" {
		t.Fatalf("nested value not written, got %#v", meta)
	}
	for _, n := range plan.Notes {
		if strings.Contains(n, "meta.source") {
			t.Fatalf("a filled nested field must not be reported as remaining work: %s", n)
		}
	}
}

func TestExportNamesDoNotCollideAcrossAliasedSteps(t *testing.T) {
	lib := libraryFrom(t, `
domain: test
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: s
    exports:
      id: the id
    aliases:
      left: {}
      right: {}
    status: draft
  shrt.test.v1.ThingService/Fetch:
    summary: s
    fields:
      id: {from: "shrt.test.v1.ThingService/Create@left->id"}
    needs: ["shrt.test.v1.ThingService/Create@right"]
    status: draft
`)
	plan, err := contract.BuildPlan("ThingService/Fetch", lib, catalogtest.New(), "c")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	seen := map[string]string{}
	for _, step := range plan.Chain.Steps {
		for name := range step.Export {
			if prior, dup := seen[name]; dup {
				t.Fatalf("export %q is emitted by both %s and %s, so one silently clobbers the other",
					name, prior, step.ID)
			}
			seen[name] = step.ID
		}
	}
	if len(seen) != 2 {
		t.Fatalf("want one export per aliased step, got %v", seen)
	}
}

func TestScanTodosAttributesEachMarkerToItsRPCAndField(t *testing.T) {
	raw := []byte(`
apiVersion: shrt/contract/v1
domain: test
description: 'TODO: describe me'
rpcs:
  shrt.test.v1.ThingService/Create:
    summary: 'TODO: what does it do'
    fields:
      name:
        note: 'TODO: where from'
    status: draft
`)
	issues := contract.ScanTodos("test", raw)
	if len(issues) != 3 {
		t.Fatalf("want one issue per marker, got %d: %v", len(issues), issues)
	}
	byField := map[string]string{}
	for _, i := range issues {
		byField[i.Field] = i.RPC
	}
	if byField["summary"] != "shrt.test.v1.ThingService/Create" {
		t.Fatalf("summary marker not attributed to its rpc: %v", byField)
	}
	if byField["fields.name.note"] != "shrt.test.v1.ThingService/Create" {
		t.Fatalf("field marker not attributed to its path: %v", byField)
	}
	if byField["description"] != "" {
		t.Fatalf("a document-level marker should carry no rpc: %v", byField)
	}
}

func TestScaffoldPreWiresAFieldWithExactlyOneProducer(t *testing.T) {
	cat := catalogtest.New()
	producers := contract.ProducersOf("id", cat.Methods(), "")
	if len(producers) != 1 {
		t.Fatalf("want exactly one producer of id, got %v", producers)
	}
	if got := producers[0].Ref(); got != "shrt.test.v1.ThingService/Create->id" {
		t.Fatalf("producer ref should name the rpc and use the arrow separator, got %q", got)
	}
}

func TestScaffoldOffersEveryCandidateWhenAFieldHasSeveralProducers(t *testing.T) {
	cat := catalogtest.New()
	producers := contract.ProducersOf("access_token", cat.Methods(), "")
	if len(producers) != 2 {
		t.Fatalf("two login rpcs both return access_token, got %v", producers)
	}
	refs := []string{producers[0].Ref(), producers[1].Ref()}
	sort.Strings(refs)
	want := []string{
		"shrt.test.v1.AuthService/Login->access_token",
		"shrt.test.v1.PartnerAuthService/Login->access_token",
	}
	if strings.Join(refs, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates = %v, want %v", refs, want)
	}
	if excluded := contract.ProducersOf("access_token", cat.Methods(), "shrt.test.v1.AuthService/Login"); len(excluded) != 1 {
		t.Fatalf("the rpc being scaffolded must not be offered as its own producer, got %v", excluded)
	}
}

func TestScaffoldIgnoresReadOnlyRPCsAsProducers(t *testing.T) {
	cat := catalogtest.New()
	for _, p := range contract.ProducersOf("id", cat.Methods(), "") {
		if strings.Contains(p.RPC, "/Fetch") {
			t.Fatalf("a Fetch rpc must not be offered as a producer: %v", p)
		}
	}
}

func TestLegacySeparatorStillParsesButIsWarned(t *testing.T) {
	ref, err := contract.ParseRef("pkg.Svc/Rpc" + "#" + "id")
	if err != nil {
		t.Fatalf("the legacy separator must keep working: %v", err)
	}
	if ref.Path != "id" {
		t.Fatalf("got %+v", ref)
	}
	if !contract.UsesLegacySeparator("pkg.Svc/Rpc" + "#" + "id") {
		t.Fatal("legacy form should be detectable so lint can nudge")
	}
	if contract.UsesLegacySeparator("pkg.Svc/Rpc->id") {
		t.Fatal("the arrow form is not legacy")
	}
}
