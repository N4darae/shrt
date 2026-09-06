package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/transport"
)

type Runner struct {
	Catalog        *catalog.Catalog
	Client         *transport.Client
	Now            func() time.Time
	ValidateInput  bool
	ValidateOutput bool
	OnStep         func(*StepRecord)
	Auth           AuthBindings
	BuildHeader    string
}

type AuthBinding struct {
	Profile     string
	Procedure   string
	TokenPath   string
	ExpiresPath string
	Body        func() ([]byte, error)
	Sink        transport.TokenSink
}

type AuthBindings []*AuthBinding

func (a *AuthBinding) observe(procedure string, response any) bool {
	if a == nil || a.Sink == nil || a.Procedure != procedure {
		return false
	}
	token, ok := chain.Get(response, a.TokenPath)
	text, isText := token.(string)
	if !ok || !isText || text == "" {
		return false
	}
	a.Sink.Seed(text, expiryOf(response, a.ExpiresPath))
	return true
}

func (a *AuthBinding) sentOwnCredentials(sent []byte, canonical func([]byte) ([]byte, error)) bool {
	if a.Body == nil {
		return false
	}
	own, err := a.Body()
	if err != nil {
		return false
	}
	return sameJSON(canonical, own, sent)
}

func sameJSON(canonical func([]byte) ([]byte, error), a, b []byte) bool {
	decode := func(raw []byte) (any, bool) {
		if canonical != nil {
			if c, err := canonical(raw); err == nil {
				raw = c
			}
		}
		var v any
		if len(bytes.TrimSpace(raw)) == 0 {
			raw = []byte("{}")
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, false
		}
		return v, true
	}
	left, ok := decode(a)
	if !ok {
		return false
	}
	right, ok := decode(b)
	if !ok {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func (bs AuthBindings) matching(procedure string) AuthBindings {
	out := make(AuthBindings, 0, len(bs))
	for _, b := range bs {
		if b != nil && b.Sink != nil && b.Procedure == procedure {
			out = append(out, b)
		}
	}
	return out
}

type seeding struct {
	seeded  []string
	refused []string
}

func (bs AuthBindings) observe(profile, procedure string, sent []byte, canonical func([]byte) ([]byte, error), response any) seeding {
	var out seeding
	for _, b := range bs.matching(procedure) {
		if profile != "" && b.Profile != profile {
			continue
		}
		if !b.sentOwnCredentials(sent, canonical) {
			out.refused = append(out.refused, b.Profile)
			continue
		}
		if b.observe(procedure, response) {
			out.seeded = append(out.seeded, b.Profile)
		}
	}
	return out
}

func (bs AuthBindings) profiles() []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if b != nil {
			out = append(out, b.Profile)
		}
	}
	return out
}

func expiryOf(response any, path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	v, ok := chain.Get(response, path)
	if !ok {
		return time.Time{}
	}
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return time.Unix(int64(t), 0)
		}
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0)
		}
	}
	return time.Time{}
}

type Options struct {
	Vars      map[string]any
	Volatile  []string
	Redact    []string
	DryRun    bool
	KeepGoing bool
	Build     string
}

type buildTracker struct {
	header string
	label  string
	last   string
	warned bool
}

func (b *buildTracker) observe(rec *Record, sr *StepRecord) {
	v := strings.TrimSpace(sr.serverBuild)
	if v == "" {
		return
	}
	defer func() { b.last = v }()
	if b.label != "" {
		if v != b.label && !b.warned {
			b.warned = true
			sr.Warning = joinLines(sr.Warning, fmt.Sprintf("the server reports build %q in %s, but -build "+
				"recorded %q: the label names a build this target was not running", v, b.header, b.label))
		}
		return
	}
	switch {
	case rec.Build == "":
		rec.Build = v
	case v != b.last:
		rec.Build += " -> " + v
		sr.Warning = joinLines(sr.Warning, fmt.Sprintf("the server's %s changed from %q to %q during this "+
			"run: a deploy landed mid-chain, so steps before and after this one ran against different builds",
			b.header, b.last, v))
	}
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n       " + b
}

