package chain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
	AuthHeader   func(*Step) (profile, header string, covered bool)
	AuthProfiles []string
	AuthEnv      func(profile string) []string
	Env          func(string) (string, bool)
}

func Lint(c *Chain, cat *catalog.Catalog) []Issue {
	return LintWith(c, cat, LintOptions{})
}

func LintWith(c *Chain, cat *catalog.Catalog, opts LintOptions) []Issue {
	issues := []Issue{}
	issues = append(issues, lintVars(c)...)
	issues = append(issues, lintExternalInputs(c, opts.Env)...)
	issues = append(issues, lintExportNames(c)...)
	issues = append(issues, lintAuthEnv(c, opts)...)
	known := map[string]bool{}
	knownExports := map[string]bool{}
	responses := map[string]*catalog.Method{}
	exports := map[string]exportOrigin{}
	idx := newRefIndex(c)
	for _, s := range c.Steps {
		m, err := cat.Lookup(s.Call)
		if err != nil {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: err.Error()})
			known[s.ID] = true
			continue
		}
		issues = append(issues, lintRefSyntax(s)...)
		issues = append(issues, lintStreaming(s, m)...)
		issues = append(issues, lintBody(s, m, cat)...)
		issues = append(issues, lintRefs(s, known, knownExports, responses, idx)...)
		for _, why := range refTypeProblems(s, m, responses, exports) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Kind: KindDeadRef, Message: why})
		}
		issues = append(issues, lintExpectPaths(s, m)...)
		issues = append(issues, lintExports(s, m)...)
		issues = append(issues, lintAuth(s, opts.AuthHeader)...)
		issues = append(issues, lintAuthProfile(s, opts.AuthProfiles)...)
		issues = append(issues, lintTransport(s, m)...)
		issues = append(issues, lintExpectRefs(s, known, knownExports, responses, idx)...)
		issues = append(issues, lintExpectRules(s)...)
		issues = append(issues, lintAssertsSomething(s)...)
		issues = append(issues, lintInertAllowFail(s)...)
		known[s.ID] = true
		responses[s.ID] = m
		noteExports(s, exports)
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
func lintExpectRefs(s *Step, known, knownExports map[string]bool, responses map[string]*catalog.Method, idx *refIndex) []Issue {
	issues := []Issue{}
	for _, e := range s.Expect {
		for _, ref := range collectRefs([]any{e.Path}) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"expect path %q carries ${%s} — a path names a location in this step's own response, not a value, so it cannot resolve. Put the reference in equals/not_equal/contains instead",
				e.Path, ref)})
		}
		for _, ref := range collectRefs([]any{e.Equals, e.NotEqual, e.Contains}) {
			r := ParseRef(ref)
			if why, own := ownStepProblem(s.ID, r, knownExports); own {
				if why != "" {
					issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
						"expect on %q carries ${%s}, which %s", e.Path, ref, why)})
				}
				continue
			}
			if why := referenceProblem(r, known, knownExports, idx); why != "" {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
					"expect on %q carries ${%s}, which %s", e.Path, ref, why)})
				continue
			}
			if r.Kind == RefStep {
				if issue, bad := refPathIssue(s.ID, r, responses); bad {
					issue.Message = fmt.Sprintf("expect on %q: %s", e.Path, issue.Message)
					issues = append(issues, issue)
				}
			}
		}
	}
	return issues
}

func ownStepProblem(stepID string, r Ref, knownExports map[string]bool) (string, bool) {
	if r.Err != nil || (r.Kind != RefStep && r.Kind != RefBare) || r.Head != stepID {
		return "", false
	}
	if r.Kind == RefBare && knownExports[r.Head] {
		return "", false
	}
	if section, _, _ := strings.Cut(r.Rest, "."); r.Kind == RefStep && section == "request" {
		return "", true
	}
	return fmt.Sprintf("reads step %q's own response. This expect already reads that response through "+
		"its path, so the reference compares the answer with itself: on the same path it cannot fail, "+
		"and on another it checks the server against nothing independent of it. Read this step's own "+
		"request (${steps.%s.request.<field>}) to assert the response echoes what was sent, or an "+
		"earlier step's response", stepID, stepID), true
}

