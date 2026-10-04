package contract

import (
	"fmt"
	"slices"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

const negativeValue = "-1"

func (p *Plan) negativeProbe(lib *Library, st *chain.Step, m *catalog.Method, f *catalog.Field, key string, min int64, failure *Failure) string {
	if !slices.Contains([]string{"int32", "int64", "sint32", "sint64", "sfixed32", "sfixed64", "float", "double"}, f.Kind) || min-1 < 0 {
		return ""
	}
	neg := p.probeCopy(lib, st, f.Name+"_negative")
	neg.Body[key] = negativeValue
	expect, with := refusedWith(m, failure)
	neg.Expect = expect
	neg.Description = fmt.Sprintf("%s at %s, below its minimum and negative, is refused%s, and nothing it would have changed moves", f.Name, negativeValue, with)
	if failure != nil {
		neg.Description += ": a backend that applies the sign subtracts or credits instead"
	}
	neg.Description += "."
	p.Chain.Steps = append(p.Chain.Steps, p.guardUnchanged(lib, []*chain.Step{neg}, neg.ID)...)
	return fmt.Sprintf("%s (%s, refused: a check for zero alone lets a negative value through)", neg.ID, negativeValue)
}
