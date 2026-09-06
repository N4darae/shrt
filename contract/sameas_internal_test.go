package contract

import (
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

func TestLintCatchesACycleFormedThroughSameAs(t *testing.T) {
	const raw = `apiVersion: shrt/contract/v1
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
	cat := catalogtest.New()
	var found bool
	for _, i := range LintLibrary(libraryFrom(t, raw), cat) {
		if i.Severity == SeverityError && strings.Contains(i.Message, "dependency cycle") {
			found = true
		}
	}
	if !found {
		t.Fatal("a cycle formed entirely through same_as edges must be reported — BuildPlan already refuses it, lint must not be blind to it")
	}
}

func TestLintRejectsSameAsNamingAResponseOnlyField(t *testing.T) {
	cat := catalogtest.New()
	raw := strings.Replace(sharesARequestValue, "Login->username", "Login->access_token", 1)
	var found bool
	for _, i := range LintLibrary(libraryFrom(t, raw), cat) {
		if i.Severity == SeverityError && strings.Contains(i.Message, "not a REQUEST field") {
			found = true
		}
	}
	if !found {
		t.Fatal("same_as naming a response-only field must be an error — it reads what a step SENDS")
	}
}

func TestLintRejectsFromAndSameAsTogether(t *testing.T) {
	cat := catalogtest.New()
	raw := strings.Replace(sharesARequestValue,
		"                same_as: shrt.test.v1.AuthService/Login->username",
		"                same_as: shrt.test.v1.AuthService/Login->username\n                from: shrt.test.v1.AuthService/Login->access_token", 1)
	var found bool
	for _, i := range LintLibrary(libraryFrom(t, raw), cat) {
		if i.Severity == SeverityError && strings.Contains(i.Message, "mutually exclusive") {
			found = true
		}
	}
	if !found {
		t.Fatal("from and same_as on one field must be an error")
	}
}