type refIndex struct {
	steps      []string
	stepAt     map[string]int
	exports    []string
	exportedBy map[string]int
	exporter   map[string]string
}

func newRefIndex(c *Chain) *refIndex {
	idx := &refIndex{stepAt: map[string]int{}, exportedBy: map[string]int{}, exporter: map[string]string{}}
	for i, s := range c.Steps {
		idx.steps = append(idx.steps, s.ID)
		if _, seen := idx.stepAt[s.ID]; !seen {
			idx.stepAt[s.ID] = i + 1
		}
		names := make([]string, 0, len(s.Export))
		for name := range s.Export {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, seen := idx.exportedBy[name]; !seen {
				idx.exportedBy[name] = i + 1
				idx.exporter[name] = s.ID
				idx.exports = append(idx.exports, name)
			}
		}
	}
	return idx
}

func (x *refIndex) laterStep(id string) string {
	if at, ok := x.stepAt[id]; ok {
		return fmt.Sprintf("refers to step %q (step %d), which does not run before this step", id, at)
	}
	return ""
}

func (x *refIndex) laterExport(name string) string {
	if at, ok := x.exportedBy[name]; ok {
		return fmt.Sprintf("reads export %q, exported by %s at step %d, which runs later, so it has no value yet",
			name, x.exporter[name], at)
	}
	return ""
}

func didYouMean(name string, candidates []string) string {
	near := namecase.Closest(name, candidates, 3)
	if len(near) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(near))
	for _, n := range near {
		quoted = append(quoted, strconv.Quote(n))
	}
	return " (did you mean " + strings.Join(quoted, " or ") + "?)"
}

func referenceProblem(r Ref, known, knownExports map[string]bool, idx *refIndex) string {
	if r.Err != nil {
		return "cannot resolve: " + r.Err.Error()
	}
	switch r.Kind {
	case RefStep:
		if !known[r.Head] {
			if why := idx.laterStep(r.Head); why != "" {
				return why
			}
			return fmt.Sprintf("refers to step %q, which does not exist in this chain%s", r.Head, didYouMean(r.Head, idx.steps))
		}
	case RefExports:
		if name, ok := r.ExportName(); ok && !knownExports[name] {
			if why := idx.laterExport(name); why != "" {
				return why
			}
			return "reads an export no step in this chain declares" + didYouMean(name, idx.exports) + ". Export " +
				"names come from a step's export: block, so a typo resolves to nothing and the run dies on it"
		}
	case RefBare:
		if !known[r.Head] && !knownExports[r.Head] {
			if why := idx.laterExport(r.Head); why != "" {
				return why
			}
			if why := idx.laterStep(r.Head); why != "" {
				return why
			}
			return "names neither a step nor an export of this chain" +
				didYouMean(r.Head, append(append([]string{}, idx.steps...), idx.exports...))
		}
	}
	return ""
}

func lintRefSyntax(s *Step) []Issue {
	values := []any{s.Body}
	for _, name := range sortedHeaderNames(s.Headers) {
		values = append(values, s.Headers[name])
	}
	for _, e := range s.Expect {
		values = append(values, e.Equals, e.NotEqual, e.Contains)
	}
	issues := []Issue{}
	walkStrings(values, func(text string) {
		for _, why := range refSyntaxProblems(text) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindRefSyntax, Message: why})
		}
	})
	return issues
}

