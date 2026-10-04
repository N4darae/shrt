package contract

import (
	"strings"

	"github.com/N4darae/shrt/chain"
)

func readRoots(c *chain.Chain) map[string]bool {
	out := map[string]bool{}
	for _, s := range c.Steps {
		if s == nil {
			continue
		}
		for _, ref := range s.References() {
			root, _, _ := strings.Cut(strings.TrimSpace(ref), ".")
			out[root] = true
		}
	}
	return out
}
