package chain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

type Issue struct {
	Step     string `json:"step,omitempty"`
	Severity string `json:"severity"`
	Kind     string `json:"kind,omitempty"`
	Message  string `json:"message"`
}

func (i Issue) IsError() bool { return i.Severity == SeverityError }

const (
	SeverityError = "error"
	SeverityWarn  = "warn"
)

type LintOptions struct {
	AuthHeader func(*Step) (profile, header string, covered bool)
	Env        func(string) (string, bool)
}

func Lint(c *Chain, cat *catalog.Catalog) []Issue {
	return LintWith(c, cat, LintOptions{})
}

func LintWith(c *Chain, cat *catalog.Catalog, opts LintOptions) []Issue {
	issues := []Issue{}
	issues = append(issues, lintVars(c)...)
	issues = append(issues, lintExternalInputs(c, opts.Env)...)
	known := map[string]bool{}
	knownExports := map[string]bool{}
	responses := map[string]*catalog.Method{}
	for _, s := range c.Steps {
		m, err := cat.Lookup(s.Call)
		if err != nil {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: err.Error()})
			known[s.ID] = true
			continue
		}
		issues = append(issues, lintStreaming(s, m)...)
		issues = append(issues, lintBody(s, m, cat)...)
		issues = append(issues, lintRefs(s, known, knownExports, responses)...)
		issues = append(issues, lintExpectPaths(s, m)...)
		issues = append(issues, lintExports(s, m)...)
		issues = append(issues, lintAuth(s, opts.AuthHeader)...)
		issues = append(issues, lintTransport(s, m)...)
		issues = append(issues, lintExpectRefs(s, known, knownExports)...)
		issues = append(issues, lintExpectRules(s)...)
		issues = append(issues, lintAssertsSomething(s)...)
		known[s.ID] = true
		responses[s.ID] = m
		for name := range s.Export {
			knownExports[name] = true
		}
	}
	return issues
}

// lintExpectRefs judges ${...} inside an expect. Comparison values (equals / not_equal / contains)
// now RESOLVE at run time — that is what lets a chain assert a money invariant across two steps —
// so the only error left is a reference in PATH, which names a location in this step's own response
// and has no value to resolve to.
//
// The referenced step must still run EARLIER, checked against the same `known` set body references
// use: an expect naming a later step would resolve to nothing and the assertion would compare
// against emptiness while looking deliberate.
func lintExpectRefs(s *Step, known, knownExports map[string]bool) []Issue {
	issues := []Issue{}
	for _, e := range s.Expect {
		for _, ref := range collectRefs([]any{e.Path}) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"expect path %q carries ${%s} — a path names a location in this step's own response, not a value, so it cannot resolve. Put the reference in equals/not_equal/contains instead",
				e.Path, ref)})
		}
		for _, ref := range collectRefs([]any{e.Equals, e.NotEqual, e.Contains}) {
			if why := referenceProblem(ParseRef(ref), known, knownExports); why != "" {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
					"expect on %q carries ${%s}, which %s", e.Path, ref, why)})
			}
		}
	}
	return issues
}

func referenceProblem(r Ref, known, knownExports map[string]bool) string {
	if r.Err != nil {
		return "cannot resolve: " + r.Err.Error()
	}
	switch r.Kind {
	case RefStep:
		if !known[r.Head] {
			return fmt.Sprintf("refers to step %q which does not run before this step", r.Head)
		}
	case RefExports:
		if name, ok := r.ExportName(); ok && !knownExports[name] {
			return "reads an export no earlier step declares. Export names come from a step's " +
				"export: block, so a typo resolves to nothing and the run dies on it"
		}
	case RefBare:
		if !known[r.Head] && !knownExports[r.Head] {
			return "names neither a step that runs before this step nor an export any earlier step declares"
		}
	}
	return ""
}

func lintStreaming(s *Step, m *catalog.Method) []Issue {
	if !m.Streaming() {
		return nil
	}
	return []Issue{{Step: s.ID, Severity: SeverityError, Message: m.StreamRefusal()}}
}