func refSyntaxProblems(text string) []string {
	out := []string{}
	for _, m := range refPattern.FindAllStringSubmatch(text, -1) {
		inner := m[1]
		trimmed := strings.TrimSpace(inner)
		if inner != trimmed {
			out = append(out, fmt.Sprintf("%q carries spaces inside the braces of %s; it resolves as ${%s}, "+
				"but reads as something else. Write ${%s}", text, m[0], trimmed, trimmed))
		}
		r := ParseRef(trimmed)
		if r.Err != nil || r.Rest == "" || (r.Kind != RefClock && r.Kind != RefUUID) {
			continue
		}
		if r.Kind == RefClock && r.Offset != 0 && isIndexSegment(r.Rest) {
			out = append(out, fmt.Sprintf("%q: the offset in ${%s} is a whole number of seconds, so it "+
				"resolves as ${%s} and the .%s is dropped. Write the offset in whole seconds", text, trimmed,
				r.Expr[:len(r.Expr)-len(r.Rest)-1], r.Rest))
			continue
		}
		out = append(out, fmt.Sprintf("%q: ${%s} takes no path, so .%s is ignored and it resolves as ${%s}",
			text, trimmed, r.Rest, r.Expr[:len(r.Expr)-len(r.Rest)-1]))
	}
	if rest := refPattern.ReplaceAllString(text, ""); strings.Contains(rest, "${") {
		out = append(out, fmt.Sprintf("%q carries ${ with no closing }, so no reference resolves there and "+
			"the text is sent as the literal characters. Close the brace; there is no escape for a literal "+
			"${, so a value that must contain one comes from ${env.NAME} or -var name=..., whose values are "+
			"never resolved again", text))
	}
	return out
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

func lintAuthProfile(s *Step, profiles []string) []Issue {
	if profiles == nil || s.Auth == "" || s.Auth == InvalidTokenAuth {
		return nil
	}
	for _, name := range profiles {
		if name == s.Auth {
			return nil
		}
	}
	if len(profiles) == 0 {
		return []Issue{{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
			"asks for auth profile %q, but the config declares no auth at all, so shrt run refuses the chain "+
				"before sending anything", s.Auth)}}
	}
	have := append([]string{}, profiles...)
	sort.Strings(have)
	return []Issue{{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
		"asks for auth profile %q, which the config does not define (have: %s), so shrt run refuses the "+
			"chain before sending anything%s", s.Auth, strings.Join(have, ", "), didYouMean(s.Auth, have))}}
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

