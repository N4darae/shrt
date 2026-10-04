package chain

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	KeepTarget   = "target"
	KeepProduces = "produces"
	KeepContract = "contract"
	KeepAsked    = "requested"

	KeepSideEffect = "side_effect"
	KeepCheckpoint = "checkpoint"
)

const SliceKeepWrites = "writes"

const RefusedNotSent = "not sent"

func DefaultReadOnlyPrefixes() []string {
	return []string{
		"Fetch", "Get", "List", "Preview", "Search",
		"Read", "Query", "Find", "Lookup", "Describe", "Show", "Count", "Export", "Download", "Retrieve", "Watch", "Subscribe",
	}
}

func IsReadOnlyCall(call string) bool { return callHasPrefix(call, ReadOnlyPrefixes()) }

func callHasPrefix(call string, prefixes []string) bool {
	name := call[strings.LastIndex(call, "/")+1:]
	return slices.ContainsFunc(prefixes, func(p string) bool {
		return strings.HasPrefix(name, p) && (len(name) == len(p) || name[len(p)] < 'a' || name[len(p)] > 'z')
	})
}

type Prereq struct {
	RPC   string   `json:"rpc"`
	Alias string   `json:"alias,omitempty"`
	Edge  string   `json:"edge"`
	For   string   `json:"for,omitempty"`
	Field string   `json:"field,omitempty"`
	Via   []string `json:"via,omitempty"`
}

func (p Prereq) calledBy(rpc string) bool {
	if rpc == p.RPC {
		return true
	}
	for _, v := range p.Via {
		if v == rpc {
			return true
		}
	}
	return false
}

func (p Prereq) Node() string {
	if p.Alias == "" {
		return p.RPC
	}
	return p.RPC + "@" + p.Alias
}

type SliceOptions struct {
	RunID   string
	Name    string
	RPCOf   func(*Step) string
	Prereqs func(rpc string) []Prereq
	Keep    []string
	Pinned  []string
	Vars    map[string]any
	RunVars map[string]any
	Refused func(stepID string) (string, bool)

	RunVarsAsDefaults bool
	Checkpoints       map[string]string
	IsLogin           func(*Step) bool
	Relax             func(stepID string) []ExpectResult
	StateIrrelevant   func(writerID, readerID string) bool
	AssertsWrite      func(writerID, readerID string) bool
	KeyField          func(rpc, field string) (key, known bool)
}