func lintAuth(s *Step, coverage func(*Step) (string, string, bool)) []Issue {
	issues := []Issue{}
	if s.Auth != "" && s.SkipAuth {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
			"skip_auth and auth: %q contradict each other — skip_auth sends no token at all, drop one", s.Auth)})
	}
	if name, ok := headerNamed(s.Headers, "Authorization"); ok && s.SkipAuth {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
			"skip_auth with a hand-written %q header is the workaround auth profiles replaced: it pins one "+
				"principal into one step, with no refresh, so the chain fails tomorrow for a reason that is "+
				"not a regression. Declare the principal as a profile in .shrt/config.yaml and name it with "+
				"auth: <profile>", name)})
		return issues
	}
	if coverage == nil {
		if name, ok := headerNamed(s.Headers, "Authorization"); ok {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Message: fmt.Sprintf(
				"%q is written by hand and the auth middleware overwrites it whenever a profile covers this "+
					"call, so this value is discarded rather than sent. Keep it only if your config declares no "+
					"auth block at all", name)})
		}
		return issues
	}
	profile, header, covered := coverage(s)
	if !covered {
		return issues
	}
	if name, ok := headerNamed(s.Headers, header); ok {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
			"%q is written by hand, and auth profile %q covers this call, so the auth middleware "+
				"overwrites the header with that profile's token: the value written here is never sent and "+
				"the step runs as %q's principal while reading as another's. To call as a different "+
				"principal, declare it as a profile in .shrt/config.yaml and name it with auth: <profile>",
			name, profile, profile)})
	}
	return issues
}

func headerNamed(headers map[string]string, want string) (string, bool) {
	for name := range headers {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(want)) {
			return name, true
		}
	}
	return "", false
}

func lintBody(s *Step, m *catalog.Method, cat *catalog.Catalog) []Issue {
	if len(s.Body) == 0 {
		return nil
	}
	schema := catalog.DescribeMessage(m.Input())
	raw, err := json.Marshal(Probe(s.Body, schema))
	if err != nil {
		return []Issue{{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf("body is not JSON encodable: %v", err)}}
	}
	if err := cat.ValidateInput(m, raw); err != nil {
		return []Issue{{Step: s.ID, Severity: SeverityError, Message: err.Error()}}
	}
	return nil
}

func Probe(body map[string]any, schema *catalog.Schema) any {
	return probeValue(body, schema.Fields)
}

func probeValue(v any, fields []*catalog.Field) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			f := findField(fields, k)
			if f == nil {
				out[k] = item
				continue
			}
			out[k] = probeField(item, f)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, probeValue(item, fields))
		}
		return out
	default:
		return v
	}
}

func probeField(v any, f *catalog.Field) any {
	if list, ok := v.([]any); ok {
		out := make([]any, 0, len(list))
		for _, item := range list {
			out = append(out, probeElement(item, f))
		}
		return out
	}
	if hasRef(v) && f.Repeated && f.MapKey == "" {
		return []any{probeElement(v, f)}
	}
	return probeElement(v, f)
}

func probeElement(v any, f *catalog.Field) any {
	if hasRef(v) {
		return placeholder(f)
	}
	if t, ok := v.(map[string]any); ok {
		return probeValue(t, f.Fields)
	}
	return v
}

func findField(fields []*catalog.Field, name string) *catalog.Field {
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func placeholder(f *catalog.Field) any {
	if len(f.EnumValues) > 0 {
		return f.EnumValues[0]
	}
	if v, ok := f.WellKnownExample(); ok {
		return v
	}
	switch f.Kind {
	case "bool":
		return false
	case "string":
		return "shrt-placeholder"
	case "bytes":
		return ""
	case "float", "double":
		return 0
	case "int64", "uint64", "sint64", "fixed64", "sfixed64":
		return "0"
	case "int32", "uint32", "sint32", "fixed32", "sfixed32":
		return 0
	case "message", "group":
		return map[string]any{}
	default:
		return "shrt-placeholder"
	}
}

func lintRefs(s *Step, known, knownExports map[string]bool, responses map[string]*catalog.Method) []Issue {
	issues := []Issue{}
	refs := append(collectRefs(s.Body), collectRefs(headerValues(s.Headers))...)
	for _, ref := range refs {
		r := ParseRef(ref)
		if why := referenceProblem(r, known, knownExports); why != "" {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf("${%s} %s", ref, why)})
			continue
		}
		if r.Kind == RefStep {
			if issue, bad := refPathIssue(s.ID, r, responses); bad {
				issues = append(issues, issue)
			}
		}
	}
	return issues
}

