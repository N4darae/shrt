package chain

import (
	"fmt"
	"sort"
	"strings"
)

const (
	SliceModeClosure = "closure"
	SliceModePin     = "pin"
)

const (
	KeepTarget   = "target"
	KeepProduces = "produces"
	KeepContract = "contract"
)

func DefaultReadOnlyPrefixes() []string {
	return []string{
		"Fetch", "Get", "List", "Preview", "Search",
		"Read", "Query", "Find", "Lookup", "Describe", "Show", "Count", "Export", "Download", "Retrieve",
	}
}

func IsReadOnlyCall(call string) bool {
	name := call
	if i := strings.LastIndex(call, "/"); i >= 0 {
		name = call[i+1:]
	}
	for _, p := range ReadOnlyPrefixes() {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

type Prereq struct {
	RPC  string `json:"rpc"`
	Edge string `json:"edge"`
}

type SliceOptions struct {
	Mode    string
	RunID   string
	Name    string
	RPCOf   func(*Step) string
	Prereqs func(rpc string) []Prereq
	Value   func(ref string) (any, bool)
}

type Keep struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Call   string `json:"call"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type Pinned struct {
	Var      string `json:"var"`
	Ref      string `json:"ref"`
	Producer string `json:"producer"`
	Value    any    `json:"value"`
}

type Unmet struct {
	Step string `json:"step"`
	RPC  string `json:"rpc"`
	Edge string `json:"edge"`
}

type Dropped struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Call  string `json:"call"`
}

type SliceResult struct {
	Source        string    `json:"source"`
	Target        string    `json:"target"`
	Mode          string    `json:"mode"`
	Run           string    `json:"run,omitempty"`
	Total         int       `json:"total"`
	Kept          []Keep    `json:"kept"`
	Pins          []Pinned  `json:"pins,omitempty"`
	Unmet         []Unmet   `json:"unmet,omitempty"`
	DroppedWrites []Dropped `json:"dropped_writes,omitempty"`
	UnderIncluded bool      `json:"under_included"`
	Chain         *Chain    `json:"-"`
}

func Slice(c *Chain, target string, opts SliceOptions) (*SliceResult, error) {
	mode := opts.Mode
	if mode == "" {
		mode = SliceModeClosure
	}
	if mode != SliceModeClosure && mode != SliceModePin {
		return nil, fmt.Errorf("unknown slice mode %q, want %q or %q", mode, SliceModeClosure, SliceModePin)
	}
	if mode == SliceModePin && opts.Value == nil {
		return nil, fmt.Errorf("slice mode %q needs a run record to pin values from", SliceModePin)
	}
	idx := newStepIndex(c)
	at, ok := idx.byID[target]
	if !ok {
		return nil, fmt.Errorf("chain %q has no step %q\nvalid step ids:\n  %s", c.Name, target, strings.Join(idx.ids(), "\n  "))
	}

	keeps := map[int]*Keep{}
	order := []int{}
	queue := []int{}
	add := func(i int, kind, reason string) {
		if _, seen := keeps[i]; seen {
			return
		}
		s := c.Steps[i]
		keeps[i] = &Keep{Index: i, ID: s.ID, Call: s.Call, Kind: kind, Reason: reason}
		order = append(order, i)
		queue = append(queue, i)
	}
	add(at, KeepTarget, KeepTarget)

	unmet := []Unmet{}
	seenUnmet := map[string]bool{}
	pinnable := func(ref string) bool {
		if mode != SliceModePin {
			return false
		}
		v, ok := opts.Value(ref)
		return ok && !valueCarriesRef(v)
	}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		s := c.Steps[i]
		for _, p := range idx.prereqsOf(s, opts) {
			j, found := idx.lastCallOf(p.RPC, i, opts)
			if !found {
				key := s.ID + "\x00" + p.RPC + "\x00" + p.Edge
				if !seenUnmet[key] {
					seenUnmet[key] = true
					unmet = append(unmet, Unmet{Step: s.ID, RPC: p.RPC, Edge: p.Edge})
				}
				continue
			}
			add(j, KeepContract, fmt.Sprintf("contract needs %s (%s)", p.RPC, p.Edge))
		}
		for _, ref := range stepRefs(s) {
			j, kind := idx.producerOf(ref, i)
			if kind != refStep {
				continue
			}
			if _, seen := keeps[j]; seen {
				continue
			}
			if pinnable(ref) {
				continue
			}
			add(j, KeepProduces, fmt.Sprintf("produces ${%s} used by %s", ref, s.ID))
		}
	}

	sort.Ints(order)
	res := &SliceResult{
		Source: c.Name,
		Target: target,
		Mode:   mode,
		Run:    opts.RunID,
		Total:  len(c.Steps),
		Kept:   make([]Keep, 0, len(order)),
	}
	for _, i := range order {
		res.Kept = append(res.Kept, *keeps[i])
	}
	res.Unmet = unmet

	pins := map[string]string{}
	usedVars := map[string]bool{}
	taken := map[string]bool{}
	for name := range c.Vars {
		taken[name] = true
	}
	for _, i := range order {
		s := c.Steps[i]
		for _, ref := range stepRefs(s) {
			j, kind := idx.producerOf(ref, i)
			switch kind {
			case refVar:
				usedVars[varNameOf(ref)] = true
			case refStep:
				if _, seen := keeps[j]; seen {
					continue
				}
				if _, done := pins[ref]; done || opts.Value == nil {
					continue
				}
				v, ok := opts.Value(ref)
				if !ok {
					continue
				}
				name := uniqueVarName(pinVarName(ref), taken)
				taken[name] = true
				usedVars[name] = true
				pins[ref] = name
				res.Pins = append(res.Pins, Pinned{Var: name, Ref: ref, Producer: c.Steps[j].ID, Value: v})
			}
		}
	}
	sort.Slice(res.Pins, func(a, b int) bool { return res.Pins[a].Var < res.Pins[b].Var })

	for i, s := range c.Steps {
		if _, seen := keeps[i]; seen {
			continue
		}
		if isWriteCall(s.Call) {
			res.DroppedWrites = append(res.DroppedWrites, Dropped{Index: i, ID: s.ID, Call: s.Call})
		}
	}
	res.UnderIncluded = len(res.DroppedWrites) > 0

	name := opts.Name
	if name == "" {
		name = DefaultSliceName(c.Name, target)
	}
	out := &Chain{
		APIVersion:  APIVersion,
		Name:        name,
		Description: sliceDescription(c, target, mode, opts.RunID, len(order), len(c.Steps), res),
		Volatile:    append([]string{}, c.Volatile...),
		Redact:      append([]string{}, c.Redact...),
	}
	for _, i := range order {
		out.Steps = append(out.Steps, rewriteStep(c.Steps[i], pins))
	}
	vars := map[string]any{}
	for name, v := range c.Vars {
		if usedVars[name] {
			vars[name] = v
		}
	}
	for _, p := range res.Pins {
		vars[p.Var] = p.Value
	}
	if len(vars) > 0 {
		out.Vars = vars
	}
	res.Chain = out
	return res, nil
}

func listSome(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:max], ", "), len(names)-max)
}

func DefaultSliceName(chainName, target string) string {
	return chainName + "-slice-" + target
}

func sliceDescription(c *Chain, target, mode, runID string, kept, total int, res *SliceResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Slice of %s reproducing step %s: %d of %d steps, mode %s", c.Name, target, kept, total, mode)
	if mode == SliceModePin {
		fmt.Fprintf(&b, ", values pinned from run %s", runID)
	}
	b.WriteString(".\n\n")
	b.WriteString("Computed by 'shrt chain slice': the target step, every earlier step whose output a kept\n")
	b.WriteString("step references, and every ordering prerequisite the contracts declare for a kept rpc.\n")
	if len(res.Pins) > 0 {
		fmt.Fprintf(&b, "%d value(s) that earlier steps produced are pinned into vars, so their producers are gone.\n", len(res.Pins))
	}
	b.WriteString("\nThis slice is a HYPOTHESIS until it is run. A dependency that is state rather than a\n")
	b.WriteString("reference leaves no trace in the YAML, so a slice can be too small and still go green.\n")
	if len(res.DroppedWrites) > 0 {
		names := make([]string, 0, len(res.DroppedWrites))
		for _, d := range res.DroppedWrites {
			names = append(names, d.ID)
		}
		fmt.Fprintf(&b, "\n%d dropped step(s) WRITE: %s.\n", len(names), listSome(names, 8))
	}
	if len(res.Unmet) > 0 {
		names := []string{}
		seen := map[string]bool{}
		for _, u := range res.Unmet {
			if seen[u.RPC] {
				continue
			}
			seen[u.RPC] = true
			names = append(names, u.RPC)
		}
		fmt.Fprintf(&b, "\nUnmet contract prerequisite(s), no earlier step calls them: %s.\n", listSome(names, 8))
	}
	return b.String()
}

const (
	refNone = iota
	refVar
	refStep
)

type stepIndex struct {
	c       *Chain
	byID    map[string]int
	exports map[string][]int
	rpc     map[int]string
}

func newStepIndex(c *Chain) *stepIndex {
	x := &stepIndex{c: c, byID: map[string]int{}, exports: map[string][]int{}, rpc: map[int]string{}}
	for i, s := range c.Steps {
		x.byID[s.ID] = i
		for name := range s.Export {
			x.exports[name] = append(x.exports[name], i)
		}
	}
	for name := range x.exports {
		sort.Ints(x.exports[name])
	}
	return x
}

func (x *stepIndex) ids() []string {
	out := make([]string, 0, len(x.c.Steps))
	for _, s := range x.c.Steps {
		out = append(out, s.ID)
	}
	return out
}

func (x *stepIndex) rpcOf(i int, opts SliceOptions) string {
	if name, ok := x.rpc[i]; ok {
		return name
	}
	name := ""
	if opts.RPCOf != nil {
		name = opts.RPCOf(x.c.Steps[i])
	}
	if name == "" {
		name = x.c.Steps[i].Call
	}
	x.rpc[i] = name
	return name
}

func (x *stepIndex) prereqsOf(s *Step, opts SliceOptions) []Prereq {
	if opts.Prereqs == nil {
		return nil
	}
	i, ok := x.byID[s.ID]
	if !ok {
		return nil
	}
	return opts.Prereqs(x.rpcOf(i, opts))
}

func (x *stepIndex) lastCallOf(rpc string, before int, opts SliceOptions) (int, bool) {
	for i := before - 1; i >= 0; i-- {
		if x.rpcOf(i, opts) == rpc {
			return i, true
		}
	}
	return 0, false
}

func (x *stepIndex) exporter(name string, before int) (int, bool) {
	best, found := 0, false
	for _, i := range x.exports[name] {
		if i < before {
			best, found = i, true
		}
	}
	return best, found
}

func (x *stepIndex) producerOf(ref string, at int) (int, int) {
	head, rest, hasRest := strings.Cut(strings.TrimSpace(ref), ".")
	switch head {
	case "uuid", "now", "nowunix", "env":
		return 0, refNone
	case "vars":
		if rest == "" {
			return 0, refNone
		}
		return 0, refVar
	case "exports":
		name, _, _ := strings.Cut(rest, ".")
		if name == "" {
			return 0, refNone
		}
		if i, ok := x.exporter(name, at); ok {
			return i, refStep
		}
		return 0, refNone
	case "steps":
		id, _, _ := strings.Cut(rest, ".")
		if i, ok := x.byID[id]; ok && i < at {
			return i, refStep
		}
		return 0, refNone
	}
	if !hasRest {
		if i, ok := x.exporter(head, at); ok {
			return i, refStep
		}
	}
	if i, ok := x.byID[head]; ok && i < at {
		return i, refStep
	}
	return 0, refNone
}

func varNameOf(ref string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(ref), ".")
	name, _, _ := strings.Cut(rest, ".")
	return name
}

func stepRefs(s *Step) []string {
	seen := map[string]bool{}
	out := []string{}
	collect := func(refs []string) {
		for _, r := range refs {
			if seen[r] {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	collect(collectRefs(s.Body))
	collect(collectRefs(headerValues(s.Headers)))
	for _, e := range s.Expect {
		collect(collectRefs([]any{e.Equals, e.NotEqual, e.Contains}))
	}
	sort.Strings(out)
	return out
}

func isWriteCall(call string) bool {
	return !IsReadOnlyCall(call)
}

func valueCarriesRef(v any) bool {
	s, ok := v.(string)
	return ok && refPattern.MatchString(s)
}

func pinVarName(ref string) string {
	var b strings.Builder
	b.WriteString("pin_")
	prev := false
	for _, r := range strings.TrimSpace(ref) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prev = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
			prev = false
		default:
			if !prev {
				b.WriteByte('_')
				prev = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func uniqueVarName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s_%d", base, i)
		if !taken[name] {
			return name
		}
	}
}

func rewriteStep(s *Step, pins map[string]string) *Step {
	out := *s
	out.Body, _ = rewriteValue(s.Body, pins).(map[string]any)
	if len(s.Headers) > 0 {
		headers := make(map[string]string, len(s.Headers))
		for k, v := range s.Headers {
			headers[k] = rewriteString(v, pins)
		}
		out.Headers = headers
	}
	if len(s.Expect) > 0 {
		expect := make([]Expectation, 0, len(s.Expect))
		for _, e := range s.Expect {
			copied := e
			copied.Equals = rewriteValue(e.Equals, pins)
			copied.NotEqual = rewriteValue(e.NotEqual, pins)
			copied.Contains = rewriteString(e.Contains, pins)
			expect = append(expect, copied)
		}
		out.Expect = expect
	}
	if len(s.Export) > 0 {
		export := make(map[string]string, len(s.Export))
		for k, v := range s.Export {
			export[k] = v
		}
		out.Export = export
	}
	if len(s.Volatile) > 0 {
		out.Volatile = append([]string{}, s.Volatile...)
	}
	return &out
}

func rewriteValue(v any, pins map[string]string) any {
	switch t := v.(type) {
	case string:
		return rewriteString(t, pins)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = rewriteValue(item, pins)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, rewriteValue(item, pins))
		}
		return out
	default:
		return v
	}
}

func rewriteString(in string, pins map[string]string) string {
	if in == "" || len(pins) == 0 {
		return in
	}
	return refPattern.ReplaceAllStringFunc(in, func(m string) string {
		ref := strings.TrimSpace(m[2 : len(m)-1])
		if name, ok := pins[ref]; ok {
			return "${vars." + name + "}"
		}
		return m
	})
}

type Verdict struct {
	Step      string         `json:"step"`
	Status    string         `json:"status"`
	ErrorCode string         `json:"error_code"`
	Expect    []ExpectResult `json:"expect"`
}

func CompareVerdicts(source, replay Verdict) []string {
	diffs := []string{}
	if source.ErrorCode != replay.ErrorCode {
		diffs = append(diffs, fmt.Sprintf("%s: source %q, slice %q", EnvelopePath(), source.ErrorCode, replay.ErrorCode))
	}
	if source.Status != replay.Status {
		diffs = append(diffs, fmt.Sprintf("step status: source %q, slice %q", source.Status, replay.Status))
	}
	if len(source.Expect) != len(replay.Expect) {
		diffs = append(diffs, fmt.Sprintf("expectation count: source %d, slice %d", len(source.Expect), len(replay.Expect)))
		return diffs
	}
	for i, want := range source.Expect {
		got := replay.Expect[i]
		if want.Path != got.Path || want.Rule != got.Rule {
			diffs = append(diffs, fmt.Sprintf("expectation %d: source %s %s, slice %s %s", i, want.Path, want.Rule, got.Path, got.Rule))
			continue
		}
		if want.Passed != got.Passed {
			diffs = append(diffs, fmt.Sprintf("expectation %d (%s %s): source passed=%t, slice passed=%t", i, want.Path, want.Rule, want.Passed, got.Passed))
		}
	}
	return diffs
}
