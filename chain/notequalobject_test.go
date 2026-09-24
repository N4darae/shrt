package chain_test

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestNotEqualAScalarOnAMessageIsUnfailable(t *testing.T) {
	got := verdictIssues(chain.Expectation{Path: "error", NotEqual: "OK"})
	if len(got) != 1 {
		t.Fatalf("error is the envelope's parent message: an object never equals the text OK, so not_equal holds on "+
			"every answer and -strict must fail it; got %v", got)
	}
	strict := chain.Promote(got, chain.IsAssertionQualityIssue)
	if !strict[0].IsError() {
		t.Fatalf("-strict must fail an unfailable assertion, got %v", strict[0])
	}
	if got := verdictIssues(chain.Expectation{Path: "error.code", NotEqual: "OK"}); len(got) != 0 {
		t.Errorf("not_equal on the scalar verdict discriminates, got %v", got)
	}
	if got := verdictIssues(chain.Expectation{Path: "error", NotEqual: map[string]any{"code": "OK"}}); len(got) != 0 {
		t.Errorf("an object compared with an object can differ or not, got %v", got)
	}
}