func failureOf(step *chain.Step, sr *StepRecord) string {
	failure := fmt.Sprintf("step %q: %s", sr.ID, firstNonEmpty(sr.Error, "expectation failed"))
	if step.AllowFail && sr.Status == StatusError {
		failure += "\nallow_fail does not cover this: the call never reached the backend, " +
			"so there is no refusal to tolerate — this is a fixture defect, not a verdict"
	}
	if step.AllowFail && sr.Drift {
		failure += "\nallow_fail tolerates a refusal, not a response the descriptor cannot " +
			"read: a step that expects to be refused still has to be told what the refusal looks " +
			"like, and that cannot be checked here"
	}
	if step.AllowFail && sr.AssertionFailed() {
		failure += "\nallow_fail tolerates the call being refused, not an expectation you wrote being " +
			"wrong: the step said what the refusal would look like and it did not look like that"
	}
	return failure
}

func readsBroken(step *chain.Step, broken map[string]bool, exporter map[string]string) (string, string, bool) {
	for _, ref := range step.References() {
		if producer := producerOf(ref, exporter); producer != "" && broken[producer] {
			return ref, producer, true
		}
	}
	return "", "", false
}

func producerOf(ref string, exporter map[string]string) string {
	head, rest, hasRest := strings.Cut(strings.TrimSpace(ref), ".")
	next, _, _ := strings.Cut(rest, ".")
	switch head {
	case "vars", "env", "uuid", "now", "nowunix", "today":
		return ""
	case "steps":
		return next
	case "exports":
		return exporter[next]
	}
	if !hasRest {
		return exporter[head]
	}
	return head
}

func (r *Runner) skippedBehind(i int, step *chain.Step, ref, producer string) *StepRecord {
	sr := &StepRecord{Index: i + 1, ID: step.ID, Call: step.Call, Status: StatusSkipped, Volatile: step.Volatile}
	if method, err := r.Catalog.Lookup(step.Call); err == nil {
		sr.Procedure = method.Procedure()
	}
	sr.Error = fmt.Sprintf("not sent: ${%s} reads step %q, which did not pass. -keep-going never sends a "+
		"request built from a failed step's response, because a refused call's response decodes to zero "+
		"values and the request would carry them as if they were real", ref, producer)
	return sr
}

func (r *Runner) Run(ctx context.Context, c *chain.Chain, opts Options) (*Record, error) {
	now := r.clock()
	rec := &Record{
		RunID:       newRunID(now),
		Chain:       c.Name,
		ChainSource: c.SourcePath,
		Target:      r.Client.BaseURL(),
		StartedAt:   now,
		Status:      StatusPassed,
		DryRun:      opts.DryRun,
		KeepGoing:   opts.KeepGoing,
		Build:       strings.TrimSpace(opts.Build),
		Vars:        mergeVars(c.Vars, opts.Vars),
		Volatile:    append(append([]string{}, c.Volatile...), opts.Volatile...),
		Redacted:    append(append([]string{}, c.Redact...), opts.Redact...),
		Steps:       make([]*StepRecord, 0, len(c.Steps)),
	}
	if err := r.checkAuthProfiles(c); err != nil {
		return nil, err
	}
	redactor := pathmask.NewRedactor(rec.Redacted)
	scope := chain.NewScope(rec.Vars)
	if r.Now != nil {
		scope.Now = r.Now
	}

	builds := &buildTracker{header: r.BuildHeader, label: rec.Build}
	broken := map[string]bool{}
	exporter := map[string]string{}
	failures := []string{}
	for i, step := range c.Steps {
		var sr *StepRecord
		behind := false
		if opts.KeepGoing {
			if ref, producer, ok := readsBroken(step, broken, exporter); ok {
				sr = r.skippedBehind(i, step, ref, producer)
				behind = true
			}
		}
		if sr == nil {
			sr = r.runStep(ctx, scope, i, step, opts, redactor)
		}
		for name := range step.Export {
			exporter[name] = step.ID
		}
		builds.observe(rec, sr)
		rec.Steps = append(rec.Steps, sr)
		if r.OnStep != nil {
			r.OnStep(sr)
		}
		if sr.Status == StatusPassed || (sr.Status == StatusSkipped && !behind) {
			continue
		}
		if step.AllowFail && sr.Status == StatusFailed && !sr.AssertionFailed() && !sr.Drift {
			continue
		}
		failure := failureOf(step, sr)
		if !opts.KeepGoing {
			rec.Status = sr.Status
			rec.Failure = failure
			break
		}
		if rec.Status == StatusPassed {
			rec.Status = sr.Status
			if behind {
				rec.Status = StatusFailed
			}
		}
		broken[step.ID] = true
		rec.FailedSteps = append(rec.FailedSteps, step.ID)
		failures = append(failures, failure)
	}
	if len(failures) > 0 {
		rec.Failure = fmt.Sprintf("-keep-going: %d of %d steps did not pass\n", len(failures), len(c.Steps)) +
			strings.Join(failures, "\n")
	}

	rec.Exports = maskExports(scope.Exports, c.Steps, redactor)
	if masked, ok := redactor.Apply(rec.Vars).(map[string]any); ok {
		rec.Vars = masked
	}
	rec.DurationMS = r.clock().Sub(now).Milliseconds()
	return rec, nil
}

