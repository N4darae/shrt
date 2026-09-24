package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/transport"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Runner struct {
	Catalog        *catalog.Catalog
	Client         *transport.Client
	Now            func() time.Time
	ValidateInput  bool
	ValidateOutput bool
	OnStep         func(*StepRecord)
	Auth           AuthBindings
	AuthRoute      func(*chain.Step) (string, bool)
	BuildHeader    string
}

type AuthBinding struct {
	Profile     string
	Procedure   string
	TokenPath   string
	ExpiresPath string
	Body        func() ([]byte, error)
	Sink        transport.TokenSink
	EnvVars     []string
	BodyFields  map[string]any
	Header      string
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

func (bs AuthBindings) learnSecrets(redactor *pathmask.Masker) {
	for _, b := range bs {
		if b == nil {
			continue
		}
		learnMaskedTemplate(redactor, b.BodyFields, "")
	}
}

func (bs AuthBindings) principals() map[string]string {
	defaults := pathmask.NewRedactor(config.DefaultRedact())
	out := map[string]string{}
	for _, b := range bs {
		if b == nil {
			continue
		}
		fields := map[string]any{}
		for k, v := range b.BodyFields {
			if defaults.Masks(k) {
				continue
			}
			resolved, err := chain.AuthBodyScope().ResolveValue(v)
			if err != nil {
				fields = nil
				break
			}
			fields[k] = withoutSecrets(defaults, resolved, k)
		}
		if fields == nil {
			continue
		}
		raw, err := json.Marshal(map[string]any{"call": b.Procedure, "fields": fields})
		if err != nil {
			continue
		}
		sum := sha256.Sum256(raw)
		out[b.Profile] = hex.EncodeToString(sum[:8])
	}
	return out
}

func withoutSecrets(secrets *pathmask.Masker, v any, path string) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range t {
			sub := pathmask.Join(path, k)
			if secrets.Masks(sub) {
				continue
			}
			out[k] = withoutSecrets(secrets, item, sub)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for i, item := range t {
			out = append(out, withoutSecrets(secrets, item, pathmask.Join(path, pathmask.IndexKey(i))))
		}
		return out
	}
	return v
}

func (bs AuthBindings) learnLoginResponse(redactor *pathmask.Masker, procedure string, response any) {
	login := false
	for _, b := range bs {
		if b == nil || b.Procedure != procedure {
			continue
		}
		login = true
		if token, ok := chain.Get(response, b.TokenPath); ok && b.TokenPath != "" {
			learnSecret(redactor, token)
		}
	}
	if login {
		learnMaskedValues(redactor, response, "")
	}
}

func learnMaskedValues(redactor *pathmask.Masker, v any, path string) {
	if path != "" && redactor.MasksValue(path, v) {
		learnSecret(redactor, v)
		return
	}
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			learnMaskedValues(redactor, item, pathmask.Join(path, k))
		}
	case []any:
		for i, item := range t {
			learnMaskedValues(redactor, item, pathmask.Join(path, pathmask.IndexKey(i)))
		}
	}
}

func learnMaskedInputs(redactor *pathmask.Masker, v any, path string, scope *chain.Scope) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			learnMaskedInputs(redactor, item, pathmask.Join(path, k), scope)
		}
	case []any:
		for i, item := range t {
			learnMaskedInputs(redactor, item, pathmask.Join(path, pathmask.IndexKey(i)), scope)
		}
	case string:
		if !redactor.Masks(path) {
			return
		}
		env := scope.Env
		if env == nil {
			env = os.LookupEnv
		}
		for _, name := range chain.AuthBodyEnvNames(map[string]any{"v": t}) {
			if value, ok := env(name); ok {
				redactor.AddSecret(value)
			}
		}
		for _, ref := range chain.VarRefs(t) {
			if value, err := scope.ResolveValue(ref); err == nil {
				learnSecret(redactor, value)
			}
		}
	}
}

func learnMaskedTemplate(redactor *pathmask.Masker, v any, path string) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			learnMaskedTemplate(redactor, item, pathmask.Join(path, k))
		}
	case []any:
		for i, item := range t {
			learnMaskedTemplate(redactor, item, pathmask.Join(path, pathmask.IndexKey(i)))
		}
	default:
		if !redactor.Masks(path) {
			return
		}
		resolved, err := chain.AuthBodyScope().ResolveValue(t)
		if err == nil && redactor.MasksValue(path, resolved) {
			learnSecret(redactor, resolved)
		}
	}
}

