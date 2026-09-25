package chain_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func TestWithinComparesAClockValueAgainstNowunixArithmetic(t *testing.T) {
	now := time.Unix(1789123474, 0)
	scope := chain.NewScope(nil)
	scope.Now = func() time.Time { return now }
	e := chain.Expectation{Path: "expires_at", Within: &chain.Within{Of: "${nowunix+3600}", By: 5}}
	resolved, err := e.ResolveWith(scope)
	if err != nil {
		t.Fatal(err)
	}
	hour := strconv.FormatInt(now.Unix()+3602, 10)
	if r := resolved.EvaluateTyped(map[string]any{"expires_at": hour}, nil, "int64"); !r.Passed {
		t.Fatalf("an expiry two seconds past the hour is within 5s of ${nowunix+3600}: %+v", r)
	}
	millis := strconv.FormatInt((now.Unix()+3600)*1000, 10)
	if r := resolved.EvaluateTyped(map[string]any{"expires_at": millis}, nil, "int64"); r.Passed {
		t.Fatalf("an expiry in milliseconds is not within 5s of ${nowunix+3600}: %+v", r)
	}
	gte := chain.Expectation{Path: "created_at", Gte: "${nowunix-60}"}
	resolved, err = gte.ResolveWith(scope)
	if err != nil {
		t.Fatal(err)
	}
	if r := resolved.EvaluateTyped(map[string]any{"created_at": now.UTC().Format(time.RFC3339)}, nil, ""); !r.Passed || r.Rule != "gte" {
		t.Fatalf("an RFC3339 timestamp compares as unix seconds: %+v", r)
	}
}

func TestComparisonRulesLoadAndLintAsOneRuleEach(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	raw := `apiVersion: shrt/v1
name: c
steps:
    - id: login
      call: AuthService/Login
      body: {username: u, password: p}
      expect:
        - path: expires_at
          within: {of: "${nowunix+3600}", by: 5}
        - path: expires_at
          between: ["${nowunix}", "${nowunix+7200}"]
        - path: expires_at
          gt: ${later.expires_at}
        - path: expires_at
          gte: 1
          lte: 2
    - id: later
      call: AuthService/Login
      body: {username: u, password: p}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := chain.LoadFile(path)
	if err != nil {
		t.Fatalf("within, between and gt are chain keys: %v", err)
	}
	var msgs []string
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.IsError() {
			msgs = append(msgs, i.Message)
		}
	}
	all := strings.Join(msgs, "\n")
	if strings.Contains(all, "carries no rule") {
		t.Fatalf("a comparison rule is a rule:\n%s", all)
	}
	if !strings.Contains(all, "carries 2 rules (gte, lte)") {
		t.Fatalf("gte and lte on one entry are two rules, of which only one fires:\n%s", all)
	}
	if !strings.Contains(all, "${later.expires_at}") {
		t.Fatalf("a bound referencing a later step cannot resolve and is linted like an equals value:\n%s", all)
	}
}
