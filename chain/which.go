package chain

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const (
	WhyRPC  = "rpc"
	WhyCode = "code"
)

type CodeAssertion struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

type Observation struct {
	Run      string
	Step     string
	Status   string
	Reached  bool
	Response any
	Failures []ExpectResult
}

type WhichQuery struct {
	RPC     string
	Code    string
	Aliases []string
}

func (q WhichQuery) Codes() []string {
	if q.Code == "" {
		return nil
	}
	return append([]string{q.Code}, q.Aliases...)
}

type WhichOptions struct {
	RPCOf        func(*Step) string
	SliceOf      func(*Chain, string) (int, bool)
	Observations func(chainName string) []Observation
	FreshVars    func(c *Chain, step, run string) []string
	ReadsOnly    func(*Step) bool
}

type WhichEvidence struct {
	Run       string         `json:"run"`
	Status    string         `json:"status"`
	Code      string         `json:"code,omitempty"`
	Path      string         `json:"path,omitempty"`
	Asserted  string         `json:"asserted_path,omitempty"`
	Holds     bool           `json:"holds"`
	Failures  []ExpectResult `json:"failures,omitempty"`
	NewerRuns int            `json:"newer_runs_not_reaching,omitempty"`
}

type WhichNewest struct {
	Run           string `json:"run"`
	Status        string `json:"status"`
	StoppedAt     string `json:"stopped_at,omitempty"`
	StoppedStatus string `json:"stopped_status,omitempty"`
}

type WhichStep struct {
	Step       string          `json:"step"`
	Index      int             `json:"index"`
	Call       string          `json:"call"`
	Why        []string        `json:"why"`
	Asserts    []CodeAssertion `json:"asserts,omitempty"`
	SliceSteps int             `json:"slice_steps,omitempty"`
	Observed   *WhichEvidence  `json:"observed,omitempty"`
	Newest     *WhichNewest    `json:"newest_unreached,omitempty"`
	ByReason   string          `json:"matched_by_reason,omitempty"`
	Kind       string          `json:"kind,omitempty"`
}

const WhichKindAuthProbe = "auth_probe"

func IsAuthProbe(s *Step) bool {
	if s == nil {
		return false
	}
	withoutToken := s.SkipAuth || s.Auth == InvalidTokenAuth
	for _, e := range s.Expect {
		if !ExpectsTransportRefusal(e) {
			continue
		}
		if withoutToken {
			return true
		}
		switch strings.ToLower(stringify(e.Equals)) {
		case "unauthenticated", "permission_denied", "401", "403":
			return true
		}
	}
	return false
}

type WhichChain struct {
	Chain    string      `json:"chain"`
	Source   string      `json:"source,omitempty"`
	Steps    int         `json:"steps"`
	Runs     int         `json:"runs"`
	Observed bool        `json:"observed"`
	Best     string      `json:"best_step"`
	Command  string      `json:"command"`
	Matches  []WhichStep `json:"matches"`

	CommandSteps  int    `json:"command_steps,omitempty"`
	Fallback      string `json:"fallback_command,omitempty"`
	FallbackSteps int    `json:"fallback_steps,omitempty"`
}

func IsCodePath(path string) bool {
	if path == TransportPrefix+".code" || path == TransportPrefix+".http_status" {
		return true
	}
	segs := SplitPath(path)
	if len(segs) == 0 {
		return false
	}
	last := segs[len(segs)-1]
	for _, name := range CodeFields() {
		if last == name {
			return true
		}
	}
	if last != EnvelopeLeaf() {
		return false
	}
	return len(segs) >= 2 && segs[len(segs)-2] == EnvelopeField()
}