func (bs AuthBindings) learnTokens(redactor *pathmask.Masker) {
	for _, b := range bs {
		if b == nil {
			continue
		}
		if holder, ok := b.Sink.(interface{ CurrentToken() string }); ok {
			redactor.AddSecret(holder.CurrentToken())
		}
	}
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
	Vars       map[string]any
	Volatile   []string
	Redact     []string
	DryRun     bool
	KeepGoing  bool
	Build      string
	heldBack   map[int]string
	principals map[string]string
	chain      *chain.Chain
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

const (
	AuthRetryResent    = transport.AuthRetryResent
	AuthRetryNotResent = transport.AuthRetryNotResent
)

const freshTokenRefused = "may be an auth regression"

func RefusedFreshToken(sr *StepRecord) bool {
	return sr != nil && sr.Status == StatusError && strings.Contains(sr.Error, freshTokenRefused)
}

func authRefusedIsNoVerdict(sr *StepRecord, fresh string) {
	if sr.Status != StatusFailed {
		return
	}
	sr.Status = StatusError
	evidence := ""
	switch fresh {
	case transport.FreshTokenRelogin:
		evidence = "the backend refused this call at authentication, then a fresh login in this run succeeded and the call " +
			"was re-sent with the new token, and the backend refused that too"
	case transport.FreshTokenAccepted:
		evidence = "the backend refused a token that a login in this run had just issued and that it had accepted on an " +
			"earlier call of this run"
	case transport.FreshTokenMinted:
		evidence = "the backend refused a token that a login in this run had just issued"
	}
	if evidence != "" {
		sr.Error = joinLines(sr.Error, evidence+": the credentials work and the token is current, so this "+freshTokenRefused+
			" in the backend (this rpc refusing valid tokens), not a credentials problem. The step is error, not failed, "+
			"because the rpc itself never answered; re-run to confirm, and treat a repeat as a finding")
		return
	}
	sr.Error = joinLines(sr.Error, "the backend refused authentication for this call, so its answer is not a verdict about the rpc: "+
		"check the credentials of the step's auth profile and re-run")
}

func authRetryWarning(retry string, cached bool) string {
	if retry == AuthRetryResent && cached {
		return "the first attempt carried a token read from the on-disk cache that no call in this run had used yet, " +
			"and the backend refused it at authentication (a restart or a revoke), so it did not perform the call: the " +
			"token was dropped, a fresh login made, and this call re-sent. This record is the second answer"
	}
	if retry == AuthRetryResent {
		return "the first attempt was answered unauthenticated, so the token was dropped, a fresh login made, " +
			"and this call re-sent: the backend received it twice. It is a read (conventions.read_only_prefixes), " +
			"so sending it again changes nothing; this record is the second answer"
	}
	return "answered unauthenticated, and not re-sent: this is not a read (conventions.read_only_prefixes), and " +
		"the backend may already have performed it, so sending it again could perform it twice. The token was " +
		"dropped, so the next call logs in fresh; re-run the chain if the token had simply expired"
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
	if err := r.checkCalls(c); err != nil {
		return nil, err
	}
	if err := r.checkAuthEnv(c); err != nil {
		return nil, err
	}
	if err := r.checkHandWrittenAuth(c); err != nil {
		return nil, err
	}
	problems := c.PreflightProblems()
	problems = append(problems, c.RedactedPinProblems(rec.Redacted)...)
	if r.Catalog != nil {
		problems = append(problems, c.ResponseRefProblems(r.Catalog)...)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("chain %q cannot run to the end, so nothing was sent: %s", c.Name, strings.Join(problems, "; "))
	}
	redactor := pathmask.NewRedactor(rec.Redacted)
	scope := chain.NewScope(rec.Vars)
	if r.Now != nil {
		scope.Now = r.Now
	}
	r.Auth.learnSecrets(redactor)
	opts.principals = r.Auth.principals()
	for _, step := range c.Steps {
		if step != nil {
			learnMaskedInputs(redactor, orEmpty(step.Body), "", scope)
		}
	}
	r.Auth.learnTokens(redactor)
	if problems := r.requestProblems(c, rec.Vars); len(problems) > 0 {
		return nil, errors.New(redactor.ScrubText(fmt.Sprintf("chain %q has a request that does not match its rpc, so nothing was sent "+
			"(checked as -dry-run does, with synthetic values for references to earlier responses): %s",
			c.Name, strings.Join(problems, "; "))))
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
	pastPins := len(c.KeptRed) > 0 && !opts.DryRun
	keepGoing := opts.KeepGoing || pastPins
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
		if keepGoing {
			if ref, producer, ok := readsBroken(step, broken, exporter); ok {
				sr = r.skippedBehind(i, step, ref, producer)
				behind, behindOn = true, producer.ID
			}
		}
		if sr == nil {
			stepOpts := opts
			stepOpts.chain = c
			if keepGoing {
				stepOpts.heldBack = heldBackExpectations(step, broken, exporter)
			}
			sr = r.runStep(ctx, scope, i, step, stepOpts, redactor)
		}
		scrubStep(sr, redactor)
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
		if !keepGoing {
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
	for i, sr := range rec.Steps {
		if i < len(c.Steps) && c.Steps[i] != nil && len(c.Unordered)+len(c.Steps[i].Unordered) > 0 {
			sr.Unordered = append(append([]string{}, c.Unordered...), c.Steps[i].Unordered...)
		}
	}
	for at, ids := range repeats {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, strconv.Quote(id))
		}
		failures[at] += fmt.Sprintf("\nthe same for %d more step(s): %s", len(ids), capIDs(quoted, 10))
	}
	switch {
	case pastPins && len(failures) == 1 && failedCount == 1 && unreached == 0:
		rec.Failure = failures[0]
	case pastPins && len(failures) > 0:
		rec.Failure = fmt.Sprintf("kept_red: ran every step, as -keep-going does; %d of %d steps did not pass\n", failedCount+unreached, len(c.Steps)) +
			strings.Join(failures, "\n")
	case len(failures) > 0:
		rec.Failure = fmt.Sprintf("-keep-going: %d of %d steps did not pass\n", failedCount+unreached, len(c.Steps)) +
			strings.Join(failures, "\n")
	}
	if unreached > 0 {
		rec.Failure += fmt.Sprintf("\n%d later step(s) not sent: %s", unreached, unreachableReason(r.Client.BaseURL(), dead))
	}

	if !opts.DryRun {
		rec.Warning = envelopeOKNeverSeen(rec.Steps)
	}
	rec.Exports = r.maskExports(scope.Exports, c.Steps, redactor)
	if masked, ok := redactor.Apply(rec.Vars).(map[string]any); ok {
		rec.Vars = masked
	}
	if scrubbed, ok := redactor.ScrubValue(rec.Exports).(map[string]any); ok && rec.Exports != nil {
		rec.Exports = scrubbed
	}
	if scrubbed, ok := redactor.ScrubValue(rec.Vars).(map[string]any); ok && rec.Vars != nil {
		rec.Vars = scrubbed
	}
	rec.Failure = redactor.ScrubText(rec.Failure)
	rec.Warning = redactor.ScrubText(rec.Warning)
	if !opts.DryRun {
		rec.KeptRed, rec.KeptRedNote = keptRedVerdict(c, rec)
		rec.KeptRedNote = redactor.ScrubText(rec.KeptRedNote)
	}
	rec.DurationMS = r.clock().Sub(now).Milliseconds()
	return rec, nil
}

func (r *Runner) requestProblems(c *chain.Chain, vars map[string]any) []string {
	if !r.ValidateInput || r.Catalog == nil {
		return nil
	}
	scope := chain.NewScope(vars)
	if r.Now != nil {
		scope.Now = r.Now
	}
	problems := []string{}
	for i, step := range c.Steps {
		if step == nil {
			continue
		}
		method, err := r.Catalog.Lookup(step.Call)
		if err != nil || method.Streaming() {
			continue
		}
		resolved, err := scope.ResolveValue(orEmpty(step.Body))
		if err == nil {
			if body, merr := json.Marshal(resolved); merr == nil {
				if verr := r.Catalog.ValidateInput(method, body); verr != nil && !r.validWithoutSynthetic(scope, method, orEmpty(step.Body), vars) {
					problems = append(problems, fmt.Sprintf("step %q (step %d): %v", step.ID, i+1, verr))
				}
			}
		}
		sample := catalog.Scaffold(method.Output())
		scope.RecordSynthetic(step.ID, resolved, sample)
		for name, path := range step.Export {
			if v, ok := chain.Get(sample, path); ok {
				scope.Exports[name] = v
			}
		}
	}
	return problems
}

func (r *Runner) validWithoutSynthetic(scope *chain.Scope, method *catalog.Method, body map[string]any, vars map[string]any) bool {
	stripped, changed := dropSynthetic(body, vars)
	if !changed {
		return false
	}
	resolved, err := scope.ResolveValue(stripped)
	if err != nil {
		return true
	}
	raw, err := json.Marshal(resolved)
	if err != nil {
		return true
	}
	return r.Catalog.ValidateInput(method, raw) == nil
}

func dropSynthetic(v any, vars map[string]any) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		changed := false
		for k, item := range t {
			if readsSynthetic(item, vars) {
				changed = true
				continue
			}
			kept, c := dropSynthetic(item, vars)
			out[k], changed = kept, changed || c
		}
		return out, changed
	case []any:
		out := make([]any, 0, len(t))
		changed := false
		for _, item := range t {
			if readsSynthetic(item, vars) {
				changed = true
				continue
			}
			kept, c := dropSynthetic(item, vars)
			out, changed = append(out, kept), changed || c
		}
		return out, changed
	}
	return v, false
}

