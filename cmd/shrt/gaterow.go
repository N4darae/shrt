package main

import (
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
)

func (r reason) lead() string {
	return fmt.Sprintf("suspect %s %s (%s)%s", rw(r.RPC), r.Step, shortRPC(r.RPC), asText(r.Profile))
}

func (r reason) inRow(head *gateItem) string {
	full := r.String()
	switch {
	case r.Kind == reasonUnclear && len(r.Or) == 0:
		read := " (" + methodName(r.ReadRPC) + asText(r.Profile) + ")"
		if head != nil && r.Read == head.Step {
			read = asText(r.Profile)
		}
		return fmt.Sprintf("unclear: %s %s (%s) or the read%s", rw(r.RPC), r.Step, shortRPC(r.RPC), read)
	case head == nil || r.Kind == reasonUnclear || r.Kind == reasonKnockOn:
		return full
	case r.Kind == reasonStored && chain.EdgeQuoted(r.Want) == head.Want && chain.EdgeQuoted(r.Got) == head.Got && methodName(r.ReadRPC) == methodName(head.Call):
		return r.lead() + ": stores other than it answered"
	case r.Step != head.Step:
		return full
	}
	if rest, ok := strings.CutPrefix(full, r.lead()); ok {
		return "suspect the " + rw(r.RPC) + asText(r.Profile) + rest
	}
	return full
}
