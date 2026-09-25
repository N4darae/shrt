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

func readExports(exports map[string]string, read map[string]bool) map[string]string {
	out := map[string]string{}
	for name, path := range exports {
		if read[name] {
			out[name] = path
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
