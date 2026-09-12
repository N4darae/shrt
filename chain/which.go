package chain

import (
	"sort"
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
}

type WhichQuery struct {
	RPC  string
	Code string
}

type WhichOptions struct {
	RPCOf        func(*Step) string
	SliceOf      func(*Chain, string) (int, bool)
	Observations func(chainName string) []Observation
}

type WhichEvidence struct {
	Run    string `json:"run"`
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
	Path   string `json:"path,omitempty"`
}

type WhichStep struct {
	Step       string          `json:"step"`
	Index      int             `json:"index"`
	Call       string          `json:"call"`
	Why        []string        `json:"why"`
	Asserts    []CodeAssertion `json:"asserts,omitempty"`
	SliceSteps int             `json:"slice_steps,omitempty"`
	Observed   *WhichEvidence  `json:"observed,omitempty"`
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
		byStep, runs := observationsFor(c.Name, opts.Observations)
		hit.Runs = runs
		for i := range matches {
			matches[i].Observed = evidenceFor(byStep[matches[i].Step], paths, q.Code)
			if opts.SliceOf != nil {
				if n, ok := opts.SliceOf(c, matches[i].Step); ok {
					matches[i].SliceSteps = n
				}
			}
		}
		sortMatches(matches)
		hit.Matches = matches
		hit.Best = matches[0].Step
		hit.Observed = matches[0].Observed != nil
		hit.Command = reproCommand(hit.Chain, matches[0])
		out = append(out, hit)
	}
	sortWhich(out)
	return out
}

func reproCommand(chainName string, best WhichStep) string {
	cmd := "shrt chain slice " + chainName + " -step " + best.Step
	if best.Observed != nil {
		cmd += " -mode pin -run " + best.Observed.Run
	}
	return cmd
}

func observationsFor(chainName string, load func(string) []Observation) (map[string][]Observation, int) {
	byStep := map[string][]Observation{}
	if load == nil {
		return byStep, 0
	}
	runs := map[string]bool{}
	for _, o := range load(chainName) {
		byStep[o.Step] = append(byStep[o.Step], o)
		runs[o.Run] = true
	}
	return byStep, len(runs)
}

func evidenceFor(list []Observation, paths []string, want string) *WhichEvidence {
	var found *WhichEvidence
	for _, o := range list {
		if !o.Reached {
			continue
		}
		path, value, ok := codeIn(o.Response, paths, want)
		if want != "" && !ok {
			continue
		}
		found = &WhichEvidence{Run: o.Run, Status: o.Status, Code: value, Path: path}
	}
	return found
}

func codeIn(response any, paths []string, want string) (string, string, bool) {
	firstPath, firstValue := "", ""
	for _, p := range paths {
		v, ok := Get(response, p)
		if !ok {
			continue
		}
		text := stringify(v)
		if text == "" {
			continue
		}
		if want != "" && strings.EqualFold(text, want) {
			return p, text, true
		}
		if firstValue == "" {
			firstPath, firstValue = p, text
		}
	}
	if want != "" {
		return "", "", false
	}
	return firstPath, firstValue, firstValue != ""
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

func sortMatches(m []WhichStep) {
	sort.SliceStable(m, func(i, j int) bool {
		a, b := m[i], m[j]
		if (a.Observed != nil) != (b.Observed != nil) {
			return a.Observed != nil
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
		if a.Observed != b.Observed {
			return a.Observed
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
