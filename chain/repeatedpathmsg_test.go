package chain_test

import (
	"regexp"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestTheUnindexedPathMessageCarriesNoDevelopmentHistory(t *testing.T) {
	dated := regexp.MustCompile(`20[0-9]{2}-[0-9]{2}-[0-9]{2}`)
	for _, i := range lintOne(t, "results.error.code") {
		if i.Kind == chain.KindUnreachable && dated.MatchString(i.Message) {
			t.Fatalf("a lint message tells the reader what to fix, not when shrt changed: %s", i.Message)
		}
	}
}
