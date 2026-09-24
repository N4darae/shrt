package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
)

func inexactIssues(t *testing.T, export map[string]string, expect ...chain.Expectation) []chain.Issue {
	t.Helper()
	c := &chain.Chain{Name: "inexact", Steps: []*chain.Step{{
		ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "thing-1"},
		Export: export,
		Expect: append([]chain.Expectation{{Path: "error.code", Equals: "OK"}}, expect...),
	}}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	out := []chain.Issue{}
	for _, i := range chain.Lint(c, catalogtest.New()) {
		if i.Kind == chain.KindInexactPath {
			out = append(out, i)
		}
	}
	return out
}

func TestAPathMatchedOnlyByFoldingIsWarnedWithTheExactName(t *testing.T) {
	got := inexactIssues(t, map[string]string{"c": "createdat"}, chain.Expectation{Path: "Error.Code", Equals: "OK"})
	if len(got) != 2 {
		t.Fatalf("want one warning for the export and one for the expect, got %+v", got)
	}
	for _, i := range got {
		if i.Severity != chain.SeverityWarn {
			t.Fatalf("the run resolves a folded path, so this is a warning: %+v", i)
		}
		if !strings.Contains(i.Message, `"created_at"`) && !strings.Contains(i.Message, `"error.code"`) {
			t.Fatalf("the warning must name the exact field path: %+v", i)
		}
	}
	if chain.IsAssertionQualityIssue(got[0]) {
		t.Fatal("a folded path still reads the right field, so -strict must not fail on it")
	}
}

func TestAnExactPathIsNotWarned(t *testing.T) {
	if got := inexactIssues(t, map[string]string{"c": "created_at"}, chain.Expectation{Path: "name", Equals: "widget"}); len(got) != 0 {
		t.Fatalf("exact field names, got %+v", got)
	}
}
