package contract

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
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
	issues = append(issues, EffectProblems(lib, cat)...)
	slices.SortStableFunc(issues, func(a, b Issue) int {
		return cmp.Or(strings.Compare(a.RPC, b.RPC), strings.Compare(a.Field, b.Field))
	})
	return issues
}

func lintOverlay(o *Overlay, lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	for i, f := range o.Failures {
		issues = append(issues, lintFailure(o.Domain, "", fmt.Sprintf("domain failure %d", i+1), f)...)
	}
	for i, f := range o.Failures {
		if f.Scope != "" && f.Scope != FailureScopeAll {
			issues = append(issues, Issue{Domain: o.Domain, Field: fmt.Sprintf("domain failure %d", i+1), Severity: SeverityError,
				Message: fmt.Sprintf("scope %q is not a scope: leave it out for a failure every rpc of this domain shares, or "+
					"write scope: all for one every rpc of every domain shares", f.Scope)})
		}
	}
	for _, rpc := range sortedKeys(o.RPCs) {
		issues = append(issues, lintRPC(o.Domain, rpc, o.RPCs[rpc], lib, cat)...)
	}
	return issues
}

func issueAdder(domain, rpc string, issues *[]Issue) func(sev, field, format string, args ...any) {
	return func(sev, field, format string, args ...any) {
		*issues = append(*issues, Issue{Domain: domain, RPC: rpc, Field: field, Severity: sev, Message: fmt.Sprintf(format, args...)})
	}
}

