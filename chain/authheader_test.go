package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func authIssues(t *testing.T, step *chain.Step) []chain.Issue {
	t.Helper()
	out := []chain.Issue{}
	for _, i := range lintOf(t, step) {
		if strings.Contains(i.Message, "uthorization") || strings.Contains(i.Message, "skip_auth") {
			out = append(out, i)
		}
	}
	return out
}

func TestLintRefusesSkipAuthWithAHandWrittenAuthorizationHeader(t *testing.T) {
	issues := authIssues(t, &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:     map[string]any{"name": "widget", "kind": "KIND_A"},
		SkipAuth: true,
		Headers:  map[string]string{"Authorization": "Bearer ${vars.token}"},
	})

	if len(issues) != 1 || !issues[0].IsError() {
		t.Fatalf("PLAYBOOK §6 bans this pairing outright, so lint must ERROR on it: %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "profile") {
		t.Errorf("the refusal has to name the replacement, or an author just deletes the header and "+
			"loses the principal: %q", issues[0].Message)
	}
}

func TestLintMatchesTheAuthorizationHeaderWhateverItsCase(t *testing.T) {
	issues := authIssues(t, &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:     map[string]any{"name": "widget", "kind": "KIND_A"},
		SkipAuth: true,
		Headers:  map[string]string{"authorization": "Bearer x"},
	})

	if len(issues) != 1 || !issues[0].IsError() {
		t.Fatalf("HTTP header names are case-insensitive, and a lowercase spelling is the same "+
			"workaround: %+v", issues)
	}
}

func TestLintWarnsThatAHandWrittenAuthorizationHeaderIsOverwritten(t *testing.T) {
	issues := authIssues(t, &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"Authorization": "Bearer x"},
	})

	if len(issues) != 1 {
		t.Fatalf("want one issue, got %+v", issues)
	}
	if issues[0].IsError() {
		t.Fatal("without the config lint cannot know whether a profile covers the call, and a backend " +
			"with no auth block at all has no other way to send a credential")
	}
	if !strings.Contains(issues[0].Message, "overwrites") {
		t.Errorf("the warning must say the value is discarded — that is the whole surprise: %q", issues[0].Message)
	}
}

func TestLintLeavesAnOrdinaryHeaderAlone(t *testing.T) {
	issues := authIssues(t, &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"X-Request-Id": "${uuid}"},
	})

	if len(issues) != 0 {
		t.Fatalf("only the auth header is special; everything else is the author's business: %+v", issues)
	}
}

func coveredBy(profile, header string) chain.LintOptions {
	return chain.LintOptions{AuthHeader: func(*chain.Step) (string, string, bool) { return profile, header, true }}
}

func authIssuesWith(t *testing.T, opts chain.LintOptions, step *chain.Step) []chain.Issue {
	t.Helper()
	step.Expect = []chain.Expectation{{Path: "error.code", Equals: "OK"}}
	c := &chain.Chain{Name: "t", Steps: []*chain.Step{step}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []chain.Issue{}
	for _, i := range chain.LintWith(c, catalogtest.New(), opts) {
		if strings.Contains(strings.ToLower(i.Message), "header") {
			out = append(out, i)
		}
	}
	return out
}

func TestLintRefusesAHandWrittenAuthHeaderWhenAProfileCoversTheCall(t *testing.T) {
	issues := authIssuesWith(t, coveredBy("default", "Authorization"), &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"Authorization": "Bearer ${vars.other_token}"},
	})

	if len(issues) != 1 || !issues[0].IsError() {
		t.Fatalf("a profile covers this call, so the middleware replaces the header and the step runs as "+
			"that profile's principal while reading as another's. That is a false green and must be an "+
			"error: %+v", issues)
	}
	if !strings.Contains(issues[0].Message, `"default"`) {
		t.Errorf("the error must name the profile the step actually runs as: %q", issues[0].Message)
	}
}

func TestLintMatchesTheHeaderTheCoveringProfileWrites(t *testing.T) {
	issues := authIssuesWith(t, coveredBy("partner", "X-Api-Key"), &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"x-api-key": "k"},
	})
	if len(issues) != 1 || !issues[0].IsError() {
		t.Fatalf("the profile writes X-Api-Key, so a hand-written one is what gets overwritten: %+v", issues)
	}

	issues = authIssuesWith(t, coveredBy("partner", "X-Api-Key"), &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"Authorization": "Bearer x"},
	})
	if len(issues) != 0 {
		t.Fatalf("a profile writing X-Api-Key leaves Authorization alone, so it is sent as written: %+v", issues)
	}
}

func TestLintLeavesAHandWrittenAuthHeaderAloneWhenNoProfileCoversTheCall(t *testing.T) {
	opts := chain.LintOptions{AuthHeader: func(*chain.Step) (string, string, bool) { return "", "", false }}
	issues := authIssuesWith(t, opts, &chain.Step{
		ID: "create", Call: "ThingService/Create",
		Body:    map[string]any{"name": "widget", "kind": "KIND_A"},
		Headers: map[string]string{"Authorization": "Bearer x"},
	})
	if len(issues) != 0 {
		t.Fatalf("no profile covers this call, so the header is what is sent and there is nothing to say: %+v", issues)
	}
}
