package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"google.golang.org/protobuf/reflect/protoreflect"
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
	Kind    string   `json:"kind,omitempty"`
	Step    string   `json:"step,omitempty"`
	RPC     string   `json:"rpc,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Read    string   `json:"read,omitempty"`
	ReadRPC string   `json:"read_rpc,omitempty"`
	Path    string   `json:"path,omitempty"`
	Want    string   `json:"want,omitempty"`
	Got     string   `json:"got,omitempty"`
	Other   string   `json:"other,omitempty"`
	Or      []reason `json:"or,omitempty"`
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

func asText(profile string) string {
	if profile == "" {
		return ""
	}
	return " as " + profile
}

func (r reason) String() string { return r.in(said{}) }

type said struct {
	row       bool
	head      *gateItem
	step, rpc string
}

func (r reason) in(s said) string {
	group := s.rpc != ""
	name := func(step, call, profile string) string {
		if !group {
			return fmt.Sprintf("%s %s (%s)%s", rw(call), step, shortRPC(call), asText(profile))
		}
		out := ""
		if step != s.step {
			out = rw(call) + " " + step
		}
		if shortRPC(call) != s.rpc {
			out += " (" + shortRPC(call) + ")"
		}
		return strings.TrimSpace(out + asText(profile))
	}
	switch {
	case r.Kind == "", group && r.Kind == reasonUnclear && strings.Contains(s.rpc, " or "):
		return ""
	case r.Kind == reasonUnclear && len(r.Or) > 1:
		names := []string{}
		for _, o := range r.Or[:2] {
			names = append(names, strings.TrimPrefix(name(o.Step, o.RPC, o.Profile), "write "))
		}
		if n := len(r.Or) - 2; n > 0 {
			names[1] += fmt.Sprintf(" +%d more", n)
		}
		return "unclear: write " + strings.Join(names, " or ")
	case r.Kind == reasonUnclear && (group || s.row && len(r.Or) == 0):
		read := " (" + methodName(r.ReadRPC) + asText(r.Profile) + ")"
		if s.head != nil && r.Read == s.head.Step {
			read = asText(r.Profile)
		}
		return "unclear: " + cmp.Or(name(r.Step, r.RPC, ""), "the write") + " or the read" + read
	case r.Kind == reasonUnclear:
		return fmt.Sprintf("unclear: %s or the read: answered %s=%s, but %s%s read %s", name(r.Step, r.RPC, ""),
			gateIndex.ReplaceAllString(r.Path, "[]$1"), valueText(r.Want), methodName(r.ReadRPC), asText(r.Profile), valueText(r.Got))
	case r.Kind == reasonKnockOn && r.Step == "":
		return "knock-on of " + name(r.Read, r.ReadRPC, "")
	case r.Kind == reasonKnockOn:
		return "knock-on of " + name(r.Step, r.RPC, "")
	}
	who, detail, rest := name(r.Step, r.RPC, r.Profile), r.detail(), ""
	if r.Kind != reasonWrite {
		rest = ": " + detail
	}
	switch h := s.head; {
	case group && (who == "" || detail == ""):
		return who + detail
	case group:
		return who + ": " + detail
	case h != nil && r.Kind == reasonStored && chain.EdgeQuoted(r.Want) == h.Want && chain.EdgeQuoted(r.Got) == h.Got && methodName(r.ReadRPC) == methodName(h.Call):
		return "suspect " + who + ": stores other than it answered"
	case h != nil && r.Step == h.Step:
		return "suspect the " + rw(r.RPC) + asText(r.Profile) + rest
	}
	return "suspect " + who + rest
}

func (r reason) detail() string {
	shown := gateIndex.ReplaceAllString(r.Path, "[]$1")
	switch r.Kind {
	case reasonStored:
		return fmt.Sprintf("answered %s=%s, but %s read %s", shown, valueText(r.Want), r.ReadRPC, valueText(r.Got))
	case reasonStoredOrder:
		return fmt.Sprintf("answered %s in another order than %s read", shown, r.ReadRPC)
	}
	detail := map[string]string{
		reasonRefused: "refused (" + r.Got + ")",
		reasonError:   "fails on its own (" + r.Got + ")",
		reasonProbe:   "fails its own auth probe (" + r.Path + ")",
		reasonProfile: "answers " + shown + " unlike as " + r.Other,
		reasonSet:     "answers another set of " + shown,
		reasonOrder:   "answers the same items in another order",
		reasonCode:    "answers " + r.Got + " where " + cmp.Or(r.Other, "it answered") + " " + r.Want,
		reasonDiffers: "answers " + shown + " unlike what " + r.Other + " returned",
		reasonSlow:    "slower than in the safe spot's run",
	}[r.Kind]
	if r.Kind == reasonOrder && r.Other != "" {
		detail += " (" + r.Other + ")"
	}
	if r.Kind == reasonRefused && r.Other != "" {
		detail += ", passes as " + r.Other
	}
	return detail
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

func tellApart(e *env, r reason, path string) string {
	if r.Kind != reasonUnclear || len(r.Or) > 0 || e == nil || e.cat == nil {
		return ""
	}
	m, err := e.cat.Lookup(r.ReadRPC)
	if err != nil {
		return ""
	}
	carrier, ok := carrierOf(m, path)
	if !ok {
		return ""
	}
	msg, field := carrier[:strings.LastIndex(carrier, ".")], leafName(carrier)
	var via []string
	for _, o := range e.cat.Methods() {
		if len(via) == 2 {
			break
		}
		if p := pathTo(o.Output(), msg, field, 0); p != "" && o.FullName != m.FullName && chain.IsReadOnlyCall(o.FullName) {
			if o.ServerStreaming {
				p = catalog.StreamMessages + "[]." + p
			}
			via = append(via, shortRPC(o.FullName)+" ("+p+")")
		}
	}
	if len(via) == 0 && r.Other == asSent {
		return fmt.Sprintf("%s answered %s as sent; only %s differs", r.Step, field, methodName(r.ReadRPC))
	}
	if len(via) == 0 {
		return ""
	}
	return "tell them apart: read " + gateIndex.ReplaceAllString(path, "[]$1") + " through " + strings.Join(via, " or ")
}

func pathTo(md protoreflect.MessageDescriptor, msg, field string, depth int) string {
	if md == nil || depth > 4 {
		return ""
	}
	if string(md.FullName()) == msg && md.Fields().ByName(protoreflect.Name(field)) != nil {
		return field
	}
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if fd.IsMap() {
			continue
		}
		if p := pathTo(fd.Message(), msg, field, depth+1); p != "" {
			if fd.IsList() {
				return string(fd.Name()) + "[]." + p
			}
			return string(fd.Name()) + "." + p
		}
	}
	return ""
}

func recordSent(e *env, rec *runner.Record) func(string) string {
	return func(step string) string {
		if st, ok := rec.Step(step); ok && st != nil {
			return sentText(e, st)
		}
		return ""
	}
}

func sentText(e *env, st *runner.StepRecord) string {
	var buf bytes.Buffer
	if len(st.Request) == 0 || json.Compact(&buf, st.Request) != nil {
		return ""
	}
	if as := asOf(e, st); as != "" {
		return " " + as + " sent " + capText(buf.String(), 300)
	}
	return " sent " + capText(buf.String(), 300)
}

const sameFault = "same fault as "
