package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
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

func (a *AuthBinding) carriesToken(response any) bool {
	token, ok := chain.Get(response, a.TokenPath)
	text, isText := token.(string)
	return ok && isText && text != ""
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
	seeded    []string
	refused   []string
	tokenless []string
}

func (bs AuthBindings) observe(profile, procedure string, sent []byte, canonical func([]byte) ([]byte, error), response any) seeding {
	var out seeding
	for _, b := range bs.matching(procedure) {
		if profile != "" && b.Profile != profile {
			continue
		}
		if !b.sentOwnCredentials(sent, canonical) {
			if b.carriesToken(response) {
				out.refused = append(out.refused, b.Profile)
			} else {
				out.tokenless = append(out.tokenless, b.Profile)
			}
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
	heldBack  map[int]string
	chain     *chain.Chain
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
	failure := fmt.Sprintf("step %q: %s", sr.ID, firstNonEmpty(sr.Error, firstFailedExpectation(sr)))
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

type exportSource struct {
	step string
	path string
}

type exportSources map[string]exportSource

func (x exportSources) steps() map[string]string {
	out := make(map[string]string, len(x))
	for name, src := range x {
		out[name] = src.step
	}
	return out
}

func readsBroken(step *chain.Step, broken map[string]*StepRecord, exporter exportSources) (string, *StepRecord, bool) {
	return readsBrokenIn(step.SendReferences(), broken, exporter)
}

func heldBackExpectations(step *chain.Step, broken map[string]*StepRecord, exporter exportSources) map[int]string {
	held := map[int]string{}
	for i, e := range step.Expect {
		if ref, producer, ok := readsBrokenIn(e.References(), broken, exporter); ok {
			held[i] = fmt.Sprintf("not evaluated: ${%s} reads step %q, which did not pass, so its value is not evidence (-keep-going)", ref, producer.ID)
		}
	}
	return held
}

func readsBrokenIn(refs []string, broken map[string]*StepRecord, exporter exportSources) (string, *StepRecord, bool) {
	steps := exporter.steps()
	for _, ref := range refs {
		producer := producerOf(ref, steps)
		sr, isBroken := broken[producer]
		if producer == "" || !isBroken {
			continue
		}
		if readsRequest(ref) && sr.Request != nil {
			continue
		}
		if readsSoundField(exportedPath(ref, exporter), sr) {
			continue
		}
		return ref, sr, true
	}
	return "", nil, false
}

func readsSoundField(ref string, sr *StepRecord) bool {
	if sr.Status != StatusFailed || sr.Transport != nil || sr.Drift {
		return false
	}
	if _, refused := inBandRefusal(sr.Response); refused {
		return false
	}
	path, ok := responsePathOf(ref)
	if !ok {
		return false
	}
	failed := failedPaths(sr.Expect)
	if len(failed) == 0 {
		return false
	}
	for _, f := range failed {
		if f == "" || strings.Contains(f, "[]") || f == path || strings.HasPrefix(path, f+".") || strings.HasPrefix(f, path+".") {
			return false
		}
	}
	return true
}

func exportedPath(ref string, exporter exportSources) string {
	r := chain.ParseRef(ref)
	name := ""
	switch r.Kind {
	case chain.RefExports:
		name = r.Rest
	case chain.RefBare:
		name = r.Head
	default:
		return ref
	}
	name, tail, _ := strings.Cut(name, ".")
	src, ok := exporter[name]
	if !ok || src.path == "" {
		return ref
	}
	if tail != "" {
		return src.step + "." + src.path + "." + tail
	}
	return src.step + "." + src.path
}

func responsePathOf(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if rest, ok := strings.CutPrefix(ref, "steps."); ok {
		_, rest, _ = strings.Cut(rest, ".")
		path, ok := strings.CutPrefix(rest, "response.")
		return path, ok && path != ""
	}
	r := chain.ParseRef(ref)
	if r.Kind != chain.RefStep || r.Rest == "" {
		return "", false
	}
	return r.Rest, true
}

func readsRequest(ref string) bool {
	r := chain.ParseRef(ref)
	if r.Kind != chain.RefStep {
		return false
	}
	section, _, _ := strings.Cut(r.Rest, ".")
	return section == "request"
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

func (r *Runner) skippedBehind(i int, step *chain.Step, ref string, producer *StepRecord) *StepRecord {
	sr := &StepRecord{Index: i + 1, ID: step.ID, Call: step.Call, Status: StatusSkipped, Volatile: step.Volatile}
	if method, err := r.Catalog.Lookup(step.Call); err == nil {
		sr.Procedure = method.Procedure()
	}
	sr.Error = fmt.Sprintf("not sent: ${%s} reads step %q, which %s", ref, producer.ID, whyNotReadable(producer))
	if producer.Request != nil {
		sr.Error += fmt.Sprintf(" A reference to its request (${steps.%s.request...}) is still safe: that "+
			"is what was sent, so a step reading only that is sent", producer.ID)
	}
	return sr
}

func (r *Runner) skippedUnreachable(i int, step *chain.Step, dead *StepRecord) *StepRecord {
	sr := &StepRecord{Index: i + 1, ID: step.ID, Call: step.Call, Status: StatusSkipped, Volatile: step.Volatile, unreachable: dead.unreachable}
	if method, err := r.Catalog.Lookup(step.Call); err == nil {
		sr.Procedure = method.Procedure()
	}
	sr.Error = "not sent: " + unreachableReason(r.Client.BaseURL(), dead)
	return sr
}

func unreachableReason(target string, dead *StepRecord) string {
	return fmt.Sprintf("the target %s is unreachable (%s at step %q), so -keep-going stopped sending",
		target, dead.unreachable, dead.ID)
}

func innermost(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err.Error()
		}
		err = next
	}
}

func whyNotReadable(sr *StepRecord) string {
	switch {
	case sr.Status == StatusSkipped:
		return "was itself not sent, so it has no response to read."
	case sr.Transport != nil:
		return fmt.Sprintf("was refused before a response body existed (transport %s), so there is no "+
			"response to read.", sr.Transport.Code)
	case sr.Status == StatusError:
		return "did not complete (" + firstLine(sr.Error) + "), so there is no response to read."
	case sr.Drift:
		return "was answered with a body the descriptor could not decode, so no value read from it can be trusted."
	}
	if code, refused := inBandRefusal(sr.Response); refused {
		return fmt.Sprintf("was refused in-band (%s = %s): a refused call's response decodes to zero values, "+
			"and the request would carry them as if they were real.", chain.EnvelopePath(), code)
	}
	if failed := failedPaths(sr.Expect); len(failed) > 0 {
		return fmt.Sprintf("was answered but failed its assertion on %s: -keep-going does not send a request "+
			"built from a response the chain has already said is wrong.", strings.Join(failed, ", "))
	}
	return "did not pass (" + firstLine(firstNonEmpty(sr.Error, sr.Status)) + "), so -keep-going does not " +
		"send a request built from its response."
}

func envelopeOKNeverSeen(steps []*StepRecord) string {
	path, ok := chain.EnvelopePath(), chain.EnvelopeOK()
	if path == "" {
		return ""
	}
	counts := map[string]int{}
	for _, sr := range steps {
		if sr.Transport != nil || sr.Drift || len(sr.Response) == 0 {
			continue
		}
		var v any
		if err := json.Unmarshal(sr.Response, &v); err != nil {
			continue
		}
		code, found := chain.Get(v, path)
		if !found || code == nil {
			continue
		}
		text := fmt.Sprint(code)
		if text == ok {
			return ""
		}
		if !assertedValue(sr.Expect, path) && !assertsTransport(sr.Expect) {
			counts[text]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	seen := make([]string, 0, len(counts))
	verdicts, refusals := true, true
	for text, n := range counts {
		shown := text
		if !looksLikeVerdict(text) {
			shown = strconv.Quote(text)
			verdicts = false
		}
		if !looksLikeRefusal(text) {
			refusals = false
		}
		seen = append(seen, fmt.Sprintf("%s (%d)", shown, n))
	}
	sort.Strings(seen)
	if !verdicts {
		return fmt.Sprintf("no response in this run carried %s = %s, and the values found at %s were %s, which "+
			"do not look like verdict codes. conventions.envelope_path in .shrt/config.yaml most likely names "+
			"the wrong field: point it at the field that carries the verdict", path, ok, path, strings.Join(seen, ", "))
	}
	if refusals {
		return ""
	}
	return fmt.Sprintf("no response in this run carried %s = %s, the configured success value; the values seen "+
		"were %s. If one of those is how this backend spells success, conventions.envelope_ok in "+
		".shrt/config.yaml is wrong, and every success is being read as a refusal", path, ok, strings.Join(seen, ", "))
}

func assertsTransport(results []chain.ExpectResult) bool {
	for _, e := range results {
		if chain.IsTransportPath(e.Path) {
			return true
		}
	}
	return false
}

var (
	verdictShape = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,40}$`)
	refusalWords = []string{"REJECT", "ERROR", "FAIL", "DENIED", "FORBIDDEN", "INVALID", "UNAUTH", "NOT_FOUND",
		"NOTFOUND", "REFUSE", "DECLINE", "CONFLICT", "ABORT", "EXPIRED"}
)

func looksLikeVerdict(text string) bool {
	return verdictShape.MatchString(text)
}

func looksLikeRefusal(text string) bool {
	upper := strings.ToUpper(text)
	for _, w := range refusalWords {
		if strings.Contains(upper, w) {
			return true
		}
	}
	return false
}

func assertedValue(results []chain.ExpectResult, path string) bool {
	for _, e := range results {
		if e.Passed && e.Path == path && e.Rule == "equals" {
			return true
		}
	}
	return false
}

func inBandRefusal(response json.RawMessage) (string, bool) {
	if len(response) == 0 {
		return "", false
	}
	var v any
	if err := json.Unmarshal(response, &v); err != nil {
		return "", false
	}
	code, ok := chain.Get(v, chain.EnvelopePath())
	if !ok || code == nil {
		return "", false
	}
	text := fmt.Sprint(code)
	return text, text != "" && text != chain.EnvelopeOK()
}

func failedPaths(results []chain.ExpectResult) []string {
	out := []string{}
	for _, e := range results {
		if !e.Passed {
			out = append(out, e.Path)
		}
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
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
	if err := checkVarsSupplied(c, opts.Vars); err != nil {
		return nil, err
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
	broken := map[string]*StepRecord{}
	exporter := exportSources{}
	failures := []string{}
	failedCount := 0
	said := map[string]int{}
	repeats := map[int][]string{}
	var dead *StepRecord
	unreached := 0
	for i, step := range c.Steps {
		var sr *StepRecord
		behind, behindOn := false, ""
		if dead != nil {
			sr = r.skippedUnreachable(i, step, dead)
			rec.Steps = append(rec.Steps, sr)
			rec.FailedSteps = append(rec.FailedSteps, step.ID)
			unreached++
			if r.OnStep != nil {
				r.OnStep(sr)
			}
			continue
		}
		if opts.KeepGoing {
			if ref, producer, ok := readsBroken(step, broken, exporter); ok {
				sr = r.skippedBehind(i, step, ref, producer)
				behind, behindOn = true, producer.ID
			}
		}
		if sr == nil {
			stepOpts := opts
			stepOpts.chain = c
			if opts.KeepGoing {
				stepOpts.heldBack = heldBackExpectations(step, broken, exporter)
			}
			sr = r.runStep(ctx, scope, i, step, stepOpts, redactor)
		}
		for name, path := range step.Export {
			exporter[name] = exportSource{step: step.ID, path: path}
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
		broken[step.ID] = sr
		rec.FailedSteps = append(rec.FailedSteps, step.ID)
		key := strings.TrimPrefix(failure, fmt.Sprintf("step %q: ", sr.ID))
		if behind {
			key = "behind " + behindOn
		}
		failedCount++
		if at, ok := said[key]; ok {
			repeats[at] = append(repeats[at], sr.ID)
		} else {
			said[key] = len(failures)
			failures = append(failures, failure)
		}
		if sr.unreachable != "" {
			dead = sr
		}
	}
	for at, ids := range repeats {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, strconv.Quote(id))
		}
		failures[at] += fmt.Sprintf("\nthe same for %d more step(s): %s", len(ids), capIDs(quoted, 10))
	}
	if len(failures) > 0 {
		rec.Failure = fmt.Sprintf("-keep-going: %d of %d steps did not pass\n", failedCount+unreached, len(c.Steps)) +
			strings.Join(failures, "\n")
	}
	if unreached > 0 {
		rec.Failure += fmt.Sprintf("\n%d later step(s) not sent: %s", unreached, unreachableReason(r.Client.BaseURL(), dead))
	}

	if !opts.DryRun {
		rec.Warning = envelopeOKNeverSeen(rec.Steps)
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
	if method.Streaming() {
		return fail(sr, errors.New(method.StreamRefusal()))
	}

	resolved, err := scope.ResolveValue(orEmpty(step.Body))
	if err != nil {
		return fail(sr, chain.ExplainLaterRef(opts.chain, i, err))
	}
	scope.RecordRequest(step.ID, resolved)
	body, err := json.Marshal(resolved)
	if err != nil {
		return fail(sr, fmt.Errorf("encode request: %w", err))
	}
	sr.Request = mustJSON(redactor.Apply(resolved), body)

	resolvedHeaders, err := resolveHeaders(scope, step.Headers)
	if err != nil {
		return fail(sr, chain.ExplainLaterRef(opts.chain, i, err))
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
		if transport.Unreachable(err) {
			sr.unreachable = innermost(err)
		}
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
		for i, why := range opts.heldBack {
			if i < len(sr.Expect) {
				sr.Expect[i] = chain.ExpectResult{Path: step.Expect[i].Path, Rule: "unevaluated", Passed: false, Detail: why}
			}
		}
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
				".shrt/config.yaml declares no auth: block (shrt init writes one only when the descriptor has an " +
				"rpc that looks like a login; see GRAMMAR.md §4), or it does and this Runner was built without Auth: deps.Bindings."
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
		sr.Expect = unevaluated(step.Expect, redactor)
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

	for i, e := range step.Expect {
		response, presence := any(decoded), sent
		if chain.IsTransportPath(e.Path) {
			response, presence = outcome, outcome
		}
		result := evaluate(scope, e, response, presence, redactor)
		if !result.Passed && result.Detail == "" && e.Path == chain.EnvelopePath() {
			result.Detail = refusalContext(decoded, e.Path, redactor)
		}
		if !result.Passed && result.Detail == "" && chain.IsTransportPath(e.Path) {
			result.Detail = envelopeBehindTransport(decoded, outcome, redactor)
		}
		if why, held := opts.heldBack[i]; held {
			result = chain.ExpectResult{Path: e.Path, Rule: "unevaluated", Passed: false, Detail: why}
		}
		sr.Expect = append(sr.Expect, result)
		if !result.Passed {
			sr.Status = StatusFailed
		}
	}

	if len(step.Expect) == 0 {
		if warning := unassertedRefusalWarning(decoded); warning != "" {
			sr.Warning = joinLines(sr.Warning, warning)
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
				line := r.String()
				if why := refusalContext(decoded, r.Path, redactor); why != "" {
					line += " (" + why + ")"
				}
				got = append(got, line)
			}
			sr.Expect = append(sr.Expect, chain.ExpectResult{
				Path:   chain.ItemEnvelope(),
				Rule:   "item_envelope",
				Want:   chain.EnvelopeOK(),
				Got:    strings.Join(got, ", "),
				Passed: false,
				Detail: itemEnvelopeDetail(decoded, surprises),
			})
			sr.Status = StatusFailed
		}
	}

	if len(step.Export) > 0 {
		sr.Exported = map[string]any{}
		for name, path := range step.Export {
			v, ok := chain.Get(decoded, path)
			if !ok {
				missing := fmt.Sprintf("export %q: path %q missing in response", name, path)
				if sr.AssertionFailed() {
					missing = failedExpectations(sr) + "; " + missing
				}
				sr.Status = StatusFailed
				sr.Error = missing
				continue
			}
			scope.Exports[name] = v
			if redactor.MasksValue(path, v) {
				sr.Exported[name] = pathmask.MaskRedacted
				continue
			}
			sr.Exported[name] = v
		}
	}
	return sr
}

func unassertedRefusalWarning(decoded any) string {
	path := chain.EnvelopePath()
	if path == "" {
		return ""
	}
	code, ok := chain.Get(decoded, path)
	if !ok || code == nil {
		return ""
	}
	text := fmt.Sprint(code)
	if text == "" || text == chain.EnvelopeOK() {
		return ""
	}
	return fmt.Sprintf("refused in-band (%s = %s, not %s), and this step declares no expect, so it is "+
		"recorded passed with nothing checked. If the refusal is the point, assert it (%s equals: %s); if "+
		"not, the backend rejected this call and later steps that read its response read zero values", path, text,
		chain.EnvelopeOK(), path, text)
}

func envelopeBehindTransport(decoded, outcome any, redactor *pathmask.Masker) string {
	path := chain.EnvelopePath()
	if path == "" {
		return ""
	}
	code, ok := chain.Get(decoded, path)
	if !ok || code == nil {
		return ""
	}
	said := fmt.Sprintf("the envelope said %s = %v", path, code)
	if context := refusalContext(decoded, path, redactor); context != "" {
		said += " " + context
	}
	if status, ok := chain.Get(outcome, "transport.http_status"); ok {
		return fmt.Sprintf("answered with HTTP %v, so the transport did not refuse it; %s", status, said)
	}
	return said
}

func refusalContext(decoded any, codePath string, redactor *pathmask.Masker) string {
	parent := ""
	if i := strings.LastIndex(codePath, "."); i >= 0 {
		parent = codePath[:i+1]
	}
	parts := []string{}
	seen := map[string]bool{}
	add := func(label, path string) {
		v, ok := chain.Get(decoded, path)
		if !ok || v == nil {
			return
		}
		text := fmt.Sprint(v)
		if text == "" || seen[label] {
			return
		}
		if redactor.MasksValue(path, v) {
			text = pathmask.MaskRedacted
		}
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		seen[label] = true
		if label == "message" {
			text = strconv.Quote(text)
		}
		parts = append(parts, label+"="+text)
	}
	add("message", parent+"message")
	for _, field := range []string{"reason", "app_code"} {
		add(field, parent+"details.0."+field)
		add(field, parent+field)
	}
	return strings.Join(parts, " ")
}

func itemEnvelopeDetail(decoded any, surprises []chain.ItemRefusal) string {
	said := "no top-level verdict at " + chain.EnvelopePath()
	if v, ok := chain.Get(decoded, chain.EnvelopePath()); ok && v != nil {
		said = fmt.Sprintf("the top-level envelope said %v", v)
		top := fmt.Sprint(v)
		same := len(surprises) > 0
		for _, r := range surprises {
			if r.Code != top {
				same = false
			}
		}
		if same {
			return fmt.Sprintf("these items carry %s, the very value the top-level envelope carries, and "+
				"conventions.envelope_ok is %q, so every one counts as refused. If %s is this backend's success "+
				"value, the items were not refused at all: set conventions.envelope_ok: %s in .shrt/config.yaml",
				top, chain.EnvelopeOK(), top, top)
		}
	}
	return "every item in a batch response carries its own verdict, and these were refused while " + said +
		" — a step that asserts only the envelope would pass having achieved nothing. A line this step " +
		"MEANS to be refused is declared by pinning that line's verdict path, or one of its code fields (" +
		strings.Join(chain.CodeFields(), ", ") + "), with equals, not_equal or contains, and is then not " +
		"reported here"
}

func evaluate(scope *chain.Scope, e chain.Expectation, response, presence any, redactor *pathmask.Masker) chain.ExpectResult {
	bound, err := e.ResolveWith(scope)
	if err != nil {
		return chain.ExpectResult{Path: e.Path, Rule: "unresolved", Passed: false, Detail: err.Error()}
	}
	result := bound.EvaluateIn(response, presence)
	if redactor.MasksValue(e.Path, result.Got) {
		result.Got = pathmask.MaskRedacted
	}
	if redactor.MasksValue(e.Path, result.Want) {
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
	out := unevaluated(expect, redactor)
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
			if _, exported := out[name]; exported && redactor.MasksValue(path, out[name]) {
				out[name] = pathmask.MaskRedacted
			}
		}
	}
	return out
}

func checkVarsSupplied(c *chain.Chain, supplied map[string]any) error {
	missing := c.MissingVars(supplied)
	if len(missing) == 0 {
		return nil
	}
	refs := make([]string, 0, len(missing))
	flags := make([]string, 0, len(missing))
	for _, name := range missing {
		refs = append(refs, "${vars."+name+"}")
		flags = append(flags, "-var "+name+"=...")
	}
	return fmt.Errorf("chain %q reads %s, which it does not declare under vars: and this run was not given, "+
		"so nothing was sent: the run would have died at the first step reading one, after every step before "+
		"it had already hit the backend. Supply %s, or declare a value under vars: in the chain",
		c.Name, strings.Join(refs, ", "), strings.Join(flags, " "))
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
	if len(s.seeded) > 0 {
		notes := make([]string, 0, len(s.seeded)+1)
		for _, profile := range s.seeded {
			notes = append(notes, seededNote(profile))
		}
		if others := append(append([]string{}, s.refused...), s.tokenless...); len(others) > 0 {
			if len(others) == 1 {
				notes = append(notes, "the "+profileNames(others)+" profile on the same login rpc was not seeded: "+
					"its configured body differs from what this login sent, so steps under it keep logging in with that body")
			} else {
				notes = append(notes, "the "+profileNames(others)+" profiles on the same login rpc were not seeded: "+
					"each one's configured body differs from what this login sent, so steps under each keep logging in "+
					"with that profile's own body")
			}
		}
		return strings.Join(notes, "\n")
	}
	all := append(append([]string{}, s.refused...), s.tokenless...)
	switch {
	case len(s.refused) > 0:
		return "did not seed any auth token: what this login sent differs from the configured body of every " +
			"profile it could seed (" + profileNames(all) + "), so the token it returned belongs to a principal " +
			"no profile describes and no later step uses it. Steps under each of those profiles keep logging in " +
			"with that profile's own body. To run steps as this principal, add a profile with this body under " +
			"auth.profiles in .shrt/config.yaml and put auth: <that profile's name> on those steps"
	case len(s.tokenless) > 0:
		return "did not seed any auth token: this login returned no token, and what it sent differs from the " +
			"configured body of every profile it could seed (" + profileNames(all) + "). Steps under each of " +
			"those profiles keep logging in with that profile's own body"
	}
	return ""
}

func profileNames(profiles []string) string {
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, profileLabel(p))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
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

func capIDs(ids []string, max int) string {
	if len(ids) <= max {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(ids[:max], ", "), len(ids)-max)
}

func firstFailedExpectation(sr *StepRecord) string {
	failed := []chain.ExpectResult{}
	for _, e := range sr.Expect {
		if !e.Passed {
			failed = append(failed, e)
		}
	}
	if len(failed) == 0 {
		return "expectation failed"
	}
	out := "expectation failed: " + chain.DescribeFailure(failed[0])
	if len(failed) > 1 {
		out += fmt.Sprintf(" (and %d more)", len(failed)-1)
	}
	return out
}

func failedExpectations(sr *StepRecord) string {
	parts := []string{}
	for _, e := range sr.Expect {
		if !e.Passed {
			parts = append(parts, chain.DescribeFailure(e))
		}
	}
	return "expectation failed: " + strings.Join(parts, "; ")
}

func declaredWant(e chain.Expectation, redactor *pathmask.Masker) any {
	declared := e.EvaluateIn(nil, nil)
	want := declared.Want
	if redactor.MasksValue(e.Path, want) {
		want = pathmask.MaskRedacted
	}
	switch declared.Rule {
	case "equals", "invalid":
		return want
	case "not_empty":
		return "not_empty"
	}
	return declared.Rule + " " + fmt.Sprint(want)
}

func unevaluated(expect []chain.Expectation, redactor *pathmask.Masker) []chain.ExpectResult {
	if len(expect) == 0 {
		return nil
	}
	out := make([]chain.ExpectResult, 0, len(expect))
	for _, e := range expect {
		out = append(out, chain.ExpectResult{
			Path:   e.Path,
			Rule:   "unevaluated",
			Want:   declaredWant(e, redactor),
			Passed: false,
			Detail: "the call was refused before a response body existed, so this assertion never ran. " +
				"allow_fail tolerates a refusal, not an assertion going unchecked. A refusal is asserted " +
				"with transport.code or transport.http_status",
		})
	}
	return out
}
