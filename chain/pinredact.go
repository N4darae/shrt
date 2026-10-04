package chain

import (
	"fmt"

	"github.com/N4darae/shrt/pathmask"
)

func (c *Chain) RedactedPinProblems(redact []string) []string {
	if len(redact) == 0 {
		return nil
	}
	masker := pathmask.NewRedactor(redact)
	out := []string{}
	for i, k := range c.KeptRed {
		if k.Got == nil || !masker.Masks(k.Path) {
			continue
		}
		out = append(out, fmt.Sprintf("kept_red[%d] pins a got on %s %s, but a redact pattern covers that path, so the "+
			"value seen there is always recorded and compared as %s: any other got can never match, and got %q "+
			"would match every value. Drop got to pin only that the expectation fails there, or pin a path redact "+
			"does not cover", i, k.Step, k.Path, pathmask.MaskRedacted, pathmask.MaskRedacted))
	}
	return out
}