func CodePaths(chains []*Chain) []string {
	seen := map[string]bool{}
	for _, c := range chains {
		for _, s := range c.Steps {
			for _, e := range s.Expect {
				if IsCodePath(e.Path) {
					seen[e.Path] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func assertedCodes(s *Step) []CodeAssertion {
	out := []CodeAssertion{}
	for _, e := range s.Expect {
		if e.Equals == nil || !IsCodePath(e.Path) {
			continue
		}
		out = append(out, CodeAssertion{Path: e.Path, Value: stringify(e.Equals)})
	}
	return out
}

func Which(chains []*Chain, q WhichQuery, opts WhichOptions) []WhichChain {
	paths := CodePaths(chains)
	out := []WhichChain{}
	for _, c := range chains {
		matches := []WhichStep{}
		for i, s := range c.Steps {
			why := []string{}
			if q.RPC != "" {
				if !stepCalls(s, q.RPC, opts.RPCOf) {
					continue
				}
				why = append(why, WhyRPC)
			}
			asserts := assertedCodes(s)
			byReason := ""
			if q.Code != "" {
				matched, reason := q.matchAsserts(asserts)
				if !matched {
					continue
				}
				byReason = reason
				why = append(why, WhyCode)
			}
			kind := ""
			if IsAuthProbe(s) {
				kind = WhichKindAuthProbe
			}
			matches = append(matches, WhichStep{
				Step: s.ID, Index: i + 1, Call: s.Call, Why: why, Asserts: asserts, ByReason: byReason, Kind: kind,
			})
		}
		if len(matches) == 0 {
			continue
		}
		hit := WhichChain{Chain: c.Name, Source: c.SourcePath, Steps: len(c.Steps)}
		byStep, order := observationsFor(c.Name, opts.Observations)
		hit.Runs = len(order)
		for i := range matches {
			assert, _ := PrimaryAssertionFor(matches[i].Asserts, q)
			matches[i].Observed = evidenceFor(byStep[matches[i].Step], order, paths, assert, q.Code != "")
			matches[i].Newest = newestUnreached(byStep[matches[i].Step], order, lastStepOf(c.Name, opts.Observations))
			if matches[i].Observed != nil {
				hit.Observed = true
			}
			if opts.SliceOf != nil {
				if n, ok := opts.SliceOf(c, matches[i].Step); ok {
					matches[i].SliceSteps = n
				}
			}
		}
		sortMatches(matches, q.Code == "")
		hit.Matches = matches
		best := matches[0]
		for _, m := range matches {
			if m.Observed != nil && m.Observed.Status != statusPassed {
				best = m
				break
			}
		}
		hit.Best = best.Step
		hit.Command = reproCommand(c, best, opts)
		out = append(out, hit)
	}
	sortWhich(out, q.Code == "")
	return out
}

func reproCommand(c *Chain, best WhichStep, opts WhichOptions) string {
	cmd := "shrt chain slice " + c.Name + " -step " + best.Step
	run := ""
	step, _ := c.Step(best.Step)
	switch {
	case best.Observed == nil:
	case IsAuthProbe(step):
	case writeStep(c, best.Step, opts.ReadsOnly):
		cmd += " -keep " + SliceKeepWrites
	default:
		run = best.Observed.Run
		cmd += " -mode pin -run " + run
	}
	return cmd + freshFlags(c, best.Step, run, opts.FreshVars)
}

func writeStep(c *Chain, id string, readsOnly func(*Step) bool) bool {
	s, ok := c.Step(id)
	if !ok || IsReadOnlyCall(s.Call) {
		return false
	}
	return readsOnly == nil || !readsOnly(s)
}

func freshFlags(c *Chain, step, run string, fresh func(*Chain, string, string) []string) string {
	if fresh == nil {
		return ""
	}
	names := append([]string{}, fresh(c, step, run)...)
	sort.Strings(names)
	out := ""
	for _, name := range names {
		out += " -var " + name + "=<fresh>"
	}
	return out
}

func observationsFor(chainName string, load func(string) []Observation) (map[string][]Observation, []string) {
	byStep := map[string][]Observation{}
	if load == nil {
		return byStep, nil
	}
	order := []string{}
	seen := map[string]bool{}
	for _, o := range load(chainName) {
		byStep[o.Step] = append(byStep[o.Step], o)
		if !seen[o.Run] {
			seen[o.Run] = true
			order = append(order, o.Run)
		}
	}
	return byStep, order
}

func (q WhichQuery) IsNumericCode() bool {
	return isDigits(strings.TrimSpace(q.Code))
}

func (q WhichQuery) matchAsserts(asserts []CodeAssertion) (bool, string) {
	if assertsCode(asserts, q.Code) {
		return true, ""
	}
	if !q.IsNumericCode() {
		return assertsAnyCode(asserts, q.Aliases), ""
	}
	for _, a := range asserts {
		if isDigits(a.Value) {
			return false, ""
		}
	}
	for _, alias := range q.Aliases {
		if !isDigits(alias) && assertsCode(asserts, alias) {
			return true, alias
		}
	}
	return false, ""
}

func (q WhichQuery) matchResponse(response any) (string, string, bool) {
	if path, ok := findCode(response, q.Code, ""); ok {
		return path, q.Code, true
	}
	for _, alias := range q.Aliases {
		path, ok := findCode(response, alias, "")
		if !ok {
			continue
		}
		if q.IsNumericCode() && !isDigits(alias) && siblingCodeDiffers(response, path, q.Code) {
			continue
		}
		return path, alias, true
	}
	return "", "", false
}

func siblingCodeDiffers(response any, path, code string) bool {
	segs := SplitPath(path)
	if len(segs) == 0 {
		return false
	}
	parent := response
	if len(segs) > 1 {
		v, ok := Get(response, strings.Join(segs[:len(segs)-1], "."))
		if !ok {
			return false
		}
		parent = v
	}
	obj, ok := parent.(map[string]any)
	if !ok {
		return false
	}
	for _, name := range CodeFields() {
		if v, ok := obj[name]; ok {
			if text := stringify(v); isDigits(text) && !strings.EqualFold(text, code) {
				return true
			}
		}
	}
	return false
}

func PrimaryAssertionFor(asserts []CodeAssertion, q WhichQuery) (CodeAssertion, bool) {
	for _, code := range q.Codes() {
		if a, ok := PrimaryAssertion(asserts, code); ok {
			return a, true
		}
	}
	if q.Code != "" {
		return CodeAssertion{}, false
	}
	return PrimaryAssertion(asserts, "")
}

func PrimaryAssertion(asserts []CodeAssertion, code string) (CodeAssertion, bool) {
	if code != "" {
		for _, a := range asserts {
			if strings.EqualFold(a.Value, code) {
				return a, true
			}
		}
		return CodeAssertion{}, false
	}
	for _, a := range asserts {
		if isDigits(a.Value) {
			return a, true
		}
	}
	if len(asserts) > 0 {
		return asserts[0], true
	}
	return CodeAssertion{}, false
}

func isDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

func IsDigits(s string) bool { return isDigits(s) }

func evidenceFor(list []Observation, order []string, paths []string, assert CodeAssertion, byCode bool) *WhichEvidence {
	var last *Observation
	for i := range list {
		if list[i].Reached {
			last = &list[i]
		}
	}
	if last == nil {
		return nil
	}
	ev := &WhichEvidence{Run: last.Run, Status: last.Status, Asserted: assert.Path}
	for _, f := range last.Failures {
		if !f.Passed {
			ev.Failures = append(ev.Failures, f)
		}
	}
	if assert.Path != "" {
		if v, ok := Get(last.Response, assert.Path); ok && stringify(v) != "" {
			ev.Path, ev.Code = assert.Path, stringify(v)
		} else {
			ev.Path, ev.Code, _ = codeIn(last.Response, paths)
		}
		ev.Holds = ev.Path == assert.Path && strings.EqualFold(ev.Code, assert.Value) && (byCode || last.Status == statusPassed)
	} else {
		ev.Path, ev.Code, _ = codeIn(last.Response, paths)
		ev.Holds = last.Status == statusPassed
	}
	for i := len(order) - 1; i >= 0 && order[i] != last.Run; i-- {
		ev.NewerRuns++
	}
	return ev
}

const statusPassed = "passed"

func lastStepOf(chainName string, load func(string) []Observation) map[string]Observation {
	last := map[string]Observation{}
	if load == nil {
		return last
	}
	for _, o := range load(chainName) {
		last[o.Run] = o
	}
	return last
}

func newestUnreached(list []Observation, order []string, last map[string]Observation) *WhichNewest {
	if len(order) == 0 {
		return nil
	}
	newest := order[len(order)-1]
	for _, o := range list {
		if o.Run != newest {
			continue
		}
		if o.Reached {
			return nil
		}
		status := o.Status
		if status == "" {
			status = "not reached"
		}
		return &WhichNewest{Run: newest, Status: status}
	}
	out := &WhichNewest{Run: newest, Status: "not in run"}
	if o, ok := last[newest]; ok {
		out.StoppedAt, out.StoppedStatus = o.Step, o.Status
	}
	return out
}

func codeIn(response any, paths []string) (string, string, bool) {
	for _, p := range paths {
		v, ok := Get(response, p)
		if !ok {
			continue
		}
		text := stringify(v)
		if text == "" || (IsTransportPath(p) && (text == TransportOK || (p == TransportPrefix+".http_status" && text == "200"))) {
			continue
		}
		return p, text, true
	}
	return "", "", false
}

func DescribeFailure(r ExpectResult) string {
	out := r.Path
	if r.Rule != "" && r.Rule != "equals" {
		out += " " + r.Rule
	}
	if r.Want != nil {
		out += " want=" + scalarText(r.Want)
	}
	if r.Got != nil {
		out += " got=" + scalarText(r.Got)
	}
	if r.Detail != "" {
		out += " (" + r.Detail + ")"
	}
	return out
}

func scalarText(v any) string {
	switch v.(type) {
	case map[string]any, []any:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return stringify(v)
}

func assertsAnyCode(asserts []CodeAssertion, codes []string) bool {
	for _, code := range codes {
		if assertsCode(asserts, code) {
			return true
		}
	}
	return false
}

func assertsCode(asserts []CodeAssertion, want string) bool {
	for _, a := range asserts {
		if strings.EqualFold(a.Value, want) {
			return true
		}
	}
	return false
}

func stepCalls(s *Step, rpc string, rpcOf func(*Step) string) bool {
	want := strings.TrimPrefix(strings.TrimSpace(rpc), "/")
	if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(s.Call), "/"), want) {
		return true
	}
	if rpcOf == nil {
		return false
	}
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(rpcOf(s)), "/"), want)
}

func evidenceRank(m WhichStep, failingFirst bool) int {
	if failingFirst {
		switch {
		case m.Observed == nil:
			return 3
		case m.Observed.Status != statusPassed && m.Observed.Holds:
			return 0
		case m.Observed.Status != statusPassed:
			return 1
		default:
			return 2
		}
	}
	switch {
	case m.Observed == nil:
		return 2
	case m.Observed.Holds && m.Observed.Status == "passed":
		return 0
	case m.Observed.Holds:
		return 1
	case m.Observed.Status == statusPassed:
		return 3
	default:
		return 4
	}
}

func sortMatches(m []WhichStep, failingFirst bool) {
	sort.SliceStable(m, func(i, j int) bool {
		a, b := m[i], m[j]
		if ra, rb := evidenceRank(a, failingFirst), evidenceRank(b, failingFirst); ra != rb {
			return ra < rb
		}
		if a.SliceSteps != b.SliceSteps {
			return sliceRank(a.SliceSteps) < sliceRank(b.SliceSteps)
		}
		return a.Index < b.Index
	})
}

func sortWhich(h []WhichChain, failingFirst bool) {
	sort.SliceStable(h, func(i, j int) bool {
		a, b := h[i], h[j]
		if ra, rb := evidenceRank(a.Matches[0], failingFirst), evidenceRank(b.Matches[0], failingFirst); ra != rb {
			return ra < rb
		}
		as, bs := sliceRank(a.Matches[0].SliceSteps), sliceRank(b.Matches[0].SliceSteps)
		if as != bs {
			return as < bs
		}
		if a.Steps != b.Steps {
			return a.Steps < b.Steps
		}
		return a.Chain < b.Chain
	})
}

func sliceRank(n int) int {
	if n <= 0 {
		return 1 << 30
	}
	return n
}

type WhichUnasserted struct {
	Chain   string `json:"chain"`
	Step    string `json:"step"`
	Index   int    `json:"index"`
	Call    string `json:"call"`
	Run     string `json:"run"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	Code    string `json:"code"`
	Command string `json:"command"`
}

func WhichObservedUnasserted(chains []*Chain, q WhichQuery, opts WhichOptions) []WhichUnasserted {
	out := []WhichUnasserted{}
	if q.Code == "" || opts.Observations == nil {
		return out
	}
	for _, c := range chains {
		byStep, _ := observationsFor(c.Name, opts.Observations)
		for i, s := range c.Steps {
			if q.RPC != "" && !stepCalls(s, q.RPC, opts.RPCOf) {
				continue
			}
			if matched, _ := q.matchAsserts(assertedCodes(s)); matched {
				continue
			}
			var hit *WhichUnasserted
			for _, o := range byStep[s.ID] {
				if !o.Reached {
					continue
				}
				if path, code, ok := q.matchResponse(o.Response); ok {
					hit = &WhichUnasserted{Chain: c.Name, Step: s.ID, Index: i + 1, Call: s.Call, Run: o.Run, Status: o.Status, Path: path, Code: code}
				}
			}
			if hit == nil {
				continue
			}
			hit.Command = reproCommand(c, WhichStep{Step: s.ID, Observed: &WhichEvidence{Run: hit.Run}}, opts)
			out = append(out, *hit)
		}
	}
	return out
}

func findCode(v any, code, prefix string) (string, bool) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if IsCodePath(path) && strings.EqualFold(stringify(t[k]), code) {
				return path, true
			}
		}
		for _, k := range keys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if p, ok := findCode(t[k], code, path); ok {
				return p, true
			}
		}
	case []any:
		for i, item := range t {
			path := strconv.Itoa(i)
			if prefix != "" {
				path = prefix + "." + path
			}
			if p, ok := findCode(item, code, path); ok {
				return p, true
			}
		}
	}
	return "", false
}

func CodeAliases(code string, responses []any) []string {
	if code == "" {
		return nil
	}
	seen := map[string]bool{strings.ToLower(code): true}
	out := []string{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			names := CodeFields()
			hit := false
			for _, name := range names {
				if x, ok := t[name]; ok && strings.EqualFold(stringify(x), code) {
					hit = true
				}
			}
			if hit {
				for _, name := range names {
					x, ok := t[name]
					if !ok || x == nil {
						continue
					}
					if text := stringify(x); text != "" && !seen[strings.ToLower(text)] {
						seen[strings.ToLower(text)] = true
						out = append(out, text)
					}
				}
			}
			for _, item := range t {
				walk(item)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		}
	}
	for _, r := range responses {
		walk(r)
	}
	sort.Strings(out)
	return out
}
