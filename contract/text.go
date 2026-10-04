package contract

import (
	"fmt"
	"strings"
)

func indentText(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func (c *RPCContract) Text(lib *Library, rpc string) string {
	var b strings.Builder
	b.WriteString("CURATED CONTRACT\n")
	if c.Summary != "" {
		fmt.Fprintf(&b, "  summary\n%s\n", indentText(strings.TrimSpace(c.Summary), "    "))
	}
	fmt.Fprintf(&b, "  status         %s\n", c.Status)
	if c.DeclaresNoRole() {
		fmt.Fprintf(&b, "  requires_role  none — this rpc reaches no role gate\n")
	} else if len(c.RequiresRole) > 0 {
		fmt.Fprintf(&b, "  requires_role  %s\n", strings.Join(c.RequiresRole, " or "))
	}
	if len(c.Required) == 0 && c.IsUnfilled("required") {
		fmt.Fprintf(&b, "  required\n    %s (unfilled)\n", TodoMarker)
	}
	writeList(&b, "required", c.Required)
	writeList(&b, "needs (declared)", c.Needs)
	writeList(&b, "before (this must precede)", c.Before)
	writeList(&b, "depends on (resolved)", c.Dependencies())
	writeList(&b, "pulled in ahead of this", lib.RequiredBy(rpc))
	if len(c.Fields) > 0 {
		b.WriteString("  fields\n")
		for _, n := range sortedKeys(c.Fields) {
			fmt.Fprintf(&b, "    %-24s %s\n", n, fieldDetail(c.Fields[n]))
		}
	}
	for _, alias := range sortedKeys(c.Aliases) {
		fmt.Fprintf(&b, "  alias @%-12s %s\n", alias, c.Aliases[alias].Note)
		for _, n := range sortedKeys(c.Aliases[alias].Fields) {
			fmt.Fprintf(&b, "    %-24s %s\n", n, fieldDetail(c.Aliases[alias].Fields[n]))
		}
	}
	effects := map[string]string{}
	for k, e := range c.Effects {
		effects[k] = e.String()
	}
	writeMap(&b, "effects", effects)
	writeMap(&b, "exports", c.Exports)
	writeMap(&b, "terminal (no consumer)", c.Terminal)
	writeMap(&b, "soft_signals", c.SoftSignals)
	writeFailures(&b, lib, rpc)
	writeList(&b, "source", c.Source)
	return b.String()
}

func writeFailures(b *strings.Builder, lib *Library, rpc string) {
	all := lib.AllFailures(rpc)
	if len(all) == 0 {
		return
	}
	b.WriteString("  failures\n")
	inherited := len(lib.InheritedFailures(rpc))
	for i, f := range all {
		tag := ""
		if i < inherited {
			tag = "  [domain-wide]"
		}
		if f.Unreachable != "" {
			fmt.Fprintf(b, "    %-30s UNREACHABLE: %s%s\n", f.Label(), f.Unreachable, tag)
			continue
		}
		detail := f.When
		if f.ConnectCode != "" {
			detail = strings.TrimSpace(f.ConnectCode + " " + detail)
		}
		if f.Field != "" {
			detail = "[" + f.Field + "] " + detail
		}
		fmt.Fprintf(b, "    %-30s %s%s\n", f.Label(), detail, tag)
	}
}

func writeList(b *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "  %s\n", label)
	for _, v := range values {
		fmt.Fprintf(b, "    %s\n", v)
	}
}

func writeMap(b *strings.Builder, label string, m map[string]string) {
	if len(m) == 0 {
		return
	}
	fmt.Fprintf(b, "  %s\n", label)
	for _, k := range sortedKeys(m) {
		fmt.Fprintf(b, "    %-24s %s\n", k, m[k])
	}
}

func fieldDetail(f *FieldContract) string {
	parts := []string{}
	for _, kv := range [][2]string{{"value ", f.Value}, {"from ", f.From}, {"oneof ", f.OneOf}, {"checked_by ", f.CheckedBy}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+kv[1])
		}
	}
	if f.Note != "" {
		parts = append(parts, strings.TrimSpace(f.Note))
	}
	return strings.Join(parts, " — ")
}
