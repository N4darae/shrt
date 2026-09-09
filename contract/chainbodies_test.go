package contract_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
)

func libraryRequiring(fields ...string) *contract.Library {
	return contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/v1",
		Domain:     "test",
		RPCs: map[string]*contract.RPCContract{
			"shrt.test.v1.ThingService/Create": {Required: fields},
		},
	}})
}

func stepChain(steps ...*chain.Step) *chain.Chain {
	c := &chain.Chain{Name: "bodies", Steps: steps}
	_ = c.Normalize()
	return c
}

func bodyIssues(t *testing.T, c *chain.Chain, lib *contract.Library) []chain.Issue {
	t.Helper()
	return contract.LintChainBodies(c, lib, catalogtest.New())
}

func TestChainBodyLint_AnEmptyRequiredFieldOnASuccessStepIsAnError(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "", "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	issues := bodyIssues(t, c, libraryRequiring("name"))
	if len(issues) != 1 {
		t.Fatalf("want exactly one issue for an empty required field, got %d: %+v", len(issues), issues)
	}
	if issues[0].Severity != chain.SeverityError {
		t.Errorf("severity = %q, want error — a step that sends nothing for a required field cannot "+
			"pass, so this is not advice", issues[0].Severity)
	}
	if !strings.Contains(issues[0].Message, "name") {
		t.Errorf("message must name the field, got %q", issues[0].Message)
	}
}

func TestChainBodyLint_AMissingRequiredFieldIsTheSameError(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	if got := len(bodyIssues(t, c, libraryRequiring("name"))); got != 1 {
		t.Fatalf("want one issue for an absent required field, got %d — absent and empty are the same "+
			"fact to the server under proto3, so they must be the same fact here", got)
	}
}

func TestChainBodyLint_AStepThatEXPECTSARefusalIsLeftAlone(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "reject_empty_name", Call: "ThingService/Create", SkipAuth: true,
		Body: map[string]any{"name": "", "kind": "KIND_A"},
		Expect: []chain.Expectation{
			{Path: "error.code", Equals: "invalid_argument"},
		},
	})

	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
		t.Fatalf("a step whose whole point is the refusal must lint clean, got %+v — this rule exists "+
			"to catch an UNFILLED scaffold, and a negative case is the opposite of that", got)
	}
}

func TestChainBodyLint_NotEqualOKIsAlsoARefusalStep(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "reject_empty_name", Call: "ThingService/Create", SkipAuth: true,
		Body: map[string]any{"name": "", "kind": "KIND_A"},
		Expect: []chain.Expectation{
			{Path: "error.code", NotEqual: "OK"},
		},
	})

	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
		t.Fatalf("not_equal: OK is how a chain says \"refuse this\" when the app code is an authoring "+
			"label that never reaches the wire, and two chains in this corpus do exactly that. got %+v", got)
	}
}

func TestChainBodyLint_AReferenceCountsAsFilled(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "${vars.tag}", "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	if got := bodyIssues(t, c, libraryRequiring("name")); len(got) != 0 {
		t.Fatalf("a ${...} resolves at run time and is not empty, got %+v", got)
	}
}

func TestChainBodyLint_NoContractMeansNoOpinion(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	if got := contract.LintChainBodies(c, contract.NewLibrary(nil), catalogtest.New()); len(got) != 0 {
		t.Fatalf("without a curated contract this rule has nothing to say, got %+v — silence here is "+
			"correct, since the alternative is inventing a requirement the descriptor cannot state", got)
	}
}

func TestChainBodyLint_AnUnspecifiedEnumMemberIsNotAValue(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "KIND_UNSPECIFIED"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	if got := len(bodyIssues(t, c, libraryRequiring("name"))); got != 1 {
		t.Fatalf("want one issue for a required field left at the zero enum member, got %d", got)
	}
}

func TestChainBodyLint_ADeliberateZeroIsAValue(t *testing.T) {
	c := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "0"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})

	if got := len(bodyIssues(t, c, libraryRequiring("name"))); got != 0 {
		t.Fatalf("a chain author who writes 0 may mean it — an int64 zero and the scaffold's own "+
			"filler are the same bytes, and only the scaffold knows which it wrote; got %d issue(s)", got)
	}
}

func TestChainBodyLint_AMeaningfulZeroEnumIsNotAPlaceholder(t *testing.T) {
	emptyLib := contract.NewLibrary([]*contract.Overlay{{
		APIVersion: "shrt/v1", Domain: "test",
		RPCs: map[string]*contract.RPCContract{"shrt.test.v1.ThingService/Create": {}},
	}})

	placeholder := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "x", "kind": "KIND_UNSPECIFIED"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})
	if got := bodyIssues(t, placeholder, emptyLib); len(got) != 1 {
		t.Fatalf("KIND_UNSPECIFIED is the scaffold's filler and must be reported, got %d issue(s): %+v", len(got), got)
	}

	meaningful := stepChain(&chain.Step{
		ID: "create", Call: "ThingService/Create", SkipAuth: true,
		Body:   map[string]any{"name": "x", "kind": "KIND_A"},
		Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}},
	})
	if got := bodyIssues(t, meaningful, emptyLib); len(got) != 0 {
		t.Errorf("KIND_A is a real value that happens to sit at enum position 0 — proto3 requires a zero "+
			"member, not that it be a placeholder. Reporting it tells an author to 'fill' a field they "+
			"deliberately set. Got: %+v", got)
	}
}