func refPathIssue(stepID string, r Ref, responses map[string]*catalog.Method) (Issue, bool) {
	ref, producer := r.Expr, r.Head
	m, ok := responses[producer]
	if !ok || m == nil {
		return Issue{}, false
	}
	rest := strings.TrimPrefix(r.Rest, "response.")
	if rest == "" || strings.HasPrefix(rest, "request") {
		return Issue{}, false
	}
	if catalog.HasPath(catalog.DescribeMessage(m.Output()).Fields, SplitPath(rest)) {
		return Issue{}, false
	}
	return Issue{
		Step:     stepID,
		Severity: SeverityWarn,
		Kind:     KindDeadRef,
		Message: fmt.Sprintf(
			"${%s} reads %q, which is not a field of %s — step %q cannot produce it, so this resolves to "+
				"nothing at run time, after every earlier step has already hit the backend. An export under that "+
				"name is a different thing: write ${exports.<name>} for that",
			ref, rest, m.Output().FullName(), producer),
	}, true
}

func headerValues(in map[string]string) []any {
	out := make([]any, 0, len(in))
	for _, v := range in {
		out = append(out, v)
	}
	return out
}

func lintAssertsSomething(s *Step) []Issue {
	if len(s.Expect) > 0 {
		return nil
	}
	return []Issue{{
		Step:     s.ID,
		Severity: SeverityWarn,
		Kind:     KindAssertsNone, Message: "asserts nothing at all, so it passes whatever the server answers — even an empty body " +
			"or a refusal. Give it at least one expect entry saying what this step should have done",
	}}
}

func indexedForm(path, at string) string {
	if len(path) <= len(at) {
		return path + ".0"
	}
	return at + ".0" + path[len(at):]
}

func lintExpectPaths(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	schema := catalog.DescribeMessage(m.Output())
	for _, e := range s.Expect {
		if e.Path == "" || refPattern.MatchString(e.Path) || IsTransportPath(e.Path) {
			continue
		}
		absent := e.Exists != nil && !*e.Exists
		if catalog.HasPath(schema.Fields, SplitPath(e.Path)) {
			if at, ok := catalog.MissingIndex(schema.Fields, SplitPath(e.Path)); ok {
				if absent {
					issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Kind: KindUnfailable, Message: fmt.Sprintf(
						"expect on %q says 'exists: false' on a path that reads THROUGH the repeated field %q "+
							"without saying which element, and such a path is never present — so the assertion "+
							"passes on every response and proves nothing. Write %s if absence of that element's "+
							"field is the point", e.Path, at, indexedForm(e.Path, at))})
					continue
				}
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Kind: KindUnreachable, Message: fmt.Sprintf(
					"expect on %q reads THROUGH the repeated field %q without saying which element, so it "+
						"can never match: a list is indexed, and %q is a list of objects rather than an "+
						"object. Write %s. 'shrt contract show' printed the un-indexed form until "+
						"2026-09-22, so a path pasted from it lints clean and fails at run time with "+
						"'path not present in response'", e.Path, at, at, indexedForm(e.Path, at))})
				continue
			}
			if why := EnumTautologyReason(e, enumValuesAt(schema.Fields, SplitPath(e.Path))); why != "" {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnfailable, Message: fmt.Sprintf(
					"expect on %q %s, so it passes whatever the server answers. An assertion that cannot "+
						"fail is the one fault no gate downstream can see — a green step proves nothing. "+
						"Assert the value this step should have produced", e.Path, why)})
			}
			continue
		}
		if absent {
			issues = append(issues, Issue{
				Step:     s.ID,
				Severity: SeverityError,
				Kind:     KindUnfailable,
				Message: fmt.Sprintf(
					"expect on %q says 'exists: false', but %s has no field at that path, so it is absent "+
						"from every response and the assertion cannot fail — a misspelt field name here is a "+
						"green that proves nothing. Name a field the message declares; if the field is new, "+
						"rebuild the descriptor with 'shrt catalog build'",
					e.Path, m.Output().FullName()),
			})
			continue
		}
		issues = append(issues, Issue{
			Step:     s.ID,
			Severity: SeverityError,
			Kind:     KindUnreachable,
			Message: fmt.Sprintf(
				"expect on %q reads a path that is not a field of %s, so it can never be present — "+
					"the assertion cannot pass on a well-formed response, and -dry-run would not say so. "+
					"Fix the path; if the field is new, rebuild the descriptor with 'shrt catalog build'%s",
				e.Path, m.Output().FullName(), transportHint(e.Path)),
		})
	}
	return issues
}

