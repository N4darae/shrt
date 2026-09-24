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
	RPC  string
	Code string
}

type WhichOptions struct {
	RPCOf        func(*Step) string
	SliceOf      func(*Chain, string) (int, bool)
	Observations func(chainName string) []Observation
	FreshVars    func(c *Chain, step, run string) []string
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
	Run    string `json:"run"`
	Status string `json:"status"`
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
}

func IsCodePath(path string) bool {
	if path == TransportPrefix+".code" {
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
			if q.Code != "" {
				if !assertsCode(asserts, q.Code) {
					continue
				}
				why = append(why, WhyCode)
			}
			matches = append(matches, WhichStep{
				Step: s.ID, Index: i + 1, Call: s.Call, Why: why, Asserts: asserts,
			})
		}
		if len(matches) == 0 {
			continue
		}
		hit := WhichChain{Chain: c.Name, Source: c.SourcePath, Steps: len(c.Steps)}
		byStep, order := observationsFor(c.Name, opts.Observations)
		hit.Runs = len(order)
		for i := range matches {
			assert, _ := PrimaryAssertion(matches[i].Asserts, q.Code)
			matches[i].Observed = evidenceFor(byStep[matches[i].Step], order, paths, assert, q.Code != "")
			matches[i].Newest = newestUnreached(byStep[matches[i].Step], order)
			if matches[i].Observed != nil {
				hit.Observed = true
			}
			if opts.SliceOf != nil {
				if n, ok := opts.SliceOf(c, matches[i].Step); ok {
					matches[i].SliceSteps = n
				}
			}
		}
		sortMatches(matches)
		hit.Matches = matches
		hit.Best = matches[0].Step
		hit.Command = reproCommand(c, matches[0], opts.FreshVars)
		out = append(out, hit)
	}
	sortWhich(out)
	return out
}

func reproCommand(c *Chain, best WhichStep, fresh func(*Chain, string, string) []string) string {
	cmd := "shrt chain slice " + c.Name + " -step " + best.Step
	run := ""
	if best.Observed != nil {
		run = best.Observed.Run
		cmd += " -mode pin -run " + run
	}
	if fresh != nil {
		names := append([]string{}, fresh(c, best.Step, run)...)
		sort.Strings(names)
		for _, name := range names {
			cmd += " -var " + name + "=<fresh>"
		}
	}
	return cmd
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

func newestUnreached(list []Observation, order []string) *WhichNewest {
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
	return &WhichNewest{Run: newest, Status: "not in run"}
}

func codeIn(response any, paths []string) (string, string, bool) {
	for _, p := range paths {
		v, ok := Get(response, p)
		if !ok {
			continue
		}
		text := stringify(v)
		if text == "" || (IsTransportPath(p) && text == TransportOK) {
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

func evidenceRank(m WhichStep) int {
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

func sortMatches(m []WhichStep) {
	sort.SliceStable(m, func(i, j int) bool {
		a, b := m[i], m[j]
		if ra, rb := evidenceRank(a), evidenceRank(b); ra != rb {
			return ra < rb
		}
		if a.SliceSteps != b.SliceSteps {
			return sliceRank(a.SliceSteps) < sliceRank(b.SliceSteps)
		}
		return a.Index < b.Index
	})
}

func sortWhich(h []WhichChain) {
	sort.SliceStable(h, func(i, j int) bool {
		a, b := h[i], h[j]
		if ra, rb := evidenceRank(a.Matches[0]), evidenceRank(b.Matches[0]); ra != rb {
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
			if assertsCode(assertedCodes(s), q.Code) {
				continue
			}
			var hit *WhichUnasserted
			for _, o := range byStep[s.ID] {
				if !o.Reached {
					continue
				}
				path, ok := findCode(o.Response, q.Code, "")
				if !ok {
					continue
				}
				hit = &WhichUnasserted{Chain: c.Name, Step: s.ID, Index: i + 1, Call: s.Call, Run: o.Run, Status: o.Status, Path: path, Code: q.Code}
			}
			if hit == nil {
				continue
			}
			hit.Command = "shrt chain slice " + c.Name + " -step " + s.ID + " -mode pin -run " + hit.Run
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
