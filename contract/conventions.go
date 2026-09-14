package contract

import (
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/namecase"
)

var notEntityIDQualifier = map[string]bool{
	"request":     true,
	"correlation": true,
	"trace":       true,
	"span":        true,
	"session":     true,
	"idempotency": true,
}

var notEntityIDSubject = map[string]bool{
	"token":    true,
	"number":   true,
	"type":     true,
	"card":     true,
	"document": true,
	"kind":     true,
	"format":   true,
	"scheme":   true,
	"prefix":   true,
	"suffix":   true,
}

func isIDWord(w string) bool {
	return w == "id" || w == "ids" || w == "uuid" || w == "uuids"
}

func IsEntityIDField(name string) bool {
	base := name
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	w := namecase.Words(base)
	switch len(w) {
	case 0:
		return false
	case 1:
		return isIDWord(w[0])
	}
	if isIDWord(w[len(w)-1]) {
		return !notEntityIDQualifier[w[len(w)-2]]
	}
	if isIDWord(w[0]) {
		return !notEntityIDSubject[w[1]]
	}
	return false
}

func IsVerdictFieldName(name string) bool {
	return name == chain.EnvelopeField() || name == chain.DefaultEnvelopeField
}

func IsPlaceholderEnumValue(value string) bool {
	w := namecase.Words(value)
	if len(w) == 0 {
		return false
	}
	switch w[len(w)-1] {
	case "unspecified", "unknown", "none", "invalid", "unset", "undefined":
		return true
	}
	return false
}

func IsPagingFieldName(name string) bool { return chain.IsPagingFieldName(name) }
