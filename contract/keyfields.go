package contract

import (
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

func KeyFieldFor(lib *Library) func(rpc, field string) (bool, bool) {
	return func(rpc, field string) (bool, bool) {
		c, ok := lib.Get(rpc)
		if !ok {
			return false, false
		}
		field = stripIndexes(field)
		if IsEntityIDField(field) || isIdempotencyField(&catalog.Field{Name: chain.PathLeaf(field), Kind: "string"}) {
			return true, true
		}
		if fc := c.Fields[field]; fc != nil && strings.Contains(strings.ToLower(fc.Note), "unique") {
			return true, true
		}
		for _, f := range lib.AllFailures(rpc) {
			if _, unique := uniquenessNoun(f); unique && f.Field != "" && stripIndexes(f.Field) == field {
				return true, true
			}
		}
		return false, true
	}
}