func lintRPC(domain, rpc string, c *RPCContract, lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	add := issueAdder(domain, rpc, &issues)
	m, err := cat.Lookup(rpc)
	if errors.Is(err, catalog.ErrNotFound) {
		add(SeverityError, "", "rpc %q is not in the descriptor (removed from the proto?): delete this entry, or rebuild "+
			"the descriptor (shrt catalog build) if the rpc should still exist%s", rpc, cat.SuggestRPC(rpc))
		return issues
	}
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
	if msg, ok := lintListNeeds(c, m, lib, cat); ok {
		add(SeverityWarn, "needs", "%s", msg)
	}

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
			continue
		}
		if problem := indexProblem(in, name); problem != "" {
			add(SeverityError, name, "required: %s", problem)
		}
	}
	issues = append(issues, lintFieldMap(domain, rpc, "fields", c.Fields, in, lib, cat)...)
	baseGaps := map[string]bool{}
	for _, gap := range indexGaps(in, c.FieldsFor("")) {
		baseGaps[gap.message] = true
		add(SeverityWarn, "fields."+gap.list, "%s", gap.message)
	}
	for _, alias := range sortedKeys(c.Aliases) {
		label := "aliases." + alias
		issues = append(issues, lintFieldMap(domain, rpc, label, c.Aliases[alias].Fields, in, lib, cat)...)
		for _, gap := range indexGaps(in, c.FieldsFor(alias)) {
			if !baseGaps[gap.message] {
				add(SeverityWarn, label+".fields."+gap.list, "@%s: %s", alias, gap.message)
			}
		}
	}
	issues = append(issues, lintOneOf(domain, rpc, c)...)

	sections := map[string]map[string]string{"exports": c.Exports, "terminal": c.Terminal, "soft_signals": c.SoftSignals}
	for _, section := range []string{"exports", "terminal", "soft_signals"} {
		for _, name := range sortedKeys(sections[section]) {
			if !catalog.HasPath(out, chain.SplitPath(name)) && (section != "exports" || !catalog.HasPath(m.Response().Fields, chain.SplitPath(name))) {
				add(SeverityError, name, "%s names %q which is not a field of %s", section, name, m.Output().FullName())
			}
			if _, both := c.Exports[name]; both && section == "terminal" {
				add(SeverityError, name, "%q is listed in both exports and terminal", name)
			}
		}
	}

	if len(c.RequiresRole) > 1 && slices.ContainsFunc(c.RequiresRole, func(role string) bool { return strings.TrimSpace(role) == RoleNone }) {
		add(SeverityError, "requires_role",
			"requires_role lists %s alongside a real role — %s means this rpc reaches no role gate, so it stands alone or not at all",
			RoleNone, RoleNone)
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
		if f.Scope != "" {
			issues = append(issues, Issue{Domain: domain, RPC: rpc, Field: fmt.Sprintf("failure %d", i+1), Severity: SeverityError,
				Message: "scope: belongs on a failure in the domain-level failures: block, where scope: all shares it with every " +
					"rpc of every domain; a failure under one rpc is that rpc's alone"})
		}
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
	add := issueAdder(domain, rpc, &issues)
	for _, name := range sortedKeys(fields) {
		f := fields[name]
		qualified := label + "." + name
		if !catalog.HasPath(in, chain.SplitPath(name)) {
			m, _ := cat.Lookup(rpc)
			add(SeverityError, qualified, "%s has %q which is not a field of %s", label, name, m.Input().FullName())
			continue
		}
		if problem := indexProblem(in, name); problem != "" {
			add(SeverityError, qualified, "%s", problem)
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
	for _, alias := range append([]string{""}, sortedKeys(c.Aliases)...) {
		groups := map[string][]string{}
		fields := c.FieldsFor(alias)
		for _, name := range sortedKeys(fields) {
			if f := fields[name]; f.OneOf != "" && (f.From != "" || f.Value != "") {
				groups[f.OneOf] = append(groups[f.OneOf], name)
			}
		}
		for _, group := range sortedKeys(groups) {
			if len(groups[group]) > 1 {
				label := Ref{RPC: rpc, Alias: alias}.Node()
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
	add := issueAdder(domain, rpc, &issues)
	if f.Code == 0 && f.ConnectCode == "" && f.Reason == "" {
		add(SeverityError, label, "%s names nothing — give it a code, a connect_code or a reason", label)
	}
	if f.Code != 0 && f.Reason == "" {
		add(SeverityWarn, label, "%s has code %d but no reason", label, f.Code)
	}
	if f.Unreachable != "" && f.When != "" {
		add(SeverityWarn, label, "%s is marked unreachable, so when is misleading — fold it into unreachable", label)
	}
	if f.Unique != nil {
		if f.Unique.Case != "" && f.Unique.Case != UniqueCaseIgnore && f.Unique.Case != UniqueCaseExact {
			add(SeverityError, label, "%s unique.case is %q; it is %q (the backend compares the value ignoring letter case) or %q", label, f.Unique.Case, UniqueCaseIgnore, UniqueCaseExact)
		}
		if _, unique := uniquenessNoun(f); !unique {
			add(SeverityWarn, label, "%s sets unique: but is not a uniqueness refusal (a reason ending Taken, Exists, Duplicate... or a when saying unique or duplicate), so contract plan never reads it", label)
		}
	}
	if f.PendingDeploy != "" {
		if f.Unreachable != "" {
			add(SeverityError, label, "%s sets both unreachable and pending_deploy — they make opposite claims: unreachable by construction versus reachable in source but absent from the running binary", label)
		}
		if !commitish(f.PendingDeploy) {
			add(SeverityError, label, "%s pending_deploy must be a commit, not prose (%q) — the field earns its keep only because a gate can run git merge-base --is-ancestor against the deployed release", label, f.PendingDeploy)
		}
	}
	return issues
}

func commitish(s string) bool {
	return len(s) >= 7 && len(s) <= 40 && strings.Trim(s, "0123456789abcdefABCDEF") == ""
}

func lintCycles(lib *Library, cat *catalog.Catalog) []Issue {
	issues := []Issue{}
	state := map[string]int{}

	var visit func(node string, trail []string) bool
	visit = func(node string, trail []string) bool {
		rpc, alias := SplitNode(node)
		rpc = canonicalCall(cat, rpc)
		canonical := Ref{RPC: rpc, Alias: alias}.Node()
		switch state[canonical] {
		case 1:
			issues = append(issues, Issue{
				Domain: lib.Domain(rpc), RPC: rpc, Field: "needs", Severity: SeverityError,
				Message: "dependency cycle: " + strings.Join(append(trail, canonical), " -> "),
			})
			return true
		case 2:
			return false
		}
		state[canonical] = 1
		if c, ok := lib.Get(rpc); ok {
			for _, dep := range c.DependenciesFor(alias) {
				if visit(dep, append(trail, canonical)) {
					break
				}
			}
		}
		for _, requires := range lib.RequiredBy(rpc) {
			if visit(requires, append(trail, canonical)) {
				break
			}
		}
		state[canonical] = 2
		return false
	}
	for _, rpc := range lib.RPCs() {
		visit(rpc, nil)
		if c, ok := lib.Get(rpc); ok {
			for _, alias := range sortedKeys(c.Aliases) {
				visit(rpc+"@"+alias, nil)
			}
		}
	}
	return issues
}

type consumerSite struct {
	rpc   string
	alias string
}

func lintAliasAgreement(lib *Library, cat *catalog.Catalog) []Issue {
	bySource := map[string][]consumerSite{}
	domainOf := map[string]string{}
	for _, o := range lib.Overlays {
		for _, rpc := range sortedKeys(o.RPCs) {
			domainOf[rpc] = o.Domain
			c := o.RPCs[rpc]
			for _, name := range sortedKeys(c.Fields) {
				ref, err := ParseRef(c.Fields[name].From)
				if err != nil {
					continue
				}
				key := canonicalCall(cat, ref.RPC) + "\x00" + ref.Path + "\x00" + name
				bySource[key] = append(bySource[key], consumerSite{rpc: rpc, alias: ref.Alias})
			}
		}
	}

	issues := []Issue{}
	for _, key := range sortedKeys(bySource) {
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
						field, describeInstance(consumer.alias), shortMessage(producer.rpc), describeInstance(producer.alias)),
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
	want := canonicalCall(cat, producer)
	return slices.ContainsFunc(append(c.Dependencies(), lib.RequiredBy(consumer)...), func(node string) bool {
		rpc, _ := SplitNode(node)
		return canonicalCall(cat, rpc) == want
	})
}

func describeInstance(alias string) string {
	if alias == "" {
		return "the un-aliased instance"
	}
	return "@" + alias
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

func indexProblem(in []*catalog.Field, name string) string {
	segs := chain.SplitPath(name)
	fields := in
	var prev *catalog.Field
	prevIndex := false
	for i, seg := range segs {
		if chain.IsDigits(seg) {
			switch {
			case prev == nil || prevIndex:
				return fmt.Sprintf("%q puts index %s where a field name belongs — an index follows a repeated field, as in lines.1.qty", name, seg)
			case !prev.Repeated:
				return fmt.Sprintf("%q indexes %s, which is not a repeated field — an index can only pick an entry of a list", name, strings.Join(segs[:i], "."))
			case len(seg) > 1 && seg[0] == '0':
				return fmt.Sprintf("%q writes index %s with a leading zero — write %s", name, seg, strings.TrimLeft(seg, "0"))
			}
			if n, _ := strconv.Atoi(seg); n >= maxPlannedEntries {
				return fmt.Sprintf("%q asks for entry %d; a plan builds at most %d entries of one list", name, n, maxPlannedEntries)
			}
			prevIndex = true
			continue
		}
		prevIndex = false
		next := fieldByName(fields, seg)
		if next == nil || next.Truncated || next.MapKey != "" {
			return ""
		}
		prev = next
		fields = next.Fields
	}
	return ""
}

type indexGap struct {
	list    string
	message string
}

func indexGaps(in []*catalog.Field, fields map[string]*FieldContract) []indexGap {
	named := map[string]map[int]bool{}
	broadcast := map[string]bool{}
	for name := range fields {
		if indexProblem(in, name) != "" || !catalog.HasPath(in, chain.SplitPath(name)) {
			continue
		}
		segs := chain.SplitPath(name)
		cur := in
		for i, seg := range segs {
			if chain.IsDigits(seg) {
				continue
			}
			f := fieldByName(cur, seg)
			if f == nil || f.MapKey != "" || f.Truncated {
				break
			}
			cur = f.Fields
			if !f.Repeated || i+1 >= len(segs) {
				continue
			}
			list := strings.Join(segs[:i+1], ".")
			if chain.IsDigits(segs[i+1]) {
				n, _ := strconv.Atoi(segs[i+1])
				if named[list] == nil {
					named[list] = map[int]bool{}
				}
				named[list][n] = true
			} else {
				broadcast[list] = true
			}
		}
	}
	out := []indexGap{}
	for _, list := range sortedKeys(named) {
		if broadcast[list] {
			continue
		}
		top := 0
		for n := range named[list] {
			top = max(top, n)
		}
		missing := []string{}
		for n := 0; n < top; n++ {
			if !named[list][n] {
				missing = append(missing, fmt.Sprintf("%s.%d", list, n))
			}
		}
		if len(missing) == 0 {
			continue
		}
		out = append(out, indexGap{list: list, message: fmt.Sprintf(
			"%s.%d is declared but %s %s not, so a plan builds %d entries of %s and sends %s as the scaffold "+
				"left it, zeros and empty strings included. Declare every entry up to the highest index, or "+
				"write the shared part without an index (%s.<field> applies to every entry)",
			list, top, strings.Join(missing, ", "), pluralVerb(len(missing), "is", "are"), top+1, list,
			strings.Join(missing, ", "), list)})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
