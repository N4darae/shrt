package main

import (
	"fmt"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const (
	reasonWrite       = "write"
	reasonStored      = "stored"
	reasonStoredOrder = "stored-order"
	reasonUnclear     = "unclear"
	reasonKnockOn     = "knock-on"
	reasonRefused     = "refused"
	reasonError       = "error"
	reasonProbe       = "probe"
	reasonProfile     = "profile"
	reasonSet         = "set"
	reasonOrder       = "order"
	reasonCode        = "code"
	reasonDiffers     = "differs"
	reasonSlow        = "slow"
)

type reason struct {
	Kind    string `json:"kind,omitempty"`
	Step    string `json:"step,omitempty"`
	RPC     string `json:"rpc,omitempty"`
	Profile string `json:"profile,omitempty"`
	Read    string `json:"read,omitempty"`
	ReadRPC string `json:"read_rpc,omitempty"`
	Path    string `json:"path,omitempty"`
	Want    string `json:"want,omitempty"`
	Got     string `json:"got,omitempty"`
	Other   string `json:"other,omitempty"`
}

func (r reason) blames() bool {
	switch r.Kind {
	case reasonWrite, reasonStored, reasonStoredOrder, reasonUnclear, reasonKnockOn:
		return true
	}
	return false
}

func (r reason) blamed(step string) string {
	if r.blames() && r.Step != step {
		return r.Step
	}
	return ""
}

func (r reason) rpc(call string) string {
	if r.Kind == reasonKnockOn && r.Step == "" {
		return r.ReadRPC
	}
	if r.Kind != "" && r.RPC != "" {
		return r.RPC
	}
	return call
}

func (r reason) String() string {
	as := ""
	if r.Profile != "" {
		as = " as " + r.Profile
	}
	who := func(step, rpc string) string {
		return fmt.Sprintf("%s %s (%s)", rw(rpc), step, shortRPC(rpc))
	}
	shown := gateIndex.ReplaceAllString(r.Path, "[]$1")
	switch r.Kind {
	case "":
		return ""
	case reasonWrite:
		return "suspect " + who(r.Step, r.RPC) + as
	case reasonStored:
		return fmt.Sprintf("suspect %s%s: answered %s=%s, but %s read %s", who(r.Step, r.RPC), as, shown, valueText(r.Want), r.ReadRPC, valueText(r.Got))
	case reasonStoredOrder:
		return fmt.Sprintf("suspect %s%s: answered %s in another order than %s read", who(r.Step, r.RPC), as, shown, r.ReadRPC)
	case reasonUnclear:
		if r.Path == "" {
			return fmt.Sprintf("unclear: %s or %s%s", who(r.Step, r.RPC), who(r.Read, r.ReadRPC), as)
		}
		return fmt.Sprintf("unclear: %s answered %s=%s, %s%s got %s", who(r.Step, r.RPC), shown, valueText(r.Want), who(r.Read, r.ReadRPC), as, valueText(r.Got))
	case reasonKnockOn:
		if r.Step == "" {
			return "knock-on of " + who(r.Read, r.ReadRPC)
		}
		return "knock-on of " + who(r.Step, r.RPC)
	}
	detail := map[string]string{
		reasonRefused: "refused (" + r.Got + ")",
		reasonError:   "fails on its own (" + r.Got + ")",
		reasonProbe:   "fails its own auth probe (" + r.Path + ")",
		reasonProfile: "answers " + shown + " unlike as " + r.Other,
		reasonSet:     "answers another set of " + shown,
		reasonOrder:   "answers the same items in another order",
		reasonCode:    "answers " + r.Got + " where it answered " + r.Want,
		reasonDiffers: "answers " + shown + " unlike what " + r.Other + " returned",
		reasonSlow:    "slower than in the safe spot's run",
	}[r.Kind]
	if r.Kind == reasonRefused && r.Other != "" {
		detail += ", passes as " + r.Other
	}
	return "suspect " + who(r.Step, r.RPC) + as + ": " + detail
}

func rw(call string) string {
	if chain.IsReadOnlyCall(call) {
		return "read"
	}
	return "write"
}

func requestLine(r reason, step string, sent func(string) string) string {
	at := step
	if r.Kind == reasonKnockOn && r.Step == "" {
		at = r.Read
	} else if r.Kind != "" && r.Step != "" {
		at = r.Step
	}
	if body := sent(at); body != "" {
		return at + body
	}
	return ""
}

func suspectLine(r reason, step string, sent func(string) string) string {
	req := requestLine(r, step, sent)
	if s := r.String(); s != "" && req != "" {
		return s + "; " + req
	}
	return req
}

func recordSent(rec *runner.Record) func(string) string {
	return func(step string) string {
		if st, ok := rec.Step(step); ok && st != nil {
			return sentText(st)
		}
		return ""
	}
}

const sameFault = "same fault as "