type Keep struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Call   string `json:"call"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type Unmet struct {
	Step string `json:"step"`
	RPC  string `json:"rpc"`
	Edge string `json:"edge"`
}

type Dropped struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Call   string `json:"call"`
	Reason string `json:"refused,omitempty"`
}

const (
	VarFromFlag = "-var"
	VarFromRun  = "run"
)

type Relaxed struct {
	Step string `json:"step"`
	Path string `json:"path"`
	Rule string `json:"rule"`
	Want any    `json:"want,omitempty"`
	Got  any    `json:"got,omitempty"`
}

type FilledVar struct {
	Var      string `json:"var"`
	Value    any    `json:"value"`
	From     string `json:"from"`
	Declared bool   `json:"declared,omitempty"`
	Default  any    `json:"default,omitempty"`
}

type SliceResult struct {
	Source        string          `json:"source"`
	Target        string          `json:"target"`
	Run           string          `json:"run,omitempty"`
	Total         int             `json:"total"`
	Reach         int             `json:"reach"`
	Kept          []Keep          `json:"kept"`
	Unmet         []Unmet         `json:"unmet,omitempty"`
	DroppedWrites []Dropped       `json:"dropped_writes,omitempty"`
	RefusedWrites []Dropped       `json:"dropped_refused_writes,omitempty"`
	UnderIncluded bool            `json:"under_included"`
	FilledVars    []FilledVar     `json:"filled_vars,omitempty"`
	MissingVars   []string        `json:"missing_vars,omitempty"`
	FreshVars     []string        `json:"fresh_vars,omitempty"`
	CarriedPins   []Pin           `json:"carried_kept_red,omitempty"`
	DroppedPins   []Pin           `json:"dropped_kept_red,omitempty"`
	Relaxed       []Relaxed       `json:"relaxed,omitempty"`
	Verified      string          `json:"verified,omitempty"`
	NotReproduced string          `json:"not_reproduced,omitempty"`
	Inconclusive  string          `json:"inconclusive,omitempty"`
	Intermittent  string          `json:"intermittent,omitempty"`
	Build         string          `json:"verify_build,omitempty"`
	SourceRef     string          `json:"-"`
	Chain         *Chain          `json:"-"`
	StateWriters  map[string]bool `json:"-"`
}

func Slice(c *Chain, target string, opts SliceOptions) (*SliceResult, error) {
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
		keeps[i] = &Keep{Index: i + 1, ID: s.ID, Call: s.Call, Kind: kind, Reason: reason}
		order = append(order, i)
		queue = append(queue, i)
	}
	add(at, KeepTarget, KeepTarget)
	for _, id := range opts.Keep {
		j, ok := idx.byID[id]
		if !ok && id == SliceKeepWrites {
			for w, s := range c.Steps[:at] {
				if !isWriteCall(s.Call) || (opts.IsLogin != nil && opts.IsLogin(s)) {
					continue
				}
				add(w, KeepAsked, "kept by -keep writes")
			}
			continue
		}
		if !ok {
			return nil, fmt.Errorf("chain %q has no step %q to keep (-keep %s keeps every earlier write step)\nvalid step ids:\n  %s", c.Name, id, SliceKeepWrites, strings.Join(idx.ids(), "\n  "))
		}
		if j > at && !containsID(opts.Pinned, id) {
			return nil, fmt.Errorf("step %q runs after the target %q, so keeping it cannot change the target's verdict", id, target)
		}
		add(j, KeepAsked, KeepAsked)
	}
	for _, id := range sortedKeys(opts.Checkpoints) {
		if j, ok := idx.byID[id]; ok {
			add(j, KeepCheckpoint, opts.Checkpoints[id])
		}
	}

	unmet := []Unmet{}
	seenUnmet := map[string]bool{}
	boundAlias := map[int]string{}
	for {
		for len(queue) > 0 {
			i := queue[0]
			queue = queue[1:]
			s := c.Steps[i]
			referenced := map[int]bool{}
			for _, ref := range stepRefs(s) {
				j, kind := idx.producerOf(ref, i)
				if kind != refStep {
					continue
				}
				referenced[j] = true
				add(j, KeepProduces, fmt.Sprintf("produces ${%s} used by %s", ref, s.ID))
			}
			for _, p := range idx.prereqsOf(s, opts) {
				if p.For != "" && !carriesAlias(s.ID, p.For) {
					continue
				}
				if valueEdge(p.Edge) && idx.fieldNeedsNoProducer(s, i, p.Field) {
					continue
				}
				if !valueEdge(p.Edge) && p.Alias == "" {
					if calls := idx.callsSharingProducers(p, i, idx.reach(i), opts); len(calls) > 0 {
						for _, j := range calls {
							add(j, KeepContract, fmt.Sprintf("contract needs %s (%s)", p.Node(), p.Edge))
						}
						continue
					}
				}
				j, found := idx.lastCallOf(p, i, referenced, opts)
				if found && p.Alias != "" && !carriesAlias(c.Steps[j].ID, p.Alias) {
					if other, bound := boundAlias[j]; bound && other != p.Alias {
						found = false
					} else {
						boundAlias[j] = p.Alias
					}
				}
				if !found {
					key := s.ID + "\x00" + p.Node() + "\x00" + p.Edge
					if !seenUnmet[key] {
						seenUnmet[key] = true
						unmet = append(unmet, Unmet{Step: s.ID, RPC: p.Node(), Edge: p.Edge})
					}
					continue
				}
				add(j, KeepContract, fmt.Sprintf("contract needs %s (%s)", p.Node(), p.Edge))
			}
		}
		added := false
		for _, w := range idx.sideEffectWrites(at, keeps, opts) {
			add(w.index, KeepSideEffect, w.reason)
			added = true
		}
		for _, w := range idx.stateWrites(at, keeps, opts) {
			add(w.index, KeepSideEffect, w.reason)
			added = true
		}
		for _, w := range idx.sameValueWrites(at, keeps, opts) {
			add(w.index, KeepSideEffect, w.reason)
			added = true
		}
		if !added {
			break
		}
	}

	sort.Ints(order)
	res := &SliceResult{
		Source: c.Name,
		Target: target,
		Run:    opts.RunID,
		Total:  len(c.Steps),
		Reach:  at + 1,
		Kept:   make([]Keep, 0, len(order)),
	}
	for _, i := range order {
		res.Kept = append(res.Kept, *keeps[i])
	}
	res.StateWriters = map[string]bool{}
	for i, s := range c.Steps[:at] {
		for _, p := range idx.prereqsOf(s, opts) {
			if !valueEdge(p.Edge) && isWriteCall(p.RPC) && isWriteCall(s.Call) && p.RPC != idx.rpcOf(i, opts) {
				res.StateWriters[s.ID] = true
			}
		}
	}
	res.Unmet = unmet
	usedVars := map[string]bool{}
	for _, i := range order {
		for _, ref := range stepRefs(c.Steps[i]) {
			if _, kind := idx.producerOf(ref, i); kind == refVar {
				usedVars[varNameOf(ref)] = true
			}
		}
	}

	for i, s := range c.Steps[:at] {
		if _, seen := keeps[i]; seen {
			continue
		}
		if !isWriteCall(s.Call) {
			continue
		}
		if opts.IsLogin != nil && opts.IsLogin(s) {
			continue
		}
		d := Dropped{Index: i + 1, ID: s.ID, Call: s.Call}
		if opts.Refused != nil {
			if why, refused := opts.Refused(s.ID); refused {
				d.Reason = why
				res.RefusedWrites = append(res.RefusedWrites, d)
				continue
			}
		}
		res.DroppedWrites = append(res.DroppedWrites, d)
	}
	res.UnderIncluded = len(res.DroppedWrites) > 0

	name := opts.Name
	if name == "" {
		name = DefaultSliceName(c.Name, target)
	}
	out := &Chain{
		APIVersion:  APIVersion,
		Name:        name,
		Description: "",
		Volatile:    append([]string{}, c.Volatile...),
		Redact:      append([]string{}, c.Redact...),
	}
	kept := map[string]bool{}
	for _, i := range order {
		st := copyStep(c.Steps[i])
		if st.ID != target && opts.Relax != nil {
			res.Relaxed = append(res.Relaxed, relaxStep(st, opts.Relax(st.ID))...)
		}
		out.Steps = append(out.Steps, st)
		kept[c.Steps[i].ID] = true
	}
	for _, k := range c.KeptRed {
		if kept[k.Step] {
			out.KeptRed = append(out.KeptRed, k)
			res.CarriedPins = append(res.CarriedPins, k)
		} else {
			res.DroppedPins = append(res.DroppedPins, k)
		}
	}
	res.FreshVars = append([]string{}, FreshVars(out.Steps, opts.IsLogin, nil)...)
	fresh := map[string]bool{}
	for _, name := range res.FreshVars {
		fresh[name] = true
	}
	vars := map[string]any{}
	declared := []string{}
	for name, v := range c.Vars {
		if usedVars[name] {
			vars[name] = v
			declared = append(declared, name)
		}
	}
	sort.Strings(declared)
	for _, name := range declared {
		if fresh[name] {
			if v, ok := opts.Vars[name]; ok && fmt.Sprint(v) != fmt.Sprint(c.Vars[name]) {
				vars[name] = v
				res.FilledVars = append(res.FilledVars, FilledVar{Var: name, Value: v, From: VarFromFlag, Declared: true, Default: c.Vars[name]})
			}
			continue
		}
		if !opts.RunVarsAsDefaults {
			continue
		}
		v, ok := opts.RunVars[name]
		if !ok || fmt.Sprint(v) == fmt.Sprint(c.Vars[name]) {
			continue
		}
		vars[name] = v
		res.FilledVars = append(res.FilledVars, FilledVar{Var: name, Value: v, From: VarFromRun, Declared: true, Default: c.Vars[name]})
	}
	undeclared := []string{}
	for name := range usedVars {
		if _, declared := c.Vars[name]; !declared {
			if _, set := vars[name]; !set {
				undeclared = append(undeclared, name)
			}
		}
	}
	sort.Strings(undeclared)
	for _, name := range undeclared {
		if v, ok := opts.Vars[name]; ok {
			vars[name] = v
			res.FilledVars = append(res.FilledVars, FilledVar{Var: name, Value: v, From: VarFromFlag})
			continue
		}
		if name == RunTagVar {
			continue
		}
		res.MissingVars = append(res.MissingVars, name)
	}
	if _, declared := c.Vars[RunTagVar]; !declared && fresh[RunTagVar] {
		if _, given := opts.Vars[RunTagVar]; !given {
			kept := []string{}
			for _, name := range res.FreshVars {
				if name != RunTagVar {
					kept = append(kept, name)
				}
			}
			res.FreshVars = kept
		}
	}
	if len(vars) > 0 {
		out.Vars = vars
	}
	res.Chain = out
	out.Description = sliceDescription(res)
	return res, nil
}

func (r *SliceResult) MarkReproduced(sourceRun, sliceRun string, at time.Time) {
	r.Verified = fmt.Sprintf("reproduced on %s: slice run %s%s gave step %s the verdict it had in source run %s",
		at.UTC().Format("2006-01-02"), sliceRun, r.onBuild(), r.Target, sourceRun)
	r.NotReproduced, r.Inconclusive, r.Intermittent = "", "", ""
	if r.Chain != nil {
		r.Chain.Description = sliceDescription(r)
	}
}

func (r *SliceResult) MarkNotReproduced(sourceRun, sliceRun string, at time.Time, difference string) {
	r.NotReproduced = fmt.Sprintf("on %s slice run %s%s did not give step %s the verdict it had in source run %s",
		at.UTC().Format("2006-01-02"), sliceRun, r.onBuild(), r.Target, sourceRun)
	if difference != "" {
		r.NotReproduced += " (" + difference + ")"
	}
	r.Verified, r.Inconclusive, r.Intermittent = "", "", ""
	if r.Chain != nil {
		r.Chain.Description = sliceDescription(r)
	}
}

func (r *SliceResult) MarkInconclusive(sourceRun, sliceRun string, at time.Time, why string) {
	r.Inconclusive = fmt.Sprintf("on %s slice run %s%s gave step %s a verdict that does not settle whether it reproduces source run %s",
		at.UTC().Format("2006-01-02"), sliceRun, r.onBuild(), r.Target, sourceRun)
	if why != "" {
		r.Inconclusive += " (" + why + ")"
	}
	r.Verified, r.NotReproduced, r.Intermittent = "", "", ""
	if r.Chain != nil {
		r.Chain.Description = sliceDescription(r)
	}
}

func (r *SliceResult) MarkIntermittent(sourceRun, sliceRuns string, reproduced, runs int, at time.Time) {
	r.Intermittent = fmt.Sprintf("on %s slice runs %s%s gave step %s the verdict it had in source run %s in %d of %d runs",
		at.UTC().Format("2006-01-02"), sliceRuns, r.onBuild(), r.Target, sourceRun, reproduced, runs)
	r.Verified, r.NotReproduced, r.Inconclusive = "", "", ""
	if r.Chain != nil {
		r.Chain.Description = sliceDescription(r)
	}
}

func (r *SliceResult) OwnRunOutcome(outcome, chainName, sourceRun, sliceRun string, at time.Time, why string) string {
	gave := "another verdict than"
	switch {
	case outcome == "INCONCLUSIVE":
		gave = "a verdict that does not settle it against"
	case strings.HasPrefix(outcome, "INTERMITTENT"):
		gave = "in only some runs the verdict of"
	}
	out := fmt.Sprintf("%s on %s: run %s%s of %s gave step %s %s %s's own run %s",
		outcome, at.UTC().Format("2006-01-02"), sliceRun, r.onBuild(), chainName, r.Target, gave, chainName, sourceRun)
	if why != "" {
		out += " (" + why + ")"
	}
	return out
}

func (r *SliceResult) OwnRunVerdict(chainName, sourceRun, sliceRun string, at time.Time) string {
	return fmt.Sprintf("reproduced on %s: run %s%s of %s gave step %s the verdict of %s's own run %s. The slice keeps every step, "+
		"so this re-ran the chain, it did not reproduce a failure of another chain",
		at.UTC().Format("2006-01-02"), sliceRun, r.onBuild(), chainName, r.Target, chainName, sourceRun)
}

func (r *SliceResult) onBuild() string {
	if r.Build == "" {
		return ""
	}
	return " on build " + r.Build
}

const (
	hypothesisParagraph = "\nThis slice is a HYPOTHESIS until it is run. A dependency that is state rather than a\n" +
		"reference leaves no trace in the YAML, so a slice can be too small and still go green.\n"
	hypothesisPrefix    = "HYPOTHESIS: not verified"
	verifiedPrefix      = "VERIFIED by 'shrt chain slice -verify': "
	notReproducedPrefix = "NOT REPRODUCED by 'shrt chain slice -verify': "
	rerunPrefix         = "RE-RUN by 'shrt chain slice -verify': "
	inconclusivePrefix  = "INCONCLUSIVE by 'shrt chain slice -verify': "
	intermittentPrefix  = "INTERMITTENT by 'shrt chain slice -verify': "
)

func verifiedLine(verified string) string {
	return verifiedPrefix + verified + ".\n"
}

func HasVerifiedVerdict(description string) bool {
	return strings.Contains(description, verifiedPrefix)
}

func IsSliceDescription(description string) bool {
	head, _, _ := strings.Cut(description, "\n")
	return strings.HasPrefix(head, "Slice of ") && strings.Contains(head, " reproducing step ")
}

func RecordVerified(description, verified string) string {
	return replaceVerdict(description, verifiedLine(verified), verifiedPrefix, notReproducedPrefix, inconclusivePrefix, intermittentPrefix)
}

func RecordRerun(description, verdict string) string {
	return replaceVerdict(description, rerunPrefix+verdict+".\n", rerunPrefix)
}

func replaceVerdict(description, line string, prefixes ...string) string {
	description = strings.Replace(description, hypothesisParagraph, "\n\x00", 1)
	kept := []string{}
	for _, l := range strings.SplitAfter(description, "\n") {
		verdict := strings.HasPrefix(l, hypothesisPrefix)
		for _, p := range prefixes {
			if strings.HasPrefix(l, p) {
				verdict = true
			}
		}
		if !verdict {
			kept = append(kept, l)
			continue
		}
		if !strings.Contains(strings.Join(kept, ""), "\x00") {
			kept = append(kept, "\x00")
		}
	}
	description = strings.Join(kept, "")
	if !strings.Contains(description, "\x00") {
		description = strings.TrimRight(description, "\n")
		if description == "" {
			return line
		}
		return description + "\n\n" + line
	}
	description = strings.Replace(description, "\x00", line, 1)
	for strings.Contains(description, "\n\n\n") {
		description = strings.ReplaceAll(description, "\n\n\n", "\n\n")
	}
	return strings.TrimRight(description, "\n") + "\n"
}

func listSome(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:max], ", "), len(names)-max)
}

func (r *SliceResult) SourceCommandRef() string {
	if r.SourceRef != "" {
		return r.SourceRef
	}
	return r.Source
}

func DefaultSliceName(chainName, target string) string {
	return chainName + "-slice-" + target
}

func SliceDescriptionPrefix(source, target string) string {
	return fmt.Sprintf("Slice of %s reproducing step %s:", source, target)
}

func sliceDescription(res *SliceResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d of %d steps.\n", SliceDescriptionPrefix(res.Source, res.Target), len(res.Kept), res.Total)
	switch {
	case res.Verified != "":
		b.WriteString(verifiedLine(res.Verified))
	case res.NotReproduced != "":
		b.WriteString(notReproducedPrefix + res.NotReproduced + ".\n")
	case res.Intermittent != "":
		b.WriteString(intermittentPrefix + res.Intermittent + ".\n")
	case res.Inconclusive != "":
		b.WriteString(inconclusivePrefix + res.Inconclusive + ".\n")
	case res.Run != "":
		fmt.Fprintf(&b, "%s; 'shrt chain slice -verify' compares it with source run %s.\n", hypothesisPrefix, res.Run)
	default:
		b.WriteString(hypothesisPrefix + " by 'shrt chain slice -verify'.\n")
	}
	asked := []string{}
	for _, k := range res.Kept {
		if k.Kind == KeepAsked {
			asked = append(asked, k.ID)
		}
	}
	if len(asked) > 0 {
		fmt.Fprintf(&b, "Kept on request: %s.\n", listSome(asked, 8))
	}
	for _, k := range res.Kept {
		if k.Kind == KeepCheckpoint {
			fmt.Fprintf(&b, "Kept as checkpoint: %s (%s).\n", k.ID, strings.TrimPrefix(k.Reason, KeepCheckpoint+": "))
		}
	}
	if len(res.Relaxed) > 0 {
		fmt.Fprintf(&b, "Relaxed, failed in run %s after an answer: %s.\n", res.Run, RelaxedList(res.Relaxed))
	}
	for _, f := range res.FilledVars {
		switch {
		case f.From == VarFromRun && f.Declared:
			fmt.Fprintf(&b, "Var %s holds the value run %s used, not the default %s declares.\n", f.Var, res.Run, res.Source)
		case f.From == VarFromRun:
			fmt.Fprintf(&b, "Var %s is not declared by %s; its value is the one run %s used.\n", f.Var, res.Source, res.Run)
		}
	}
	if len(res.FreshVars) > 0 {
		fmt.Fprintf(&b, "Kept writes create with %s: run it with -var <name>=<fresh>.\n", strings.Join(res.FreshVars, ", "))
	}
	if len(res.DroppedWrites) > 0 {
		names := make([]string, 0, len(res.DroppedWrites))
		for _, d := range res.DroppedWrites {
			names = append(names, d.ID)
		}
		fmt.Fprintf(&b, "%d dropped step(s) WRITE: %s.\n", len(names), listSome(names, 8))
	}
	if len(res.RefusedWrites) > 0 {
		names := make([]string, 0, len(res.RefusedWrites))
		for _, d := range res.RefusedWrites {
			names = append(names, d.ID)
		}
		fmt.Fprintf(&b, "%d dropped write step(s) refused in run %s: %s.\n", len(names), res.Run, listSome(names, 8))
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
		fmt.Fprintf(&b, "Unmet contract prerequisite(s), no earlier step calls them: %s.\n", listSome(names, 8))
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
	reached map[int]map[int]bool
}

func newStepIndex(c *Chain) *stepIndex {
	x := &stepIndex{c: c, byID: map[string]int{}, exports: map[string][]int{}, rpc: map[int]string{}, reached: map[int]map[int]bool{}}
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

func (x *stepIndex) lastCallOf(p Prereq, before int, referenced map[int]bool, opts SliceOptions) (int, bool) {
	best, rank := 0, 0
	for i := before - 1; i >= 0; i-- {
		if !p.calledBy(x.rpcOf(i, opts)) || producesNothing(x.c.Steps[i], opts) {
			continue
		}
		r := 1
		if p.Alias == "" || carriesAlias(x.c.Steps[i].ID, p.Alias) {
			r += 2
		}
		if referenced[i] {
			r++
		}
		if r > rank {
			best, rank = i, r
		}
	}
	return best, rank > 0
}

func (x *stepIndex) callsSharingProducers(p Prereq, before int, referenced map[int]bool, opts SliceOptions) []int {
	out := []int{}
	for i := 0; i < before; i++ {
		if !p.calledBy(x.rpcOf(i, opts)) || producesNothing(x.c.Steps[i], opts) {
			continue
		}
		for _, ref := range stepRefs(x.c.Steps[i]) {
			if j, kind := x.producerOf(ref, i); kind == refStep && referenced[j] {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

func (x *stepIndex) reach(i int) map[int]bool {
	if got, ok := x.reached[i]; ok {
		return got
	}
	out := map[int]bool{}
	x.reached[i] = out
	for _, ref := range stepRefs(x.c.Steps[i]) {
		j, kind := x.producerOf(ref, i)
		if kind != refStep || out[j] {
			continue
		}
		out[j] = true
		for k := range x.reach(j) {
			out[k] = true
		}
	}
	return out
}

type sideEffectWrite struct {
	index  int
	reason string
}

func (x *stepIndex) sideEffectWrites(at int, keeps map[int]*Keep, opts SliceOptions) []sideEffectWrite {
	type state struct {
		rpc      string
		kept     string
		neededBy string
		entities map[int]bool
	}
	states := []state{}
	for i := range keeps {
		for _, p := range x.prereqsOf(x.c.Steps[i], opts) {
			if valueEdge(p.Edge) || !isWriteCall(p.RPC) {
				continue
			}
			entities := map[int]bool{}
			kept := ""
			for j := range keeps {
				if j >= i || x.rpcOf(j, opts) != p.RPC {
					continue
				}
				for k := range x.reach(j) {
					if x.reach(i)[k] {
						entities[k] = true
					}
				}
				if kept == "" || x.c.Steps[j].ID < kept {
					kept = x.c.Steps[j].ID
				}
			}
			if len(entities) > 0 {
				states = append(states, state{rpc: p.RPC, kept: kept, neededBy: x.c.Steps[i].ID, entities: entities})
			}
		}
	}
	sort.Slice(states, func(a, b int) bool {
		if states[a].neededBy != states[b].neededBy {
			return states[a].neededBy < states[b].neededBy
		}
		return states[a].rpc < states[b].rpc
	})
	out := []sideEffectWrite{}
	for w := 0; w < at; w++ {
		if _, kept := keeps[w]; kept {
			continue
		}
		s := x.c.Steps[w]
		if !isWriteCall(s.Call) || notSent(s, opts) || (opts.IsLogin != nil && opts.IsLogin(s)) {
			continue
		}
		rpc := x.rpcOf(w, opts)
		for _, st := range states {
			if rpc != st.rpc && !x.rpcNeeds(rpc, st.rpc, opts) {
				continue
			}
			shared := ""
			for k := range x.reach(w) {
				if st.entities[k] && (shared == "" || x.c.Steps[k].ID < shared) {
					shared = x.c.Steps[k].ID
				}
			}
			if shared == "" {
				continue
			}
			out = append(out, sideEffectWrite{index: w, reason: fmt.Sprintf("changes the state %s sets on %s, which %s needs (%s needs %s)",
				st.rpc, shared, st.neededBy, rpc, st.rpc)})
			break
		}
	}
	return out
}

func (x *stepIndex) entitiesSent(i int) []int {
	out := []int{}
	for _, ref := range x.c.Steps[i].SendReferences() {
		if r := ParseRef(ref); r.Kind == RefStep {
			if section, _, _ := strings.Cut(r.Rest, "."); section == "request" {
				continue
			}
		}
		if j, kind := x.producerOf(ref, i); kind == refStep {
			out = append(out, j)
		}
	}
	return out
}

func (x *stepIndex) entitiesReached(i int, throughWrites bool, seen map[int]bool) map[int]bool {
	out := map[int]bool{}
	if seen[i] {
		return out
	}
	seen[i] = true
	for _, j := range x.entitiesSent(i) {
		out[j] = true
		if throughWrites || IsReadOnlyCall(x.c.Steps[j].Call) {
			for k := range x.entitiesReached(j, throughWrites, seen) {
				out[k] = true
			}
		}
	}
	return out
}

func (x *stepIndex) stateWrites(at int, keeps map[int]*Keep, opts SliceOptions) []sideEffectWrite {
	readers := []int{}
	asserts := map[int]bool{}
	for i, k := range keeps {
		if i == at || k.Kind == KeepAsked || IsReadOnlyCall(x.c.Steps[i].Call) {
			readers = append(readers, i)
		} else if opts.AssertsWrite != nil {
			readers = append(readers, i)
			asserts[i] = true
		}
	}
	sort.Ints(readers)
	out := []sideEffectWrite{}
	for w := 0; w < at; w++ {
		if _, kept := keeps[w]; kept {
			continue
		}
		s := x.c.Steps[w]
		if !isWriteCall(s.Call) || notSent(s, opts) || (opts.IsLogin != nil && opts.IsLogin(s)) {
			continue
		}
		reach := x.entitiesReached(w, true, map[int]bool{})
		for _, r := range readers {
			if w >= r {
				continue
			}
			if opts.StateIrrelevant != nil && opts.StateIrrelevant(s.ID, x.c.Steps[r].ID) || asserts[r] && !opts.AssertsWrite(s.ID, x.c.Steps[r].ID) {
				continue
			}
			shared := ""
			for e := range x.entitiesReached(r, false, map[int]bool{}) {
				if _, fresh := keeps[e]; !fresh {
					continue
				}
				if reach[e] && (shared == "" || x.c.Steps[e].ID < shared) {
					shared = x.c.Steps[e].ID
				}
			}
			if shared == "" {
				if filter, v := listFilterShared(s, x.c.Steps[r]); filter != "" {
					out = append(out, sideEffectWrite{index: w, reason: fmt.Sprintf("sends a value built from ${vars.%s}, as %s filters by it (%s), "+
						"so what it creates can be listed there with no reference to it", v, x.c.Steps[r].ID, filter)})
					break
				}
				continue
			}
			out = append(out, sideEffectWrite{index: w, reason: fmt.Sprintf("changes the state of what %s created, which %s reads",
				shared, x.c.Steps[r].ID)})
			break
		}
	}
	return out
}

func (x *stepIndex) rpcNeeds(rpc, need string, opts SliceOptions) bool {
	if opts.Prereqs == nil {
		return false
	}
	for _, p := range opts.Prereqs(rpc) {
		if p.RPC == need && !valueEdge(p.Edge) {
			return true
		}
	}
	return false
}

func (x *stepIndex) fieldNeedsNoProducer(s *Step, at int, field string) bool {
	if field == "" || s.Body == nil {
		return false
	}
	v, found := Get(s.Body, field)
	if !found || v == nil {
		return false
	}
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	for _, ref := range collectRefs(v) {
		if _, kind := x.producerOf(ref, at); kind == refStep {
			return false
		}
	}
	return true
}

func producesNothing(s *Step, opts SliceOptions) bool {
	if opts.Refused != nil {
		if _, refused := opts.Refused(s.ID); refused {
			return true
		}
	}
	return ExpectsRefusal(s)
}

func notSent(s *Step, opts SliceOptions) bool {
	if opts.Refused == nil {
		return false
	}
	why, refused := opts.Refused(s.ID)
	return refused && why == RefusedNotSent
}

func ExpectsRefusal(s *Step) bool {
	for _, e := range s.Expect {
		if ExpectsTransportRefusal(e) {
			return true
		}
		if strings.Join(SplitPath(e.Path), ".") == EnvelopePath() && (e.Equals != nil && stringify(e.Equals) != EnvelopeOK() || e.NotEqual != nil && stringify(e.NotEqual) == EnvelopeOK()) {
			return true
		}
	}
	return false
}

func valueEdge(edge string) bool {
	return edge == "from" || edge == "same_as"
}

func carriesAlias(id, alias string) bool {
	return strings.HasSuffix(id, "_"+alias) || strings.Contains(id, "_"+alias+"_")
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
	refs := s.References()
	sort.Strings(refs)
	return slices.Compact(refs)
}

func isWriteCall(call string) bool {
	return !IsReadOnlyCall(call)
}

func copyStep(s *Step) *Step {
	out := *s
	out.Body, _ = copyValue(s.Body).(map[string]any)
	out.Headers = maps.Clone(s.Headers)
	out.Export = maps.Clone(s.Export)
	out.Volatile = slices.Clone(s.Volatile)
	if len(s.Expect) > 0 {
		out.Expect = make([]Expectation, 0, len(s.Expect))
		for _, e := range s.Expect {
			out.Expect = append(out.Expect, e.MapOperands(copyValue))
		}
	}
	return &out
}

func copyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = copyValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, copyValue(item))
		}
		return out
	default:
		return v
	}
}

type Verdict struct {
	Step      string            `json:"step"`
	Status    string            `json:"status"`
	ErrorCode string            `json:"error_code"`
	Refusal   map[string]string `json:"refusal,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Expect    []ExpectResult    `json:"expect"`
}