func readsSynthetic(v any, vars map[string]any) bool {
	text, ok := v.(string)
	if !ok {
		return false
	}
	for _, ref := range chainRefs(text) {
		r := chain.ParseRef(ref)
		switch r.Kind {
		case chain.RefExports:
			return true
		case chain.RefStep:
			if !strings.HasPrefix(r.Rest, "request") {
				return true
			}
		case chain.RefBare:
			if _, isVar := vars[r.Head]; !isVar {
				return true
			}
		}
	}
	return false
}

var refExpr = regexp.MustCompile(`\$\{([^}]+)\}`)

func chainRefs(text string) []string {
	out := []string{}
	for _, m := range refExpr.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
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
	outputFields := catalog.DescribeMessage(method.Output()).Fields
	requestRedactor := redactor.WithNumeric(numericPaths(catalog.DescribeMessage(method.Input()).Fields))
	redactor = redactor.WithNumeric(numericPaths(outputFields))

	resolved, err := scope.ResolveValue(orEmpty(step.Body))
	if err != nil {
		return fail(sr, chain.ExplainLaterRef(opts.chain, i, err))
	}
	scope.RecordRequest(step.ID, resolved)
	sr.BodyRefs = BodyRefs(step.Body)
	body, err := json.Marshal(resolved)
	if err != nil {
		return fail(sr, fmt.Errorf("encode request: %w", err))
	}
	sr.Request = mustJSON(requestRedactor.Apply(resolved), body)

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
	r.Auth.learnTokens(redactor)
	if profile, routed := transport.CallAuthProfile(call); routed {
		sr.AuthProfile = firstNonEmpty(profile, NoAuthProfile)
		sr.AuthPrincipal = opts.principals[profile]
	}
	if retry, _ := call.Meta[transport.MetaAuthRetry].(string); retry != "" {
		sr.AuthRetry = retry
		cached, _ := call.Meta[transport.MetaAuthRetryCached].(bool)
		sr.Warning = joinLines(sr.Warning, authRetryWarning(retry, cached))
	}
	if refused, _ := call.Meta[transport.MetaAuthRefused].(bool); refused && !step.AllowFail {
		fresh, _ := call.Meta[transport.MetaAuthRefusedFresh].(string)
		defer authRefusedIsNoVerdict(sr, fresh)
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

	if key, repeated := repeatedKey(res.Body, method.Output()); repeated {
		var last any
		if err := json.Unmarshal(res.Body, &last); err == nil {
			sr.Response = mustJSON(redactor.Apply(last), res.Body)
		}
		sr.Status = StatusFailed
		sr.Expect = unevaluatedBecause(step.Expect, redactor, "the backend answered, but the body repeats a key, so this assertion never ran")
		sr.Error = fmt.Sprintf("the response repeats the key %s, so it carries two values for one field and decoders "+
			"disagree on which one counts (this record keeps the last). Nothing here is a verdict: the expectations "+
			"were not evaluated. A backend that writes a key twice is the defect to report", key)
		return sr
	}
	canonical, populated, unknown, cerr := r.Catalog.CanonicalizeDiscardingUnknown(method.Output(), res.Body)
	if cerr != nil && unknownHoldsAVerdict(unknown) {
		unknown = nil
	}
	staleOutput := false
	switch {
	case cerr == nil:
	case r.ValidateOutput:
		sr.Response = jsonBodyOrString(res.Body)
		if len(unknown) > 0 {
			var decoded any
			if err := json.Unmarshal(canonical, &decoded); err == nil {
				sr.Response = mustJSON(redactor.Apply(decoded), canonical)
			}
		}
		sr.Status = StatusFailed
		sr.Drift = true
		sr.Expect = unevaluatedBecause(step.Expect, redactor, "the backend answered, but conventions.validate_output "+
			"is on and the body does not match the response message, so this assertion never ran")
		sr.Error = "conventions.validate_output is on and this response does not match " +
			string(method.Output().FullName()) + ": " + cerr.Error() +
			"\n       The request WAS sent and the backend answered " + fmt.Sprint(res.Status) +
			". Nothing here is evidence about the rpc: the expectations were not evaluated, because " +
			"the body they would read could not be decoded. Rebuild the descriptor ('shrt catalog " +
			"build') and re-run before reading it as a backend defect."
		return sr
	case len(unknown) > 0:
		sr.Warning = joinLines(sr.Warning, "response carries field(s) "+string(method.Output().FullName())+
			" does not declare, discarded before the expectations ran: "+strings.Join(unknown, ", ")+
			". An added field is backward compatible; rebuild the descriptor ('shrt catalog build') to read it. "+
			"A field is read only under its proto name or its JSON name, exactly as protojson reads it, so a name "+
			"listed here that differs from a declared one only in case or separators is not read either")
	default:
		canonical = res.Body
		populated = res.Body
		staleOutput = true
		sr.Warning = joinLines(sr.Warning, "response kept as sent, it does not match "+string(method.Output().FullName())+
			" (the descriptor may be stale): "+cerr.Error())
	}

	var decoded any
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return fail(sr, fmt.Errorf("decode response: %w", err))
	}
	var sent any
	if err := json.Unmarshal(populated, &sent); err != nil {
		return fail(sr, fmt.Errorf("decode response: %w", err))
	}
	r.Auth.learnLoginResponse(redactor, method.Procedure(), decoded)
	sr.Response = mustJSON(redactor.Apply(decoded), canonical)
	scope.Record(step.ID, resolved, decoded)
	canonicalInput := func(raw []byte) ([]byte, error) { return r.Catalog.Canonicalize(method.Input(), raw) }
	sr.Note = seedingNote(r.Auth.observe(step.Auth, method.Procedure(), body, canonicalInput, decoded))

	for i, e := range step.Expect {
		response, presence := any(decoded), sent
		if chain.IsTransportPath(e.Path) {
			response, presence = outcome, outcome
		}
		kind := ""
		if !chain.IsTransportPath(e.Path) {
			kind = fieldKind(outputFields, e.Path)
		}
		result := evaluateTyped(scope, e, response, presence, kind, redactor)
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

	verdictDeclared := (!staleOutput && catalog.HasResponsePath(outputFields, chain.SplitPath(chain.EnvelopePath()))) ||
		verdictBlocked(decoded, chain.EnvelopePath())
	if len(step.Expect) == 0 {
		if warning := unassertedRefusalWarning(decoded, verdictDeclared); warning != "" {
			sr.Warning = joinLines(sr.Warning, warning)
		}
	} else if result, refused := unpinnedRefusal(scope, decoded, step.Expect, verdictDeclared, redactor); refused {
		sr.Expect = append(sr.Expect, result)
		sr.Status = StatusFailed
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
		if surprises := chain.UndeclaredRefusals(refusals, boundExpect(scope, step.Expect)); len(surprises) > 0 {
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
				learnSecret(redactor, v)
				sr.Exported[name] = pathmask.MaskRedacted
				continue
			}
			sr.Exported[name] = v
		}
	}
	return sr
}