func lintRefs(s *Step, known, knownExports map[string]bool, responses map[string]*catalog.Method, idx *refIndex) []Issue {
	issues := []Issue{}
	refs := append(collectRefs(s.Body), collectRefs(headerValues(s.Headers))...)
	for _, ref := range refs {
		r := ParseRef(ref)
		if why := referenceProblem(r, known, knownExports, idx); why != "" {
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
	why, bad := responseRefProblem(r, responses)
	if !bad {
		return Issue{}, false
	}
	hint := ". An export under that name is a different thing: write ${exports.<name>} for that"
	if strings.HasPrefix(r.Rest, "request.") {
		hint = ". A request path names a field of the request message that step sends, not of its response"
	}
	return Issue{
		Step:     stepID,
		Severity: SeverityError,
		Kind:     KindDeadRef,
		Message:  fmt.Sprintf("${%s} %s%s", r.Expr, why, hint),
	}, true
}

func responseRefProblem(r Ref, responses map[string]*catalog.Method) (string, bool) {
	if r.Kind != RefStep || r.Err != nil {
		return "", false
	}
	m, ok := responses[r.Head]
	if !ok || m == nil {
		return "", false
	}
	rest := r.Rest
	if rest == "request" {
		return "", false
	}
	if path, ok := strings.CutPrefix(rest, "request."); ok {
		if catalog.HasResponsePath(catalog.DescribeMessage(m.Input()).Fields, SplitPath(path)) {
			return "", false
		}
		return fmt.Sprintf("reads request path %q, which is not a field of %s — step %q never sends it, so the run "+
			"would die resolving it after every earlier step had already hit the backend, and shrt run refuses "+
			"the chain before sending anything", path, m.Input().FullName(), r.Head), true
	}
	rest = strings.TrimPrefix(rest, "response.")
	if rest == "" || rest == "response" {
		return "", false
	}
	fields := catalog.DescribeMessage(m.Output()).Fields
	if catalog.HasResponsePath(fields, SplitPath(rest)) {
		return "", false
	}
	return fmt.Sprintf("reads %q, which is not a field of %s — step %q cannot produce it, so the run would "+
		"die resolving it after every earlier step had already hit the backend, and shrt run refuses the "+
		"chain before sending anything", rest, m.Output().FullName(), r.Head), true
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

func lintInertAllowFail(s *Step) []Issue {
	if !s.AllowFail || len(s.Expect) == 0 {
		return nil
	}
	say := fmt.Sprintf("%s.code equals: permission_denied for a Connect error", TransportPrefix)
	inBand := ""
	if p := EnvelopePath(); p != "" {
		say += fmt.Sprintf(", or %s equals: <the refusal code> for an in-band refusal", p)
		inBand = fmt.Sprintf(", and an in-band refusal on %s is never covered by allow_fail", p)
	}
	return []Issue{{
		Step:     s.ID,
		Severity: SeverityWarn,
		Kind:     KindInertAllowFail,
		Message: fmt.Sprintf("allow_fail: true does nothing on this step: it tolerates only a transport refusal "+
			"(the call answered with a Connect error) on a step that declares NO expect, and this step "+
			"declares %d. Here the expectations alone decide the verdict — a refusal they do not describe "+
			"still fails the step%s. Drop allow_fail; if this step is meant to be refused, its expectations "+
			"are how you say which refusal (%s), and the step passes when it gets exactly that",
			len(s.Expect), inBand, say),
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
		if catalog.HasResponsePath(schema.Fields, SplitPath(e.Path)) {
			if exact, inexact := inexactPath(schema.Fields, e.Path); inexact {
				issues = append(issues, inexactPathIssue(s.ID, fmt.Sprintf("expect path %q", e.Path), exact, m))
			}
			if at, ok := catalog.ResponseMissingIndex(schema.Fields, SplitPath(e.Path)); ok {
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
						"object. Write %s. Unindexed, the path is never present in a response, and the step "+
						"fails at run time with 'path not present in response'", e.Path, at, at, indexedForm(e.Path, at))})
				continue
			}
			if why := EnumTautologyReason(e, enumValuesAt(schema.Fields, SplitPath(e.Path))); why != "" {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnfailable, Message: fmt.Sprintf(
					"expect on %q %s, so it passes whatever the server answers. An assertion that cannot "+
						"fail is the one fault no gate downstream can see — a green step proves nothing. "+
						"Assert the value this step should have produced", e.Path, why)})
			}
			if e.NotEqual != nil && IsVerdictPath(e.Path) && stringify(e.NotEqual) == "" && EnvelopeOK() != "" {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindUnfailable, Message: fmt.Sprintf(
					"expect on %q says not_equal \"\", which holds on the ok value %s and on every refusal alike: "+
						"no answer that carries a verdict can fail it, so it declares neither success nor a refusal, "+
						"and the runner does not accept it as pinning the verdict. Write equals: %s for a call that "+
						"must succeed, or the refusal code for one that must be refused",
					e.Path, EnvelopeOK(), EnvelopeOK())})
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
	names := make([]string, 0, len(s.Export))
	for name := range s.Export {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := s.Export[name]
		if exact, inexact := inexactPath(schema.Fields, path); inexact {
			issues = append(issues, inexactPathIssue(s.ID, fmt.Sprintf("export %q reads %q", name, path), exact, m))
		}
		if !catalog.HasResponsePath(schema.Fields, SplitPath(path)) {
			issues = append(issues, Issue{
				Step:     s.ID,
				Severity: SeverityError,
				Kind:     KindBadExport, Message: fmt.Sprintf("export %q reads %q which is not a field of %s, so shrt run "+
					"fails this step when the path is missing from the response", name, path, m.Output().FullName()),
			})
		}
	}
	return issues
}

func lintExportNames(c *Chain) []Issue {
	issues := []Issue{}
	stepAt := map[string]int{}
	for i, s := range c.Steps {
		if _, seen := stepAt[s.ID]; !seen {
			stepAt[s.ID] = i + 1
		}
	}
	writer := map[string]int{}
	for i, s := range c.Steps {
		names := make([]string, 0, len(s.Export))
		for name := range s.Export {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if at, clash := stepAt[name]; clash {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
					"export %q has the same name as step %q (step %d), so ${%s} is ambiguous: the bare reference "+
						"reads the export once one exists and the step's whole response otherwise, while "+
						"${%s.<field>} always reads the step. Rename the export", name, name, at, name, name)})
			}
			if prev, twice := writer[name]; twice {
				issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Kind: KindExportOverwritten, Message: fmt.Sprintf(
					"export %q is also written by step %q (step %d), and this later write silently replaces it: "+
						"every ${%s} after step %d reads this step's value, and none reads the first. Give each "+
						"export its own name", name, c.Steps[prev-1].ID, prev, name, i+1)})
			}
			writer[name] = i + 1
		}
	}
	return issues
}

