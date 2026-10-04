package main

import (
	"cmp"
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

func (r reason) inGroup(rpc, step string) string {
	named := func(s, call, profile string) string {
		out := ""
		if s != step {
			out = rw(call) + " " + s
		}
		if shortRPC(call) != rpc {
			out += " (" + shortRPC(call) + ")"
		}
		return strings.TrimSpace(out + asText(profile))
	}
	switch {
	case r.Kind == "", r.Kind == reasonUnclear && strings.Contains(rpc, " or "):
		return ""
	case r.Kind == reasonUnclear && len(r.Or) > 1:
		names := []string{}
		for _, o := range r.Or[:2] {
			names = append(names, strings.TrimPrefix(named(o.Step, o.RPC, o.Profile), "write "))
		}
		if n := len(r.Or) - 2; n > 0 {
			names[1] += fmt.Sprintf(" +%d more", n)
		}
		return "unclear: write " + strings.Join(names, " or ")
	case r.Kind == reasonUnclear:
		return "unclear: " + cmp.Or(named(r.Step, r.RPC, ""), "the write") + " or the read (" + methodName(r.ReadRPC) + asText(r.Profile) + ")"
	case r.Kind == reasonKnockOn && r.Step == "":
		return "knock-on of " + named(r.Read, r.ReadRPC, "")
	case r.Kind == reasonKnockOn:
		return "knock-on of " + named(r.Step, r.RPC, "")
	}
	full := r.String()
	rest, ok := strings.CutPrefix(full, r.lead())
	if !ok {
		return full
	}
	rest = strings.TrimPrefix(rest, ": ")
	switch who := named(r.Step, r.RPC, r.Profile); {
	case who == "":
		return rest
	case rest == "":
		return who
	default:
		return who + ": " + rest
	}
}
