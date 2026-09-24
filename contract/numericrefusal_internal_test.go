package contract

import (
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestANumericEnvelopeRefusalMarksTheStepAsAProbe(t *testing.T) {
	envelope := chain.EnvelopePath()
	probe := &chain.Step{ID: "p", Expect: []chain.Expectation{{Path: envelope, Equals: 1603}}}
	if stepExpectsSuccess(probe) {
		t.Fatalf("equals: 1603 on %s names a refusal whatever YAML type it was written as", envelope)
	}
	happy := &chain.Step{ID: "h", Expect: []chain.Expectation{{Path: envelope, Equals: chain.EnvelopeOK()}}}
	if !stepExpectsSuccess(happy) {
		t.Fatal("equals the OK value is a step expecting success")
	}
}