func CompareVerdicts(source, replay Verdict) []string {
	return CompareVerdictsMasking(source, replay, nil)
}

func CompareVerdictsMasking(source, replay Verdict, same func(path string, a, b any) bool) []string {
	alike := func(path string, a, b any) bool { return same != nil && same(path, a, b) }
	diffs := []string{}
	if source.ErrorCode != replay.ErrorCode {
		diffs = append(diffs, fmt.Sprintf("%s: source %q, slice %q", EnvelopePath(), source.ErrorCode, replay.ErrorCode))
	}
	if source.Transport != replay.Transport {
		diffs = append(diffs, fmt.Sprintf("transport: source %s, slice %s", orNoRefusal(source.Transport), orNoRefusal(replay.Transport)))
	}
	for _, field := range refusalFields(source.Refusal, replay.Refusal) {
		a, b := source.Refusal[field], replay.Refusal[field]
		if a != b && !alike(field, a, b) {
			diffs = append(diffs, fmt.Sprintf("refusal %s: source %s, slice %s", field, orNoRefusal(strconv.Quote(a)), orNoRefusal(strconv.Quote(b))))
		}
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
			diffs = append(diffs, fmt.Sprintf("expectation %d: source %s %s, slice %s %s", i+1, want.Path, want.Rule, got.Path, got.Rule))
			continue
		}
		if want.Passed != got.Passed {
			diffs = append(diffs, fmt.Sprintf("expectation %d (%s %s): source passed=%t, slice passed=%t", i+1, want.Path, want.Rule, want.Passed, got.Passed))
			continue
		}
		if !want.Passed && verdictText(want.Want) != verdictText(got.Want) && !alike(want.Path, want.Want, got.Want) {
			diffs = append(diffs, fmt.Sprintf("expectation %d (%s %s): failed in both, with other values: source want %s got %s, slice want %s got %s",
				i+1, want.Path, want.Rule, verdictText(want.Want), verdictText(want.Got), verdictText(got.Want), verdictText(got.Got)))
			continue
		}
		if !want.Passed && verdictText(want.Got) != verdictText(got.Got) && !alike(want.Path, want.Got, got.Got) {
			diffs = append(diffs, fmt.Sprintf("expectation %d (%s %s): failed in both, differently: source got %s, slice got %s",
				i+1, want.Path, want.Rule, verdictText(want.Got), verdictText(got.Got)))
		}
	}
	return diffs
}