func inexactPath(fields []*catalog.Field, path string) (string, bool) {
	segs := SplitPath(path)
	out := make([]string, 0, len(segs))
	for i, seg := range segs {
		if isIndexSegment(seg) {
			out = append(out, seg)
			continue
		}
		var f *catalog.Field
		for _, candidate := range fields {
			if candidate.Name == seg {
				f = candidate
				break
			}
		}
		if f == nil {
			for _, candidate := range fields {
				if namecase.Equal(candidate.Name, seg) {
					f = candidate
					break
				}
			}
		}
		if f == nil {
			return "", false
		}
		out = append(out, f.Name)
		if f.Truncated || f.MapKey != "" {
			out = append(out, segs[i+1:]...)
			break
		}
		fields = f.Fields
	}
	exact := strings.Join(out, ".")
	return exact, exact != strings.Join(segs, ".")
}

func inexactPathIssue(stepID, what, exact string, m *catalog.Method) Issue {
	return Issue{Step: stepID, Severity: SeverityWarn, Kind: KindInexactPath, Message: fmt.Sprintf(
		"%s, which matches a field of %s only by folding case and separators; the field is %q. It resolves "+
			"at run time, but a reader, a grep and a diff against the proto see a name the message does not "+
			"declare: write %q", what, m.Output().FullName(), exact, exact)}
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
				"and without it shrt run refuses the chain before sending anything",
			strings.Join(missingVars, "}, ${vars."),
			"-var "+strings.Join(missingVars, "=... -var ")+"=...")})
	}
	if env == nil {
		if len(needEnv) > 0 {
			issues = append(issues, Issue{Severity: SeverityWarn, Message: fmt.Sprintf(
				"reads these environment variables, and shrt run refuses the chain before sending anything "+
					"while one is unset: %s", strings.Join(needEnv, ", "))})
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
			"reads environment variables that are not exported in this shell: %s — shrt run refuses the "+
				"chain before sending anything until they are set",
			strings.Join(unset, ", "))})
	}
	return issues
}

func lintAuthEnv(c *Chain, opts LintOptions) []Issue {
	if opts.AuthHeader == nil || opts.AuthEnv == nil || opts.Env == nil {
		return nil
	}
	firstStep := map[string]string{}
	order := []string{}
	for _, s := range c.Steps {
		if s == nil || s.SkipAuth || s.Auth == InvalidTokenAuth {
			continue
		}
		profile, _, covered := opts.AuthHeader(s)
		if !covered {
			continue
		}
		if _, seen := firstStep[profile]; !seen {
			firstStep[profile] = s.ID
			order = append(order, profile)
		}
	}
	issues := []Issue{}
	for _, profile := range order {
		unset := []string{}
		for _, name := range opts.AuthEnv(profile) {
			if _, ok := opts.Env(name); !ok {
				unset = append(unset, name)
			}
		}
		if len(unset) == 0 {
			continue
		}
		issues = append(issues, Issue{Severity: SeverityWarn, Message: fmt.Sprintf(
			"from step %q on, this chain runs steps under auth profile %q, whose login body reads environment "+
				"variables that are not exported in this shell: %s — shrt run refuses the chain before sending "+
				"anything until they are set, since the login would fail after earlier steps had run",
			firstStep[profile], profile, strings.Join(unset, ", "))})
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
					"fail is the one fault no gate downstream can see — a green step proves nothing. %s",
				e.Path, why, TautologyRemedy(e))})
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

	KindExportOverwritten = "export-overwritten"
	KindInexactPath       = "inexact-path"
	KindRefSyntax         = "reference-syntax"

	KindInertAllowFail = "inert-allow-fail"
)

func IsAssertionQualityIssue(i Issue) bool {
	switch i.Kind {
	case KindUnfailable, KindAssertsNone, KindUnreachable, KindDeadRef, KindBadExport, KindInertAllowFail,
		KindExportOverwritten:
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