func (r *Runner) runStep(ctx context.Context, scope *chain.Scope, i int, step *chain.Step, opts Options, redactor *pathmask.Masker) *StepRecord {
	sr := &StepRecord{Index: i + 1, ID: step.ID, Call: step.Call, Status: StatusPassed, Volatile: step.Volatile}

	method, err := r.Catalog.Lookup(step.Call)
	if err != nil {
		return fail(sr, err)
	}
	sr.Procedure = method.Procedure()

	resolved, err := scope.ResolveValue(orEmpty(step.Body))
	if err != nil {
		return fail(sr, err)
	}
	body, err := json.Marshal(resolved)
	if err != nil {
		return fail(sr, fmt.Errorf("encode request: %w", err))
	}
	sr.Request = mustJSON(redactor.Apply(resolved), body)

	resolvedHeaders, err := resolveHeaders(scope, step.Headers)
	if err != nil {
		return fail(sr, err)
	}

	if r.ValidateInput {
		if err := r.Catalog.ValidateInput(method, body); err != nil {
			return fail(sr, err)
		}
	}
	if opts.DryRun {
		sr.Status = StatusSkipped
		sample := catalog.Scaffold(method.Output())
		scope.RecordSynthetic(step.ID, resolved, sample)
		for name, path := range step.Export {
			if v, ok := chain.Get(sample, path); ok {
				scope.Exports[name] = v
			}
		}
		for _, e := range step.Expect {
			if _, err := e.ResolveWith(scope); err != nil {
				sr.Status = StatusFailed
				sr.Expect = append(sr.Expect, chain.ExpectResult{
					Path: e.Path, Rule: "unresolved", Passed: false, Detail: err.Error()})
			}
		}
		return sr
	}

	call := &transport.Call{Procedure: method.Procedure(), Body: body, Header: resolvedHeaders}
	if step.SkipAuth || step.Auth != "" {
		call.Meta = map[string]any{"skip_auth": step.SkipAuth, "auth": step.Auth}
	}
	res, err := r.Client.Do(ctx, call)
	if profile, routed := transport.CallAuthProfile(call); routed {
		sr.AuthProfile = firstNonEmpty(profile, NoAuthProfile)
	}
	if err != nil {
		return fail(sr, err)
	}
	sr.HTTPStatus = res.Status
	sr.LatencyMS = res.Latency.Milliseconds()
	if r.BuildHeader != "" && res.Header != nil {
		sr.serverBuild = res.Header.Get(r.BuildHeader)
	}

	outcome := transportOutcome(res)
	if res.Error != nil {
		sr.Response = jsonBodyOrString(res.Body)
		sr.Transport = &TransportError{Code: res.Error.Code, Message: res.Error.Message}
		sr.Expect = evaluateRefused(scope, step.Expect, outcome, redactor)
		if refusalAsserted(step.Expect, sr.Expect) {
			sr.Note = "refused as this step asserted: " + res.Error.Error()
			if len(step.Export) > 0 {
				sr.Status = StatusFailed
				sr.Error = "the call was refused as asserted, so there is no response to export from"
			}
			return sr
		}
		sr.Status = StatusFailed
		sr.Error = res.Error.Error()
		if res.Error.Code == "unauthenticated" && len(r.Auth) == 0 {
			sr.Error += "\n       this Runner carries no auth bindings, so no token was ever attached. Either " +
				".shrt/config.yaml declares no auth: block (shrt init writes none — it cannot guess your login " +
				"rpc; see GRAMMAR.md §4), or it does and this Runner was built without Auth: deps.Bindings."
		}
		return sr
	}

	canonical, populated, cerr := r.Catalog.CanonicalizeWithPresence(method.Output(), res.Body)
	staleOutput := false
	switch {
	case cerr == nil:
	case r.ValidateOutput:
		sr.Response = jsonBodyOrString(res.Body)
		sr.Status = StatusFailed
		sr.Drift = true
		sr.Expect = unevaluated(step.Expect)
		sr.Error = "conventions.validate_output is on and this response does not match " +
			string(method.Output().FullName()) + ": " + cerr.Error() +
			"\n       The request WAS sent and the backend answered " + fmt.Sprint(res.Status) +
			". Nothing here is evidence about the rpc: the expectations were not evaluated, because " +
			"the body they would read could not be decoded. Rebuild the descriptor ('shrt catalog " +
			"build') and re-run before reading it as a backend defect."
		return sr
	default:
		canonical = res.Body
		populated = res.Body
		staleOutput = true
		sr.Warning = "response kept as sent, it does not match " + string(method.Output().FullName()) +
			" (the descriptor may be stale): " + cerr.Error()
	}

	var decoded any
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return fail(sr, fmt.Errorf("decode response: %w", err))
	}
	var sent any
	if err := json.Unmarshal(populated, &sent); err != nil {
		return fail(sr, fmt.Errorf("decode response: %w", err))
	}
	sr.Response = mustJSON(redactor.Apply(decoded), canonical)
	scope.Record(step.ID, resolved, decoded)
	canonicalInput := func(raw []byte) ([]byte, error) { return r.Catalog.Canonicalize(method.Input(), raw) }
	sr.Note = seedingNote(r.Auth.observe(step.Auth, method.Procedure(), body, canonicalInput, decoded))

	for _, e := range step.Expect {
		response, presence := any(decoded), sent
		if chain.IsTransportPath(e.Path) {
			response, presence = outcome, outcome
		}
		result := evaluate(scope, e, response, presence, redactor)
		sr.Expect = append(sr.Expect, result)
		if !result.Passed {
			sr.Status = StatusFailed
		}
	}

	if chain.ItemEnvelope() != "" && (staleOutput || chain.ItemEnvelopeDeclared(catalog.DescribeMessage(method.Output()).Fields)) {
		refusals, itemErr := chain.ItemRefusals(decoded)
		switch {
		case itemErr == nil:
		case staleOutput:
			refusals = nil
			sr.Warning += "\n       the per-item verdict could not be checked against this response: " + itemErr.Error()
		default:
			return fail(sr, itemErr)
		}
		if surprises := chain.UndeclaredRefusals(refusals, step.Expect); len(surprises) > 0 {
			got := make([]string, 0, len(surprises))
			for _, r := range surprises {
				got = append(got, r.String())
			}
			sr.Expect = append(sr.Expect, chain.ExpectResult{
				Path:   chain.ItemEnvelope(),
				Rule:   "item_envelope",
				Want:   chain.EnvelopeOK(),
				Got:    strings.Join(got, ", "),
				Passed: false,
				Detail: "every item in a batch response carries its own verdict, and these were refused " +
					"while the top-level envelope said OK — a step that asserts only the envelope would " +
					"pass having achieved nothing. A line this step MEANS to be refused is declared by " +
					"asserting that line's verdict path (equals, not_equal or contains), and is then not " +
					"reported here",
			})
			sr.Status = StatusFailed
		}
	}

	if len(step.Export) > 0 {
		sr.Exported = map[string]any{}
		for name, path := range step.Export {
			v, ok := chain.Get(decoded, path)
			if !ok {
				sr.Status = StatusFailed
				sr.Error = fmt.Sprintf("export %q: path %q missing in response", name, path)
				continue
			}
			scope.Exports[name] = v
			if redactor.Masks(path) {
				sr.Exported[name] = pathmask.MaskRedacted
				continue
			}
			sr.Exported[name] = v
		}
	}
	return sr
}