func lintExports(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	schema := catalog.DescribeMessage(m.Output())
	for name, path := range s.Export {
		if !catalog.HasPath(schema.Fields, SplitPath(path)) {
			issues = append(issues, Issue{
				Step:     s.ID,
				Severity: SeverityWarn,
				Kind:     KindBadExport, Message: fmt.Sprintf("export %q reads %q which is not a field of %s", name, path, m.Output().FullName()),
			})
		}
	}
	return issues
}

func isIndex(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func ExternalInputs(c *Chain) (vars []string, env []string) {
	wantVar := map[string]bool{}
	wantEnv := map[string]bool{}
	for _, s := range c.Steps {
		refs := append(collectRefs(s.Body), collectRefs(headerValues(s.Headers))...)
		for _, e := range s.Expect {
			refs = append(refs, collectRefs([]any{e.Equals, e.NotEqual, e.Contains})...)
		}
		for _, ref := range refs {
			r := ParseRef(ref)
			name, _, _ := strings.Cut(r.Rest, ".")
			if name == "" {
				continue
			}
			switch r.Kind {
			case RefVars:
				if _, declared := c.Vars[name]; !declared {
					wantVar[name] = true
				}
			case RefEnv:
				wantEnv[r.Rest] = true
			}
		}
	}
	return sortedKeys(wantVar), sortedKeys(wantEnv)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func lintExternalInputs(c *Chain, env func(string) (string, bool)) []Issue {
	issues := []Issue{}
	missingVars, needEnv := ExternalInputs(c)
	if len(missingVars) > 0 {
		issues = append(issues, Issue{Severity: SeverityWarn, Message: fmt.Sprintf(
			"reads ${vars.%s} which this chain does not declare in vars: — the run needs %s, "+
				"and without it the run dies at the first step that reads one, after every step before it has already hit the backend",
			strings.Join(missingVars, "}, ${vars."),
			"-var "+strings.Join(missingVars, "=... -var ")+"=...")})
	}
	if env == nil {
		if len(needEnv) > 0 {
			issues = append(issues, Issue{Severity: SeverityWarn, Message: fmt.Sprintf(
				"reads these environment variables, and the run dies at the first step that reads one "+
					"that is unset: %s", strings.Join(needEnv, ", "))})
		}
		return issues
	}
	unset := []string{}
	for _, name := range needEnv {
		if _, ok := env(name); !ok {
			unset = append(unset, name)
		}
	}
	if len(unset) > 0 {
		issues = append(issues, Issue{Severity: SeverityWarn, Message: fmt.Sprintf(
			"reads environment variables that are not exported in this shell: %s — the run dies at the "+
				"first step that reads one, after every step before it has already hit the backend",
			strings.Join(unset, ", "))})
	}
	return issues
}

func lintVars(c *Chain) []Issue {
	issues := []Issue{}
	for _, name := range sortedVarNames(c.Vars) {
		text, ok := c.Vars[name].(string)
		if !ok {
			continue
		}
		for _, ref := range collectRefs([]any{text}) {
			issues = append(issues, Issue{Severity: SeverityError, Message: fmt.Sprintf(
				"var %q carries ${%s}, and a var value is NOT resolved — it is stored and handed back verbatim, so the literal text would be sent to the server and every check would still pass. Put the reference in the body that uses it, or supply the value with -var at run time",
				name, ref)})
		}
	}
	return issues
}

func sortedVarNames(vars map[string]any) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ruleNames(e Expectation) []string {
	names := []string{}
	if e.Exists != nil {
		names = append(names, "exists")
	}
	if e.NotEmpty {
		names = append(names, "not_empty")
	}
	if e.Contains != "" {
		names = append(names, "contains")
	}
	if e.NotEqual != nil {
		names = append(names, "not_equal")
	}
	if e.Equals != nil {
		names = append(names, "equals")
	}
	return names
}

func lintExpectRules(s *Step) []Issue {
	issues := []Issue{}
	for _, e := range s.Expect {
		names := ruleNames(e)
		switch {
		case len(names) == 0:
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"expect on %q carries no rule, so it asserts nothing", e.Path)})
		case len(names) > 1:
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"expect on %q carries %d rules (%s) and only ONE can fire: Evaluate picks them in the fixed order exists > not_empty > contains > not_equal > equals, so %q wins and the rest are discarded silently. Split them into separate expect entries",
				e.Path, len(names), strings.Join(names, ", "), names[0])})
		}
		if why := TautologyReason(e); why != "" {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnfailable, Message: fmt.Sprintf(
				"expect on %q %s, so it passes whatever the server answers. An assertion that cannot "+
					"fail is the one fault no gate downstream can see — a green step proves nothing. "+
					"Assert the value this step should have produced",
				e.Path, why)})
		}
	}
	return issues
}