func learnSecret(redactor *pathmask.Masker, v any) {
	switch t := v.(type) {
	case map[string]any:
		for _, item := range t {
			learnSecret(redactor, item)
		}
	case []any:
		for _, item := range t {
			learnSecret(redactor, item)
		}
	case string:
		redactor.AddSecret(t)
	case nil, bool:
	default:
		redactor.AddSecret(fmt.Sprint(t))
	}
}

func scrubStep(sr *StepRecord, redactor *pathmask.Masker) {
	sr.Request = redactor.ScrubJSON(sr.Request)
	sr.Response = redactor.ScrubJSON(sr.Response)
	for i := range sr.Expect {
		e := &sr.Expect[i]
		e.Want = redactor.ScrubValue(e.Want)
		e.Got = redactor.ScrubValue(e.Got)
		e.Detail = redactor.ScrubText(e.Detail)
	}
	if scrubbed, ok := redactor.ScrubValue(sr.Exported).(map[string]any); ok && sr.Exported != nil {
		sr.Exported = scrubbed
	}
	sr.Error = redactor.ScrubText(sr.Error)
	sr.Warning = redactor.ScrubText(sr.Warning)
	sr.Note = redactor.ScrubText(sr.Note)
	if sr.Transport != nil {
		sr.Transport.Message = redactor.ScrubText(sr.Transport.Message)
	}
}