func evaluate(scope *chain.Scope, e chain.Expectation, response, presence any, redactor *pathmask.Masker) chain.ExpectResult {
	bound, err := e.ResolveWith(scope)
	if err != nil {
		return chain.ExpectResult{Path: e.Path, Rule: "unresolved", Passed: false, Detail: err.Error()}
	}
	result := bound.EvaluateIn(response, presence)
	if redactor.MasksValue(e.Path, result.Got) || redactor.MasksValue(e.Path, result.Want) {
		result.Got = pathmask.MaskRedacted
		result.Want = pathmask.MaskRedacted
	}
	return result
}

func transportOutcome(res *transport.Result) map[string]any {
	if res.Error == nil {
		return chain.TransportOutcome(res.Status, "", "")
	}
	return chain.TransportOutcome(res.Status, res.Error.Code, res.Error.Message)
}

func evaluateRefused(scope *chain.Scope, expect []chain.Expectation, outcome map[string]any, redactor *pathmask.Masker) []chain.ExpectResult {
	out := unevaluated(expect)
	for i, e := range expect {
		if chain.IsTransportPath(e.Path) {
			out[i] = evaluate(scope, e, outcome, outcome, redactor)
		}
	}
	return out
}

func refusalAsserted(expect []chain.Expectation, results []chain.ExpectResult) bool {
	if !chain.HasTransportExpectation(expect) {
		return false
	}
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

func maskExports(exports map[string]any, steps []*chain.Step, redactor *pathmask.Masker) map[string]any {
	if len(exports) == 0 {
		return exports
	}
	out := make(map[string]any, len(exports))
	for name, v := range exports {
		out[name] = v
	}
	for _, s := range steps {
		for name, path := range s.Export {
			if _, exported := out[name]; exported && redactor.Masks(path) {
				out[name] = pathmask.MaskRedacted
			}
		}
	}
	return out
}

func (r *Runner) checkAuthProfiles(c *chain.Chain) error {
	known := map[string]bool{}
	for _, name := range r.Auth.profiles() {
		known[name] = true
	}
	for _, step := range c.Steps {
		if step.Auth == transport.InvalidTokenProfile && len(known) == 0 {
			return fmt.Errorf("step %q asks for auth: %s, but the config declares no auth at all, so there is "+
				"no header to carry a token in and the probe would silently send none. Use skip_auth: true "+
				"for a no-token probe", step.ID, transport.InvalidTokenProfile)
		}
		if step.Auth == "" || known[step.Auth] || step.Auth == transport.InvalidTokenProfile {
			continue
		}
		have := r.Auth.profiles()
		if len(have) == 0 {
			return fmt.Errorf("step %q asks for auth profile %q, but the config declares no auth at all", step.ID, step.Auth)
		}
		sort.Strings(have)
		return fmt.Errorf("step %q asks for auth profile %q, which the config does not define (have: %s)",
			step.ID, step.Auth, strings.Join(have, ", "))
	}
	return nil
}

func seedingNote(s seeding) string {
	notes := make([]string, 0, len(s.seeded)+1)
	for _, profile := range s.seeded {
		notes = append(notes, seededNote(profile))
	}
	if len(s.refused) > 0 {
		names := make([]string, 0, len(s.refused))
		for _, p := range s.refused {
			names = append(names, profileLabel(p))
		}
		notes = append(notes, "did not seed the "+strings.Join(names, ", ")+" auth token: this login sent "+
			"other credentials than that profile's body in .shrt/config.yaml, so its token belongs to a "+
			"different principal. Steps under that profile keep logging in as the configured one — to act "+
			"as this principal, declare a profile with these credentials and name it with auth:")
	}
	return strings.Join(notes, "\n")
}

func profileLabel(profile string) string {
	if profile == transport.DefaultProfile {
		return "shared (default)"
	}
	return profile
}

func seededNote(profile string) string {
	if profile == transport.DefaultProfile {
		return "seeded the shared auth token, later steps reuse it instead of logging in again"
	}
	return "seeded the " + profile + " auth token, later steps with auth: " + profile + " reuse it instead of logging in again"
}

func fail(sr *StepRecord, err error) *StepRecord {
	sr.Status = StatusError
	sr.Error = err.Error()
	return sr
}

func (r *Runner) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func resolveHeaders(scope *chain.Scope, in map[string]string) (map[string][]string, error) {
	out := map[string][]string{}
	for k, v := range in {
		resolved, err := scope.ResolveValue(v)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", k, err)
		}
		out[k] = []string{fmt.Sprintf("%v", resolved)}
	}
	return out, nil
}

func jsonBodyOrString(body []byte) json.RawMessage {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	quoted, err := json.Marshal(string(body))
	if err != nil {
		return json.RawMessage(`"<body is neither JSON nor valid UTF-8>"`)
	}
	return json.RawMessage(quoted)
}

func mustJSON(v any, fallback []byte) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fallback
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes()))
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func mergeVars(base, override map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func newRunID(t time.Time) string {
	return t.UTC().Format("20060102T150405Z") + "-" + randSuffix()
}

func unevaluated(expect []chain.Expectation) []chain.ExpectResult {
	if len(expect) == 0 {
		return nil
	}
	out := make([]chain.ExpectResult, 0, len(expect))
	for _, e := range expect {
		out = append(out, chain.ExpectResult{
			Path:   e.Path,
			Rule:   "unevaluated",
			Passed: false,
			Detail: "the call was refused before a response body existed, so this assertion never ran. " +
				"allow_fail tolerates a refusal, not an assertion going unchecked. A refusal is asserted " +
				"with transport.code or transport.http_status",
		})
	}
	return out
}