func enumValuesAt(fields []*catalog.Field, segs []string) []string {
	for len(segs) > 0 && isIndexSegment(segs[0]) {
		segs = segs[1:]
	}
	if len(segs) == 0 {
		return nil
	}
	for _, f := range fields {
		if !namecase.Equal(f.Name, segs[0]) {
			continue
		}
		rest := segs[1:]
		for len(rest) > 0 && isIndexSegment(rest[0]) {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return f.EnumValues
		}
		return enumValuesAt(f.Fields, rest)
	}
	return nil
}

func isIndexSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func LintCorpus(chains []*Chain) []Issue {
	issues := []Issue{}
	byName := map[string][]string{}
	for _, c := range chains {
		if c == nil || c.Name == "" {
			continue
		}
		byName[c.Name] = append(byName[c.Name], c.SourcePath)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		paths := byName[name]
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		issues = append(issues, Issue{
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"%d files declare name %q: %s. The name keys the run directory and the safe spot, so one "+
					"file's run lands in another's history and a human confirming a run id cannot tell "+
					"which file produced it",
				len(paths), name, strings.Join(paths, ", ")),
		})
	}
	return issues
}

const (
	KindUnfailable  = "unfailable-assertion"
	KindAssertsNone = "asserts-nothing"
	KindUnreachable = "unreachable-path"
	KindDeadRef     = "unproducible-reference"
	KindBadExport   = "export-reads-nonfield"
)

func IsAssertionQualityIssue(i Issue) bool {
	switch i.Kind {
	case KindUnfailable, KindAssertsNone, KindUnreachable, KindDeadRef, KindBadExport:
		return true
	}
	return false
}

func Promote(issues []Issue, promote func(Issue) bool) []Issue {
	out := make([]Issue, 0, len(issues))
	for _, i := range issues {
		if i.Severity == SeverityWarn && promote(i) {
			i.Severity = SeverityError
		}
		out = append(out, i)
	}
	return out
}
