package contract

import (
	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func ApplyConventions(readOnlyPrefixes []string, envelopePath, envelopeOK string) {
	chain.ApplyConventions(readOnlyPrefixes, envelopePath, envelopeOK)
}

func CarriesEnvelope(m *catalog.Method) bool {
	for _, f := range catalog.DescribeMessage(m.Output()).Fields {
		if f.Name == chain.EnvelopeField() {
			return true
		}
	}
	return false
}

func SuccessExpectation(m *catalog.Method) []chain.Expectation {
	if CarriesEnvelope(m) {
		return []chain.Expectation{{Path: chain.EnvelopePath(), Equals: chain.EnvelopeOK()}}
	}
	out := catalog.DescribeMessage(m.Output()).Fields
	scalarID := func(f *catalog.Field) bool { return isScalar(f) && !f.Repeated && IsEntityIDField(f.Name) }
	scalar := func(f *catalog.Field) bool { return isScalar(f) && !f.Repeated }
	list := func(f *catalog.Field) bool { return f.Repeated }
	for _, want := range []func(*catalog.Field) bool{scalarID, scalar, list} {
		if f := pickAssertable(out, want); f != nil {
			return []chain.Expectation{{Path: f.Name, NotEmpty: true}}
		}
	}
	return nil
}

func isScalar(f *catalog.Field) bool { return f.Kind != "message" && f.Kind != "group" }

func pickAssertable(fields []*catalog.Field, want func(*catalog.Field) bool) *catalog.Field {
	for _, f := range fields {
		if IsVerdictFieldName(f.Name) || IsPagingFieldName(f.Name) {
			continue
		}
		if want(f) {
			return f
		}
	}
	return nil
}