func unassertedRefusalWarning(decoded any, verdictDeclared bool) string {
	path := chain.EnvelopePath()
	if path == "" {
		return ""
	}
	text, has := verdictText(decoded, path)
	if !has {
		if !verdictDeclared {
			return ""
		}
		return fmt.Sprintf("the response carries no verdict at %s (absent or empty, where this rpc's response "+
			"message declares one), and this step declares no expect, so it is recorded passed with nothing "+
			"checked; nothing says the backend did what was asked", path)
	}
	if text == chain.EnvelopeOK() {
		return ""
	}
	return fmt.Sprintf("refused in-band (%s = %s, not %s), and this step declares no expect, so it is "+
		"recorded passed with nothing checked. If the refusal is the point, assert it (%s equals: %s); if "+
		"not, the backend rejected this call and later steps that read its response read zero values", path, text,
		chain.EnvelopeOK(), path, text)
}

func verdictBlocked(decoded any, path string) bool {
	segs := chain.SplitPath(path)
	cur := decoded
	for i, seg := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return i > 0 && cur != nil
		}
		key, ok := namecase.LookupKey(m, seg)
		if !ok {
			return false
		}
		cur = m[key]
	}
	return false
}

func repeatedKey(raw []byte, md protoreflect.MessageDescriptor) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func(path string, md protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor) (string, bool, error)
	walk = func(path string, md protoreflect.MessageDescriptor, fd protoreflect.FieldDescriptor) (string, bool, error) {
		tok, err := dec.Token()
		if err != nil {
			return "", false, err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return "", false, nil
		}
		switch delim {
		case '{':
			var fields protoreflect.FieldDescriptors
			var mapValue protoreflect.FieldDescriptor
			switch {
			case fd != nil && fd.IsMap():
				mapValue = fd.MapValue()
			case md != nil && !strings.HasPrefix(string(md.FullName()), "google.protobuf."):
				fields = md.Fields()
			}
			seen := map[string]string{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return "", false, err
				}
				key, _ := keyTok.(string)
				child := pathmask.Join(path, key)
				identity := key
				var childField protoreflect.FieldDescriptor
				if mapValue != nil {
					childField = mapValue
				} else if fields != nil {
					if f := fields.ByJSONName(key); f != nil {
						childField = f
					} else if f := fields.ByName(protoreflect.Name(key)); f != nil {
						childField = f
					}
					if childField != nil {
						identity = string(childField.Name())
					}
				}
				if first, dup := seen[identity]; dup {
					if first != key {
						return child + " (also spelt " + first + ")", true, nil
					}
					return child, true, nil
				}
				seen[identity] = key
				var childMsg protoreflect.MessageDescriptor
				if childField != nil && !childField.IsMap() {
					childMsg = childField.Message()
				}
				if at, found, err := walk(child, childMsg, childField); found || err != nil {
					return at, found, err
				}
			}
		case '[':
			var elem protoreflect.MessageDescriptor
			if fd != nil && fd.IsList() {
				elem = fd.Message()
			}
			for i := 0; dec.More(); i++ {
				if at, found, err := walk(pathmask.Join(path, pathmask.IndexKey(i)), elem, nil); found || err != nil {
					return at, found, err
				}
			}
		}
		_, err = dec.Token()
		return "", false, err
	}
	at, found, _ := walk("", md, nil)
	return at, found
}

