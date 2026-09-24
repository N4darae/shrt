package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestLintAcceptsAnExpectationReadingItsOwnStepsRequest(t *testing.T) {
	for _, ref := range []string{"${steps.create.request.name}", "${create.request.name}"} {
		msgs := lintMessages(t, chainExpecting(t, chain.Expectation{Path: "id", Equals: ref}))
		if len(msgs) != 0 {
			t.Errorf("%s: asserting the response echoes what this step sent resolves at run time "+
				"(the request is recorded before expectations run), so lint must accept it. got %v", ref, msgs)
		}
	}
}

func TestLintRejectsAnExpectationReadingItsOwnStepsResponse(t *testing.T) {
	for _, ref := range []string{"${steps.create.response.id}", "${create.id}", "${create}"} {
		msgs := lintMessages(t, chainExpecting(t, chain.Expectation{Path: "id", Equals: ref}))
		if len(msgs) != 1 || !strings.Contains(msgs[0], "own response") {
			t.Errorf("%s: want one error naming the step's own response, got %v", ref, msgs)
			continue
		}
		if strings.Contains(msgs[0], "does not run before") {
			t.Errorf("%s: the step is this one, so 'does not run before this step' is the wrong reason: %q", ref, msgs[0])
		}
	}
}

func TestLintStillRejectsABodyReadingItsOwnRequest(t *testing.T) {
	c := chainExpecting(t, chain.Expectation{Path: "id", NotEmpty: true})
	c.Steps[0].Body["kind"] = "${steps.create.request.name}"
	msgs := lintMessages(t, c)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "does not run before") {
		t.Fatalf("a body cannot be built from itself, want one error, got %v", msgs)
	}
}
