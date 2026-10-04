package contract

import (
	"slices"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

const sharesARequestValue = `apiVersion: shrt/contract/v1
domain: demo
rpcs:
    shrt.test.v1.AuthService/Login:
        summary: authenticates
        required: [username]
        status: draft
    shrt.test.v1.ThingService/Create:
        summary: creates a thing under that principal
        required: [name]
        fields:
            name:
                same_as: shrt.test.v1.AuthService/Login->username
        status: draft
`

func TestSameAsUnifiesBothSitesOntoOneChainVar(t *testing.T) {
	cat := catalogtest.New()
	lib := libraryFrom(t, sharesARequestValue)

	p, err := BuildPlan("shrt.test.v1.ThingService/Create", lib, cat, "demo")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(p.Order) != 2 {
		t.Fatalf("order is %v, want the producer pulled in", p.Order)
	}

	const want = "${vars.login_username}"
	login, create := p.stepByID("login"), p.stepByID("create")
	if login == nil || create == nil {
		t.Fatalf("steps are %v", p.Order)
	}
	if got := login.Body["username"]; got != want {
		t.Errorf("producer sends %v, want %s", got, want)
	}
	if got := create.Body["name"]; got != want {
		t.Errorf("consumer sends %v, want %s", got, want)
	}
	if _, declared := p.Chain.Vars["login_username"]; !declared {
		t.Errorf("the var was never declared on the chain: %v", p.Chain.Vars)
	}
}

func TestSameAsEmitsOneNoteNamingTheVarToFill(t *testing.T) {
	cat := catalogtest.New()
	p, err := BuildPlan("shrt.test.v1.ThingService/Create", libraryFrom(t, sharesARequestValue), cat, "demo")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var hits int
	for _, n := range p.Notes {
		if strings.Contains(n, "vars.login_username") {
			hits++
		}
		if strings.Contains(n, "step login: username is required") {
			t.Errorf("note tells the author to fill the producer field, but same_as replaced it with a var: %q", n)
		}
	}
	if hits != 1 {
		t.Fatalf("got %d notes naming the var, want exactly 1: %v", hits, p.Notes)
	}
}

const sameAsCycle = `apiVersion: shrt/contract/v1
domain: demo
rpcs:
    shrt.test.v1.AuthService/Login:
        summary: authenticates
        required: [username]
        fields:
            username:
                same_as: shrt.test.v1.ThingService/Create->name
        status: draft
    shrt.test.v1.ThingService/Create:
        summary: creates a thing
        required: [name]
        fields:
            name:
                same_as: shrt.test.v1.AuthService/Login->username
        status: draft
`

func TestLintRejectsABrokenSameAs(t *testing.T) {
	for _, tc := range []struct{ name, raw, needle, why string }{
		{"a cycle formed through same_as", sameAsCycle, "dependency cycle",
			"a cycle formed entirely through same_as edges must be reported — BuildPlan already refuses it, lint must not be blind to it"},
		{"same_as naming a response-only field", strings.Replace(sharesARequestValue, "Login->username", "Login->access_token", 1),
			"not a REQUEST field", "same_as naming a response-only field must be an error — it reads what a step SENDS"},
		{"from and same_as together", strings.Replace(sharesARequestValue,
			"                same_as: shrt.test.v1.AuthService/Login->username",
			"                same_as: shrt.test.v1.AuthService/Login->username\n                from: shrt.test.v1.AuthService/Login->access_token", 1),
			"mutually exclusive", "from and same_as on one field must be an error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !slices.ContainsFunc(LintLibrary(libraryFrom(t, tc.raw), catalogtest.New()), func(i Issue) bool {
				return i.Severity == SeverityError && strings.Contains(i.Message, tc.needle)
			}) {
				t.Fatal(tc.why)
			}
		})
	}
}

func TestLintSeesNoCycleWhenAnAliasSharesAValueWithItsOwnPlainRPC(t *testing.T) {
	const raw = `apiVersion: shrt/contract/v1
domain: demo
rpcs:
    shrt.test.v1.ThingService/Create:
        summary: creates a thing
        required: [name]
        fields:
            name:
                value: widget
        aliases:
            replay:
                note: the same create again, to pin the duplicate refusal
                fields:
                    name:
                        same_as: shrt.test.v1.ThingService/Create->name
        status: draft
`
	cat := catalogtest.New()
	for _, i := range LintLibrary(libraryFrom(t, raw), cat) {
		if strings.Contains(i.Message, "dependency cycle") {
			t.Fatalf("Create@replay depends on plain Create, which depends on nothing; plan builds it, lint must agree: %s", i.Message)
		}
	}
	if _, err := BuildPlan("shrt.test.v1.ThingService/Create@replay", libraryFrom(t, raw), cat, "replay"); err != nil {
		t.Fatalf("plan: %v", err)
	}
}

func TestSameAsKeepsTheProducersTemplatedValue(t *testing.T) {
	cat := catalogtest.New()
	raw := strings.Replace(sharesARequestValue, "        required: [username]\n",
		"        required: [username]\n        fields:\n            username:\n                value: user-${vars.tag}\n", 1)
	p, err := BuildPlan("shrt.test.v1.ThingService/Create", libraryFrom(t, raw), cat, "demo")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	login, create := p.stepByID("login"), p.stepByID("create")
	if login == nil || create == nil {
		t.Fatalf("steps are %v", p.Order)
	}
	if got := login.Body["username"]; got != "user-${vars.tag}" {
		t.Errorf("producer sends %v, want the contract's value: user-${vars.tag}", got)
	}
	if got := create.Body["name"]; got != "${steps.login.request.username}" {
		t.Errorf("consumer sends %v, want a reference to what the producer sent", got)
	}
	if v, declared := p.Chain.Vars["login_username"]; declared {
		t.Errorf("no var is needed when the consumer reads the producer's request, got vars %v (login_username=%q)", p.Chain.Vars, v)
	}
}