func verdictText(decoded any, path string) (string, bool) {
	code, ok := chain.Get(decoded, path)
	if !ok || code == nil {
		return "", false
	}
	text := fmt.Sprint(code)
	return text, text != ""
}

func unpinnedRefusal(scope *chain.Scope, decoded any, expect []chain.Expectation, verdictDeclared bool, redactor *pathmask.Masker) (chain.ExpectResult, bool) {
	path := chain.EnvelopePath()
	if path == "" {
		return chain.ExpectResult{}, false
	}
	text, has := verdictText(decoded, path)
	if pinsVerdict(scope, expect, !has) {
		return chain.ExpectResult{}, false
	}
	if !has {
		if !verdictDeclared {
			return chain.ExpectResult{}, false
		}
		why := "it is absent or empty, though this rpc's response message declares it"
		if verdictBlocked(decoded, path) {
			why = "a value on the way to it is not an object (as in a string status), so no code can be read there"
		}
		detail := fmt.Sprintf("the response carries no verdict at %s: "+why+", and no expectation on this step pins the verdict, so the ones that "+
			"held read zero values nothing vouches for. If an absent verdict is what this rpc answers, pin it "+
			"(%s exists: false); if not, the backend did not say it did what was asked", path, path)
		return chain.ExpectResult{Path: path, Rule: "envelope", Want: chain.EnvelopeOK(), Got: "", Passed: false, Detail: detail}, true
	}
	if text == chain.EnvelopeOK() {
		return chain.ExpectResult{}, false
	}
	detail := fmt.Sprintf("refused in-band (%s = %s, not %s), and no expectation on this step pins the verdict, "+
		"so the ones that held read the zero values a refusal leaves. If the refusal is the point, assert it "+
		"(%s equals: %s); if not, the backend rejected this call", path, text, chain.EnvelopeOK(), path, text)
	if context := refusalContext(decoded, path, redactor); context != "" {
		detail += " (" + context + ")"
	}
	return chain.ExpectResult{Path: path, Rule: "envelope", Want: chain.EnvelopeOK(), Got: text, Passed: false, Detail: detail}, true
}

