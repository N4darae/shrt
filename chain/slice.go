package chain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SliceModeClosure = "closure"
	SliceModePin     = "pin"
)

const (
	KeepTarget   = "target"
	KeepProduces = "produces"
	KeepContract = "contract"
	KeepAsked    = "requested"
)

const SliceKeepWrites = "writes"

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
		if strings.HasPrefix(name, p) && wordBoundaryAt(name, len(p)) {
			return true
		}
	}
	return false
}

func wordBoundaryAt(name string, at int) bool {
	if at >= len(name) {
		return true
	}
	c := name[at]
	return c < 'a' || c > 'z'
}

type Prereq struct {
	RPC   string `json:"rpc"`
	Alias string `json:"alias,omitempty"`
	Edge  string `json:"edge"`
	For   string `json:"for,omitempty"`
}

func (p Prereq) Node() string {
	if p.Alias == "" {
		return p.RPC
	}
	return p.RPC + "@" + p.Alias
}

type SliceOptions struct {
	Mode    string
	RunID   string
	Name    string
	RPCOf   func(*Step) string
	Prereqs func(rpc string) []Prereq
	Value   func(ref string) (any, bool)
	Keep    []string
	Vars    map[string]any
	RunVars map[string]any
	Refused func(stepID string) (string, bool)

	RunVarsAsDefaults bool
	Performed         func(stepID string) bool
	IsLogin           func(*Step) bool
	Relax             func(stepID string) []ExpectResult
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

type Satisfied struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	RPC   string `json:"rpc"`
	Edge  string `json:"edge"`
	For   string `json:"for"`
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
	Reason string `json:"wrote_nothing,omitempty"`
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
	Source        string      `json:"source"`
	Target        string      `json:"target"`
	Mode          string      `json:"mode"`
	Run           string      `json:"run,omitempty"`
	Total         int         `json:"total"`
	Reach         int         `json:"reach"`
	Kept          []Keep      `json:"kept"`
	Pins          []Pinned    `json:"pins,omitempty"`
	Unmet         []Unmet     `json:"unmet,omitempty"`
	Satisfied     []Satisfied `json:"satisfied_by_run,omitempty"`
	DroppedWrites []Dropped   `json:"dropped_writes,omitempty"`
	RefusedWrites []Dropped   `json:"dropped_refused_writes,omitempty"`
	UnderIncluded bool        `json:"under_included"`
	FilledVars    []FilledVar `json:"filled_vars,omitempty"`
	MissingVars   []string    `json:"missing_vars,omitempty"`
	FreshVars     []string    `json:"fresh_vars,omitempty"`
	DroppedPins   []Pin       `json:"dropped_kept_red,omitempty"`
	Relaxed       []Relaxed   `json:"relaxed,omitempty"`
	Verified      string      `json:"verified,omitempty"`
	NotReproduced string      `json:"not_reproduced,omitempty"`
	Inconclusive  string      `json:"inconclusive,omitempty"`
	Build         string      `json:"verify_build,omitempty"`
	SourceRef     string      `json:"-"`
	Chain         *Chain      `json:"-"`
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
				if opts.Refused != nil {
					if _, refused := opts.Refused(s.ID); refused {
						continue
					}
				}
				add(w, KeepAsked, "kept by -keep writes")
			}
			continue
		}
		if !ok {
			return nil, fmt.Errorf("chain %q has no step %q to keep (-keep %s keeps every earlier write step)\nvalid step ids:\n  %s", c.Name, id, SliceKeepWrites, strings.Join(idx.ids(), "\n  "))
		}
		if j > at {
			return nil, fmt.Errorf("step %q runs after the target %q, so keeping it cannot change the target's verdict", id, target)
		}
		add(j, KeepAsked, KeepAsked)
	}

	unmet := []Unmet{}
	satisfied := map[int]Satisfied{}
	seenUnmet := map[string]bool{}
	boundAlias := map[int]string{}
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
		referenced := map[int]bool{}
		pinnedOnly := map[int]bool{}
		for _, ref := range stepRefs(s) {
			j, kind := idx.producerOf(ref, i)
			if kind != refStep {
				continue
			}
			referenced[j] = true
			if pinnable(ref) {
				if _, seen := keeps[j]; !seen {
					pinnedOnly[j] = true
				}
				continue
			}
			delete(pinnedOnly, j)
			add(j, KeepProduces, fmt.Sprintf("produces ${%s} used by %s", ref, s.ID))
		}
		for _, p := range idx.prereqsOf(s, opts) {
			if p.For != "" && !carriesAlias(s.ID, p.For) {
				continue
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
			if pinnedOnly[j] && valueEdge(p.Edge) {
				continue
			}
			if leftToRun(mode, p, c.Steps[j], opts) {
				if _, done := satisfied[j]; !done {
					satisfied[j] = Satisfied{Index: j + 1, ID: c.Steps[j].ID, RPC: p.Node(), Edge: p.Edge, For: s.ID}
				}
				continue
			}
			add(j, KeepContract, fmt.Sprintf("contract needs %s (%s)", p.Node(), p.Edge))
		}
	}

	sort.Ints(order)
	res := &SliceResult{
		Source: c.Name,
		Target: target,
		Mode:   mode,
		Run:    opts.RunID,
		Total:  len(c.Steps),
		Reach:  at + 1,
		Kept:   make([]Keep, 0, len(order)),
	}
	for _, i := range order {
		res.Kept = append(res.Kept, *keeps[i])
	}
	res.Unmet = unmet
	for j, sat := range satisfied {
		if _, kept := keeps[j]; !kept {
			res.Satisfied = append(res.Satisfied, sat)
		}
	}
	sort.Slice(res.Satisfied, func(a, b int) bool { return res.Satisfied[a].Index < res.Satisfied[b].Index })

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
		st := rewriteStep(c.Steps[i], pins)
		if st.ID != target && opts.Relax != nil {
			res.Relaxed = append(res.Relaxed, relaxStep(st, opts.Relax(st.ID))...)
		}
		out.Steps = append(out.Steps, st)
		kept[c.Steps[i].ID] = true
	}
	for _, k := range c.KeptRed {
		if kept[k.Step] {
			out.KeptRed = append(out.KeptRed, k)
		} else {
			res.DroppedPins = append(res.DroppedPins, k)
		}
	}
	existing := []*Step{}
	if mode == SliceModePin {
		for i, s := range c.Steps[:at] {
			if _, seen := keeps[i]; !seen {
				existing = append(existing, s)
			}
		}
	}
	pinned := map[string]bool{}
	for _, p := range res.Pins {
		pinned[p.Var] = true
	}
	res.FreshVars = []string{}
	for _, name := range FreshVars(out.Steps, opts.IsLogin, existing) {
		if !pinned[name] {
			res.FreshVars = append(res.FreshVars, name)
		}
	}
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
		if mode != SliceModePin && !opts.RunVarsAsDefaults {
			continue
		}
		v, ok := opts.RunVars[name]
		if !ok || fmt.Sprint(v) == fmt.Sprint(c.Vars[name]) {
			continue
		}
		vars[name] = v
		res.FilledVars = append(res.FilledVars, FilledVar{Var: name, Value: v, From: VarFromRun, Declared: true, Default: c.Vars[name]})
	}
	for _, p := range res.Pins {
		vars[p.Var] = p.Value
	}
	undeclared := []string{}
	for name := range usedVars {
		if _, declared := c.Vars[name]; !declared {
			if _, pinned := vars[name]; !pinned {
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
		if v, ok := opts.RunVars[name]; ok && mode == SliceModePin {
			vars[name] = v
			res.FilledVars = append(res.FilledVars, FilledVar{Var: name, Value: v, From: VarFromRun})
			continue
		}
		res.MissingVars = append(res.MissingVars, name)
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
	r.NotReproduced, r.Inconclusive = "", ""
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
	r.Verified, r.Inconclusive = "", ""
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
	r.Verified, r.NotReproduced = "", ""
	if r.Chain != nil {
		r.Chain.Description = sliceDescription(r)
	}
}

func (r *SliceResult) OwnRunOutcome(outcome, chainName, sourceRun, sliceRun string, at time.Time, why string) string {
	gave := "another verdict than"
	if outcome == "INCONCLUSIVE" {
		gave = "a verdict that does not settle it against"
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
	verifiedPrefix      = "VERIFIED by 'shrt chain slice -verify': "
	notReproducedPrefix = "NOT REPRODUCED by 'shrt chain slice -verify': "
	rerunPrefix         = "RE-RUN by 'shrt chain slice -verify': "
	inconclusivePrefix  = "INCONCLUSIVE by 'shrt chain slice -verify': "
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
	return replaceVerdict(description, verifiedLine(verified), verifiedPrefix, notReproducedPrefix, inconclusivePrefix)
}

func RecordRerun(description, verdict string) string {
	return replaceVerdict(description, rerunPrefix+verdict+".\n", rerunPrefix)
}

func replaceVerdict(description, line string, prefixes ...string) string {
	if strings.Contains(description, hypothesisParagraph) {
		description = strings.Replace(description, hypothesisParagraph, "\n\x00", 1)
	}
	kept := []string{}
	for _, l := range strings.SplitAfter(description, "\n") {
		verdict := false
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
	fmt.Fprintf(&b, "%s %d of %d steps, mode %s", SliceDescriptionPrefix(res.Source, res.Target), len(res.Kept), res.Total, res.Mode)
	if res.Mode == SliceModePin {
		fmt.Fprintf(&b, ", values pinned from run %s", res.Run)
	}
	b.WriteString(".\n\n")
	b.WriteString("Computed by 'shrt chain slice': the target step, every earlier step whose output a kept\n")
	b.WriteString("step references, and every ordering prerequisite the contracts declare for a kept rpc.\n")
	asked := []string{}
	for _, k := range res.Kept {
		if k.Kind == KeepAsked {
			asked = append(asked, k.ID)
		}
	}
	if len(asked) > 0 {
		fmt.Fprintf(&b, "Kept on request: %s.\n", listSome(asked, 8))
	}
	if len(res.Pins) > 0 {
		fmt.Fprintf(&b, "%d value(s) that earlier steps produced are pinned into vars, so their producers are gone.\n", len(res.Pins))
	}
	if len(res.Relaxed) > 0 {
		fmt.Fprintf(&b, "Relaxed: kept step(s) failed these expectations in run %s after the backend answered, so the call took\n"+
			"effect; the slice drops them so it reaches the target, and each call must still be answered: %s.\n", res.Run, RelaxedList(res.Relaxed))
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
		fmt.Fprintf(&b, "Kept writes interpolate var(s) %s into what they create: run it with a value the backend has\nnot seen, -var <name>=<fresh>.\n", strings.Join(res.FreshVars, ", "))
	}
	if len(res.Satisfied) > 0 {
		parts := make([]string, 0, len(res.Satisfied))
		for _, sat := range res.Satisfied {
			parts = append(parts, fmt.Sprintf("%s (%s %s for %s)", sat.ID, sat.Edge, sat.RPC, sat.For))
		}
		fmt.Fprintf(&b, "Contract prerequisite write(s) run %s already performed are left to that run, not re-sent: %s.\n", res.Run, listSome(parts, 8))
	}
	if res.Verified != "" {
		b.WriteString("\n" + verifiedLine(res.Verified))
	} else if res.NotReproduced != "" {
		b.WriteString("\n" + notReproducedPrefix + res.NotReproduced + ".\n")
	} else if res.Inconclusive != "" {
		b.WriteString("\n" + inconclusivePrefix + res.Inconclusive + ".\n")
	} else {
		b.WriteString(hypothesisParagraph)
	}
	if len(res.DroppedWrites) > 0 {
		names := make([]string, 0, len(res.DroppedWrites))
		for _, d := range res.DroppedWrites {
			names = append(names, d.ID)
		}
		fmt.Fprintf(&b, "\n%d dropped step(s) WRITE: %s.\n", len(names), listSome(names, 8))
	}
	if len(res.RefusedWrites) > 0 {
		names := make([]string, 0, len(res.RefusedWrites))
		for _, d := range res.RefusedWrites {
			names = append(names, d.ID)
		}
		fmt.Fprintf(&b, "\n%d dropped write step(s) were refused in run %s and wrote nothing: %s.\n", len(names), res.Run, listSome(names, 8))
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

func (x *stepIndex) lastCallOf(p Prereq, before int, referenced map[int]bool, opts SliceOptions) (int, bool) {
	best, rank := 0, 0
	for i := before - 1; i >= 0; i-- {
		if x.rpcOf(i, opts) != p.RPC {
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

func leftToRun(mode string, p Prereq, s *Step, opts SliceOptions) bool {
	if mode != SliceModePin || valueEdge(p.Edge) || opts.Performed == nil || !isWriteCall(s.Call) {
		return false
	}
	return opts.Performed(s.ID)
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
