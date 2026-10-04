package contract

import (
	"fmt"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

const negativeValue = "-1"

func signedKind(kind string) bool {
	switch kind {
	case "int32", "int64", "sint32", "sint64", "sfixed32", "sfixed64", "float", "double":
		return true
	}
	return false
}

func (p *Plan) negativeProbe(lib *Library, st *chain.Step, m *catalog.Method, f *catalog.Field, key string, min int64, failure *Failure) string {
	if !signedKind(f.Kind) || min-1 < 0 {
		return ""
	}
	neg := p.probeCopy(lib, st, f.Name+"_negative")
	neg.Body[key] = negativeValue
	if failure != nil {
		neg.Expect = refusalFor(m, *failure, true)
		neg.Description = fmt.Sprintf("%s at %s, below its minimum and negative, is refused with %s, and nothing it would "+
			"have changed moves: a backend that applies the sign subtracts or credits instead.", f.Name, negativeValue, failure.Label())
	} else {
		neg.Expect = []chain.Expectation{{Path: chain.EnvelopePath(), NotEqual: chain.EnvelopeOK()}}
		neg.Description = fmt.Sprintf("%s at %s, below its minimum and negative, is refused, and nothing it would have "+
			"changed moves.", f.Name, negativeValue)
	}
	p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{neg}, neg.ID)...)
	return fmt.Sprintf("%s (%s, refused: a check for zero alone lets a negative value through)", neg.ID, negativeValue)
}