func pinsVerdict(scope *chain.Scope, expect []chain.Expectation, absent bool) bool {
	okEnvelope := okAnswer(chain.EnvelopePath(), chain.EnvelopeOK())
	okTransport := chain.TransportOutcome(200, "", "")
	for _, e := range boundExpect(scope, expect) {
		switch {
		case chain.IsTransportPath(e.Path):
			if !e.EvaluateTyped(okTransport, okTransport, "").Passed {
				return true
			}
		case chain.CoversVerdict(e.Path):
			if absent && e.NotEqual != nil {
				continue
			}
			if (e.Equals != nil && chain.IsVerdictItself(e.Path)) || !e.EvaluateTyped(okEnvelope, okEnvelope, "").Passed {
				return true
			}
		}
	}
	return false
}

func boundExpect(scope *chain.Scope, expect []chain.Expectation) []chain.Expectation {
	out := make([]chain.Expectation, len(expect))
	for i, e := range expect {
		out[i] = e
		if bound, err := e.ResolveWith(scope); err == nil {
			out[i] = bound
		}
	}
	return out
}

func okAnswer(path, ok string) map[string]any {
	segs := chain.SplitPath(path)
	root := map[string]any{}
	node := root
	for i, seg := range segs {
		if i == len(segs)-1 {
			node[seg] = ok
			break
		}
		next := map[string]any{}
		node[seg] = next
		node = next
	}
	return root
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
	missing := ""
	for _, r := range surprises {
		if r.Code == chain.NoItemVerdict {
			missing = fmt.Sprintf(". A line marked %s carries no verdict at all while another line of the same "+
				"batch carries %s explicitly, so nothing says that line succeeded", chain.NoItemVerdict, chain.EnvelopeOK())
			break
		}
	}
	return "every item in a batch response carries its own verdict, and these were refused while " + said + missing +
		" — a step that asserts only the envelope would pass having achieved nothing. A line this step " +
		"MEANS to be refused is declared by pinning that line's verdict path, or one of its code fields (" +
		strings.Join(chain.CodeFields(), ", ") + "), with equals, not_equal or contains, and is then not " +
		"reported here"
}

func evaluate(scope *chain.Scope, e chain.Expectation, response, presence any, redactor *pathmask.Masker) chain.ExpectResult {
	return evaluateTyped(scope, e, response, presence, "", redactor)
}