func refusalFields(a, b map[string]string) []string {
	out := []string{}
	for _, field := range []string{"message", "reason", "app_code"} {
		if a[field] != "" || b[field] != "" {
			out = append(out, field)
		}
	}
	return out
}

func orNoRefusal(s string) string {
	if s == "" || s == `""` {
		return "none"
	}
	return s
}

func verdictText(v any) string {
	if v == nil {
		return "nothing"
	}
	return scalarText(v)
}

var listingPrefixes = []string{"List", "Search", "Query", "Find"}

func isListingCall(call string) bool { return callHasPrefix(call, listingPrefixes) }

func filterVars(v any, out map[string]string) {
	walkLeaves(v, "", "", func(path, _, t string) {
		refs := []string{}
		for _, m := range refPattern.FindAllStringSubmatch(t, -1) {
			refs = append(refs, strings.TrimSpace(m[1]))
		}
		for _, ref := range refs {
			if r := ParseRef(ref); r.Kind != RefVars {
				return
			}
		}
		for _, ref := range refs {
			name, _, _ := strings.Cut(ParseRef(ref).Rest, ".")
			if _, seen := out[name]; !seen && name != "" {
				out[name] = path
			}
		}
	})
}

func listFilterShared(writer, reader *Step) (string, string) {
	if writer == nil || reader == nil || !isListingCall(reader.Call) || ExpectsRefusal(reader) || reader.SkipAuth {
		return "", ""
	}
	filters := map[string]string{}
	filterVars(reader.Body, filters)
	if len(filters) == 0 {
		return "", ""
	}
	sent := map[string]string{}
	filterVars(writer.Body, sent)
	for _, name := range sortedKeys(sent) {
		if field, ok := filters[name]; ok {
			return field, name
		}
	}
	return "", ""
}
