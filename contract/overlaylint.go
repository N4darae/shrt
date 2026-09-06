package contract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
)

type Issue struct {
	Domain   string `json:"domain,omitempty"`
	RPC      string `json:"rpc,omitempty"`
	Field    string `json:"field,omitempty"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func (i Issue) IsError() bool { return i.Severity == SeverityError }

const (
	SeverityError = "error"
	SeverityWarn  = "warn"
)

var validCheckedBy = map[string]bool{
	CheckedByFK: true, CheckedByAppLookup: true, CheckedByNone: true,
}

func LintLibrary(lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	for _, o := range lib.Overlays {
		issues = append(issues, lintOverlay(o, lib, cat)...)
	}
	issues = append(issues, lintCycles(lib, cat)...)
	issues = append(issues, lintAliasAgreement(lib, cat)...)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].RPC != issues[j].RPC {
			return issues[i].RPC < issues[j].RPC
		}
		return issues[i].Field < issues[j].Field
	})
	return issues
}

func lintOverlay(o *Overlay, lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	for i, f := range o.Failures {
		issues = append(issues, lintFailure(o.Domain, "", fmt.Sprintf("domain failure %d", i+1), f)...)
	}
	for _, rpc := range sortedRPCNames(o.RPCs) {
		issues = append(issues, lintRPC(o.Domain, rpc, o.RPCs[rpc], lib, cat)...)
	}
	return issues
}

func lintRPC(domain, rpc string, c *RPCContract, lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	add := func(sev, field, format string, args ...any) {
		issues = append(issues, Issue{
			Domain: domain, RPC: rpc, Field: field, Severity: sev, Message: fmt.Sprintf(format, args...),
		})
	}
	m, err := cat.Lookup(rpc)
	if err != nil {
		add(SeverityError, "", "%v", err)
		return issues
	}
	if rpc != m.FullName {
		add(SeverityWarn, "", "write the fully-qualified name %q so the key is stable", m.FullName)
	}
	if strings.TrimSpace(c.Summary) == "" {
		add(SeverityWarn, "", "no summary — an agent cannot tell what this rpc does")
	}
	if c.Status != StatusDraft && c.Status != StatusVerified {
		add(SeverityError, "", "status must be %q or %q, got %q", StatusDraft, StatusVerified, c.Status)
	}
	if c.Status == StatusVerified && c.VerifiedRun == "" {
		add(SeverityError, "", "status is verified but no verified_run names the run that proved it")
	}

	in := catalog.DescribeMessage(m.Input()).Fields
	out := catalog.DescribeMessage(m.Output()).Fields

	for _, name := range c.Required {
		if IsRequiredLiteral(name) {
			if len(c.Required) > 1 {
				add(SeverityError, name, "required mixes the literal %s with other entries — it is a statement "+
					"about the whole list and cannot sit beside a field name", strings.TrimSpace(name))
			}
			if strings.TrimSpace(name) == RequiredUnknown {
				add(SeverityWarn, name, "required is %s: the handler was not found, so nothing yet says which "+
					"fields the server rejects without. Honest, and still owed — a chain built from this rpc "+
					"lints clean while sending zero values", RequiredUnknown)
			}
			continue
		}
		if !catalog.HasPath(in, chain.SplitPath(name)) {
			add(SeverityError, name, "required lists %q which is not a field of %s", name, m.Input().FullName())
		}
	}
	issues = append(issues, lintFieldMap(domain, rpc, "fields", c.Fields, in, lib, cat)...)
	for _, alias := range sortedAliasNames(c.Aliases) {
		label := "aliases." + alias
		issues = append(issues, lintFieldMap(domain, rpc, label, c.Aliases[alias].Fields, in, lib, cat)...)
	}
	issues = append(issues, lintOneOf(domain, rpc, c)...)

	for _, name := range sortedKeys(c.Exports) {
		if !catalog.HasPath(out, chain.SplitPath(name)) {
			add(SeverityError, name, "exports names %q which is not a field of %s", name, m.Output().FullName())
		}
	}
	for _, name := range sortedKeys(c.Terminal) {
		if !catalog.HasPath(out, chain.SplitPath(name)) {
			add(SeverityError, name, "terminal names %q which is not a field of %s", name, m.Output().FullName())
		}
		if _, both := c.Exports[name]; both {
			add(SeverityError, name, "%q is listed in both exports and terminal", name)
		}
	}
	for _, name := range sortedKeys(c.SoftSignals) {
		if !catalog.HasPath(out, chain.SplitPath(name)) {
			add(SeverityError, name, "soft_signals names %q which is not a field of %s", name, m.Output().FullName())
		}
	}

	if len(c.RequiresRole) > 1 {
		for _, role := range c.RequiresRole {
			if strings.TrimSpace(role) == RoleNone {
				add(SeverityError, "requires_role",
					"requires_role lists %s alongside a real role — %s means this rpc reaches no role gate, so it stands alone or not at all",
					RoleNone, RoleNone)
				break
			}
		}
	}

	for _, need := range c.Needs {
		issues = append(issues, lintNode(domain, rpc, "needs", need, lib, cat)...)
	}
	for _, target := range c.Before {
		issues = append(issues, lintNode(domain, rpc, "before", target, lib, cat)...)
	}

	seen := map[string]int{}
	for i, f := range c.Failures {
		issues = append(issues, lintFailure(domain, rpc, fmt.Sprintf("failure %d", i+1), f)...)
		if f.Code != 0 {
			key := f.Label() + "\x00" + f.Field
			if prior, dup := seen[key]; dup {
				where := ""
				if f.Field != "" {
					where = " on field " + f.Field + ","
				}
				add(SeverityWarn, "", "failure %d repeats %s%s already listed as failure %d. "+
					"Two entries differing only in 'field:' are two branches, not a duplicate, and are "+
					"not reported — these two are the same branch written twice", i+1, f.Label(), where, prior)
			}
			seen[key] = i + 1
		}
	}
	return issues
}

func lintFieldMap(domain, rpc, label string, fields map[string]*FieldContract, in []*catalog.Field, lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	add := func(sev, field, format string, args ...any) {
		issues = append(issues, Issue{
			Domain: domain, RPC: rpc, Field: field, Severity: sev, Message: fmt.Sprintf(format, args...),
		})
	}
	for _, name := range sortedFieldNames(fields) {
		f := fields[name]
		qualified := label + "." + name
		if !catalog.HasPath(in, chain.SplitPath(name)) {
			m, _ := cat.Lookup(rpc)
			add(SeverityError, qualified, "%s has %q which is not a field of %s", label, name, m.Input().FullName())
			continue
		}
		if f.CheckedBy != "" && !validCheckedBy[f.CheckedBy] && !IsTodo(f.CheckedBy) {
			add(SeverityError, qualified, "checked_by must be %s, %s or %s, got %q",
				CheckedByFK, CheckedByAppLookup, CheckedByNone, f.CheckedBy)
		}
		if f.SameAs != "" {
			if f.From != "" {
				add(SeverityError, qualified, "from and same_as are mutually exclusive — from reads a response, same_as pins a request")
			}
			issues = append(issues, lintSameAs(domain, rpc, qualified, f.SameAs, lib, cat)...)
		}
		if f.From == "" {
			continue
		}
		if UsesLegacySeparator(f.From) {
			add(SeverityWarn, qualified, "from uses the legacy %q separator, write %q instead",
				LegacyRefSeparator, RefSeparator)
		}
		ref, err := ParseRef(f.From)
		if err != nil {
			add(SeverityError, qualified, "%v", err)
			continue
		}
		src, err := cat.Lookup(ref.RPC)
		if err != nil {
			add(SeverityError, qualified, "from names an unknown rpc: %v", err)
			continue
		}
		if !catalog.HasPath(catalog.DescribeMessage(src.Output()).Fields, chain.SplitPath(ref.Path)) {
			add(SeverityError, qualified, "from reads %q which is not a field of %s", ref.Path, src.Output().FullName())
		}
		issues = append(issues, lintAliasDeclared(domain, rpc, qualified, ref.RPC, ref.Alias, lib, cat)...)
	}
	return issues
}

func lintOneOf(domain, rpc string, c *RPCContract) []Issue {
	issues := []Issue{}
	for _, alias := range append([]string{""}, sortedAliasNames(c.Aliases)...) {
		groups := map[string][]string{}
		fields := c.FieldsFor(alias)
		for _, name := range sortedFieldNames(fields) {
			f := fields[name]
			if f.OneOf == "" {
				continue
			}
			if f.From != "" || f.Value != "" {
				groups[f.OneOf] = append(groups[f.OneOf], name)
			}
		}
		for _, group := range sortedKeys2(groups) {
			if len(groups[group]) > 1 {
				label := rpc
				if alias != "" {
					label = rpc + "@" + alias
				}
				issues = append(issues, Issue{
					Domain: domain, RPC: rpc, Field: "oneof." + group, Severity: SeverityError,
					Message: fmt.Sprintf("%s: oneof group %q has %d fields carrying a value (%s) — exactly one may",
						label, group, len(groups[group]), strings.Join(groups[group], ", ")),
				})
			}
		}
	}
	return issues
}

func lintNode(domain, rpc, label, node string, lib *Library, cat *catalog.Catalog) []Issue {
	target, alias := SplitNode(node)
	if _, err := cat.Lookup(target); err != nil {
		return []Issue{{Domain: domain, RPC: rpc, Field: label, Severity: SeverityError,
			Message: fmt.Sprintf("%s names an unknown rpc: %v", label, err)}}
	}
	return lintAliasDeclared(domain, rpc, label, target, alias, lib, cat)
}

func lintAliasDeclared(domain, rpc, label, target, alias string, lib *Library, cat *catalog.Catalog) []Issue {
	if alias == "" {
		return nil
	}
	m, err := cat.Lookup(target)
	if err != nil {
		return nil
	}
	c, ok := lib.Get(m.FullName)
	if !ok {
		return nil
	}
	if _, declared := c.Aliases[alias]; declared {
		return nil
	}
	return []Issue{{
		Domain: domain, RPC: rpc, Field: label, Severity: SeverityWarn,
		Message: fmt.Sprintf("alias %q is not declared under aliases on %s, so it only creates a second identical step — declare it there if the two instances must differ",
			alias, m.FullName),
	}}
}

func lintFailure(domain, rpc, label string, f Failure) []Issue {
	issues := []Issue{}
	add := func(sev, format string, args ...any) {
		issues = append(issues, Issue{Domain: domain, RPC: rpc, Field: label, Severity: sev,
			Message: fmt.Sprintf(format, args...)})
	}
	if f.Code == 0 && f.ConnectCode == "" && f.Reason == "" {
		add(SeverityError, "%s names nothing — give it a code, a connect_code or a reason", label)
	}
	if f.Code != 0 && f.Reason == "" {
		add(SeverityWarn, "%s has code %d but no reason", label, f.Code)
	}
	if f.Unreachable != "" && f.When != "" {
		add(SeverityWarn, "%s is marked unreachable, so when is misleading — fold it into unreachable", label)
	}
	if f.PendingDeploy != "" {
		if f.Unreachable != "" {
			add(SeverityError, "%s sets both unreachable and pending_deploy — they make opposite claims: unreachable by construction versus reachable in source but absent from the running binary", label)
		}
		if !commitish(f.PendingDeploy) {
			add(SeverityError, "%s pending_deploy must be a commit, not prose (%q) — the field earns its keep only because a gate can run git merge-base --is-ancestor against the deployed release", label, f.PendingDeploy)
		}
	}
	return issues
}

func commitish(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func lintCycles(lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	state := map[string]int{}

	var visit func(node string, trail []string) bool
	visit = func(node string, trail []string) bool {
		rpc, _ := SplitNode(node)
		if m, err := cat.Lookup(rpc); err == nil {
			rpc = m.FullName
		}
		switch state[rpc] {
		case 1:
			issues = append(issues, Issue{
				Domain: lib.Domain(rpc), RPC: rpc, Field: "needs", Severity: SeverityError,
				Message: "dependency cycle: " + strings.Join(append(trail, rpc), " -> "),
			})
			return true
		case 2:
			return false
		}
		state[rpc] = 1
		if c, ok := lib.Get(rpc); ok {
			for _, dep := range c.Dependencies() {
				if visit(dep, append(trail, rpc)) {
					break
				}
			}
		}
		for _, requires := range lib.RequiredBy(rpc) {
			if visit(requires, append(trail, rpc)) {
				break
			}
		}
		state[rpc] = 2
		return false
	}
	for _, rpc := range lib.RPCs() {
		visit(rpc, nil)
	}
	return issues
}

func sortedRPCNames(m map[string]*RPCContract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys2(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type consumerSite struct {
	rpc   string
	alias string
}

func lintAliasAgreement(lib *Library, cat *catalog.Catalog) []Issue {
	bySource := map[string][]consumerSite{}
	domainOf := map[string]string{}
	for _, o := range lib.Overlays {
		for _, rpc := range sortedRPCNames(o.RPCs) {
			domainOf[rpc] = o.Domain
			c := o.RPCs[rpc]
			for _, name := range sortedFieldNames(c.Fields) {
				ref, err := ParseRef(c.Fields[name].From)
				if err != nil {
					continue
				}
				producer := ref.RPC
				if m, err := cat.Lookup(producer); err == nil {
					producer = m.FullName
				}
				key := producer + "\x00" + ref.Path + "\x00" + name
				bySource[key] = append(bySource[key], consumerSite{rpc: rpc, alias: ref.Alias})
			}
		}
	}

	issues := []Issue{}
	for _, key := range sortedKeysOf(bySource) {
		sites := bySource[key]
		field := strings.SplitN(key, "\x00", 3)[2]
		for i, a := range sites {
			for _, b := range sites[i+1:] {
				if a.alias == b.alias || a.rpc == b.rpc {
					continue
				}
				consumer, producer, ok := dependentPair(a, b, lib, cat)
				if !ok {
					continue
				}
				issues = append(issues, Issue{
					Domain:   domainOf[consumer.rpc],
					RPC:      consumer.rpc,
					Field:    field,
					Severity: SeverityWarn,
					Message: fmt.Sprintf(
						"%s reads %s but %s, which this rpc depends on, writes %s — one chain, two instances. A write and a read that disagree about which instance lint clean, run green, and return nothing",
						field, describeInstance(consumer.alias), shortRPCName(producer.rpc), describeInstance(producer.alias)),
				})
			}
		}
	}
	return issues
}

func dependentPair(a, b consumerSite, lib *Library, cat *catalog.Catalog) (consumer, producer consumerSite, ok bool) {
	if directlyDepends(a.rpc, b.rpc, lib, cat) {
		return a, b, true
	}
	if directlyDepends(b.rpc, a.rpc, lib, cat) {
		return b, a, true
	}
	return consumerSite{}, consumerSite{}, false
}

func directlyDepends(consumer, producer string, lib *Library, cat *catalog.Catalog) bool {
	c, ok := lib.Get(consumer)
	if !ok {
		return false
	}
	canonical := func(rpc string) string {
		if m, err := cat.Lookup(rpc); err == nil {
			return m.FullName
		}
		return rpc
	}
	want := canonical(producer)
	for _, node := range c.Dependencies() {
		rpc, _ := SplitNode(node)
		if canonical(rpc) == want {
			return true
		}
	}
	for _, node := range lib.RequiredBy(consumer) {
		rpc, _ := SplitNode(node)
		if canonical(rpc) == want {
			return true
		}
	}
	return false
}

func describeInstance(alias string) string {
	if alias == "" {
		return "the un-aliased instance"
	}
	return "@" + alias
}

func shortRPCName(rpc string) string {
	if i := strings.LastIndex(rpc, "."); i >= 0 {
		return rpc[i+1:]
	}
	return rpc
}

func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func lintSameAs(domain, rpc, field, raw string, lib *Library, cat *catalog.Catalog) []Issue {
	issue := func(sev, format string, args ...any) Issue {
		return Issue{Domain: domain, RPC: rpc, Field: field, Severity: sev, Message: fmt.Sprintf(format, args...)}
	}
	ref, err := ParseRef(raw)
	if err != nil {
		return []Issue{issue(SeverityError, "same_as: %v", err)}
	}
	src, err := cat.Lookup(ref.RPC)
	if err != nil {
		return []Issue{issue(SeverityError, "same_as names an unknown rpc: %v", err)}
	}
	issues := []Issue{}
	if !catalog.HasPath(catalog.DescribeMessage(src.Input()).Fields, chain.SplitPath(ref.Path)) {
		issues = append(issues, issue(SeverityError,
			"same_as pins %q which is not a REQUEST field of %s — same_as reads what a step SENDS, not what it returns",
			ref.Path, src.Input().FullName()))
	}
	return append(issues, lintAliasDeclared(domain, rpc, field, ref.RPC, ref.Alias, lib, cat)...)
}