func evaluateTyped(scope *chain.Scope, e chain.Expectation, response, presence any, kind string, redactor *pathmask.Masker) chain.ExpectResult {
	bound, err := e.ResolveWith(scope)
	if err != nil {
		return chain.ExpectResult{Path: e.Path, Rule: "unresolved", Passed: false, Detail: err.Error()}
	}
	result := bound.EvaluateTyped(response, presence, kind)
	if redactor.MasksValue(e.Path, result.Got) {
		result.Got = pathmask.MaskRedacted
	}
	if redactor.MasksValue(e.Path, result.Want) {
		result.Want = pathmask.MaskRedacted
	}
	return result
}

func fieldKind(fields []*catalog.Field, path string) string {
	segs := chain.SplitPath(path)
	f, ok := catalog.ResponseFieldAt(fields, segs)
	if !ok || f == nil || f.MapKey != "" || len(segs) == 0 {
		return ""
	}
	if f.Repeated && !isIndexSegment(segs[len(segs)-1]) {
		return ""
	}
	return f.Kind
}

func numericPaths(fields []*catalog.Field) func(string) bool {
	return func(path string) bool { return chain.IsNumericKind(fieldKind(fields, path)) }
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

func (r *Runner) maskExports(exports map[string]any, steps []*chain.Step, redactor *pathmask.Masker) map[string]any {
	if len(exports) == 0 {
		return exports
	}
	out := make(map[string]any, len(exports))
	for name, v := range exports {
		out[name] = v
	}
	for _, s := range steps {
		masker := redactor
		if r.Catalog != nil {
			if m, err := r.Catalog.Lookup(s.Call); err == nil {
				masker = redactor.WithNumeric(numericPaths(catalog.DescribeMessage(m.Output()).Fields))
			}
		}
		for name, path := range s.Export {
			if _, exported := out[name]; exported && masker.MasksValue(path, out[name]) {
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

func (r *Runner) checkCalls(c *chain.Chain) error {
	if r.Catalog == nil {
		return nil
	}
	for i, step := range c.Steps {
		method, err := r.Catalog.Lookup(step.Call)
		if err != nil {
			return fmt.Errorf("step %q (step %d) calls %q, which the catalog does not have, so nothing was sent: %w",
				step.ID, i+1, step.Call, err)
		}
		if method.Streaming() {
			return fmt.Errorf("step %q (step %d) cannot be sent, so nothing was sent: %s", step.ID, i+1, method.StreamRefusal())
		}
	}
	return nil
}

func (r *Runner) checkAuthEnv(c *chain.Chain) error {
	if r.AuthRoute == nil {
		return nil
	}
	byProfile := map[string]*AuthBinding{}
	for _, b := range r.Auth {
		if b != nil {
			byProfile[b.Profile] = b
		}
	}
	for i, step := range c.Steps {
		if step == nil || step.SkipAuth {
			continue
		}
		profile, routed := r.AuthRoute(step)
		b := byProfile[profile]
		if !routed || b == nil {
			continue
		}
		unset := []string{}
		for _, name := range b.EnvVars {
			if _, set := os.LookupEnv(name); !set {
				unset = append(unset, name)
			}
		}
		if len(unset) == 0 {
			continue
		}
		refs := make([]string, 0, len(unset))
		for _, name := range unset {
			refs = append(refs, "${env."+name+"}")
		}
		return fmt.Errorf("step %q (step %d) runs under auth profile %q, whose login body reads %s, and env %s "+
			"is not set, so nothing was sent: the login would fail at that step, after every step before it "+
			"had already hit the backend. Export %s", step.ID, i+1, profile, strings.Join(refs, ", "),
			strings.Join(unset, ", "), strings.Join(unset, ", "))
	}
	return nil
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

func unknownHoldsAVerdict(unknown []string) bool {
	verdicts := []string{chain.EnvelopePath(), chain.ItemEnvelope()}
	for _, u := range unknown {
		for _, v := range verdicts {
			if v != "" && (v == u || strings.HasPrefix(v, u+".")) {
				return true
			}
		}
	}
	return false
}

func unevaluated(expect []chain.Expectation, redactor *pathmask.Masker) []chain.ExpectResult {
	return unevaluatedBecause(expect, redactor, "the call was refused before a response body existed, so this assertion never ran. "+
		"allow_fail tolerates a refusal, not an assertion going unchecked. A refusal is asserted "+
		"with transport.code or transport.http_status")
}

func unevaluatedBecause(expect []chain.Expectation, redactor *pathmask.Masker, why string) []chain.ExpectResult {
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
			Detail: why,
		})
	}
	return out
}
