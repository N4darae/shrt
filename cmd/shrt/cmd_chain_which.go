package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func chainWhich(args []string) error {
	fs := flag.NewFlagSet("chain which", flag.ContinueOnError)
	rpc := fs.String("rpc", "", "chains with a step calling this rpc, as package.Service/Rpc, Service/Rpc or a bare Rpc")
	code := fs.String("code", "", "chains asserting this app_code, envelope code, failure reason, transport code (unauthenticated) or HTTP status (401)")
	asJSON := fs.Bool("json", false, "emit JSON")
	verbose := fs.Bool("v", false, "explain the chain headers and the rows")
	setUsage(fs, "usage: shrt chain which [-rpc <rpc>] [-code <n>] [-json] [-v]   which chains, or local run records, exercise an rpc or a failure code",
		"\nexit codes:\n  0  a chain or run record matched\n  1  nothing matched, or bad flags (neither -rpc nor -code, an unknown rpc)\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q\n\nusage: shrt chain which [-rpc <rpc>] [-code <n>] [-json] [-v]", rest[0])
	}
	if *rpc == "" && *code == "" {
		return fmt.Errorf("name what to look for: -rpc <Service/Rpc>, -code <app_code>, or both")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	chains, _, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil {
		return err
	}

	q := chain.WhichQuery{Code: *code}
	if *rpc != "" {
		m, err := e.cat.Lookup(*rpc)
		if err != nil {
			return err
		}
		q.RPC = m.FullName
	}
	lib, err := e.library()
	if err != nil {
		return err
	}
	opts := chain.WhichOptions{
		RPCOf:        rpcOf(e),
		SliceOf:      sliceSizeOf(e, lib),
		Observations: runObservations(e),
		FreshVars:    freshVarsOf(e, lib),
		ReadsOnly:    isLoginStep(e),
	}
	q.Aliases = whichCodeAliases(q.Code, chains, opts.Observations, lib)
	hits := chain.Which(chains, q, opts)
	byName := map[string]*chain.Chain{}
	for _, c := range chains {
		byName[c.Name] = c
	}
	preferClosureRepro(e, lib, byName, hits)
	if len(hits) == 0 {
		seen := chain.WhichObservedUnasserted(chains, q, opts)
		if len(seen) == 0 {
			return fmt.Errorf("no chain in %s %s, and no local run record observed it\nnothing to slice: the corpus does not exercise it yet", e.chainsDir(), describeWhichQuery(q))
		}
		if *asJSON {
			return emitJSON(map[string]any{"chains": hits, "observed_unasserted": seen})
		}
		printObservedUnasserted(e, byName, q, seen)
		return nil
	}
	if *asJSON {
		return emitJSON(hits)
	}
	printWhich(hits, q, e.targetURL(), *verbose, situationsOf(byName, lib, e))
	return nil
}

func situationsOf(byName map[string]*chain.Chain, lib *contract.Library, e *env) func(string, string) string {
	known := map[string]map[string]contract.Situation{}
	return func(name, step string) string {
		if _, ok := known[name]; !ok {
			known[name] = contract.Situations(byName[name], lib, e.cat)
		}
		if sit, ok := known[name][step]; ok {
			return sit.String()
		}
		return ""
	}
}

func describeWhichQuery(q chain.WhichQuery) string {
	parts := []string{}
	if q.RPC != "" {
		parts = append(parts, "calling "+q.RPC)
	}
	if q.Code != "" {
		parts = append(parts, strings.Join(append([]string{"asserting " + q.Code}, q.Aliases...), " or "))
	}
	return strings.Join(parts, " and ")
}

func sliceSizeOf(e *env, lib *contract.Library) func(*chain.Chain, string) (int, bool) {
	opts := chain.SliceOptions{RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib), KeyField: contract.KeyFieldFor(lib)}
	return func(c *chain.Chain, step string) (int, bool) {
		res, err := chain.Slice(c, step, opts)
		if err != nil {
			return 0, false
		}
		return len(res.Kept), true
	}
}

func runObservations(e *env) func(string) []chain.Observation {
	refused := map[string]bool{}
	return func(name string) []chain.Observation {
		ids, err := e.store.ListRuns(name)
		if err != nil || len(ids) == 0 {
			return nil
		}
		out := []chain.Observation{}
		for _, id := range ids {
			rec, err := e.store.LoadRun(name, id)
			if errors.Is(err, store.ErrRunEdited) && !refused[name+"/"+id] {
				refused[name+"/"+id] = true
				fmt.Fprintf(os.Stderr, "chain which: not cited: %v\n", err)
			}
			if err != nil || e.otherTarget(rec.Target) {
				continue
			}
			for _, s := range rec.Steps {
				o := chain.Observation{
					Run:    rec.RunID,
					Step:   s.ID,
					Status: s.Status,
					Reached: s.Status == runner.StatusPassed || s.Status == runner.StatusFailed ||
						(s.Status == runner.StatusError && (s.HTTPStatus != 0 || s.Transport != nil)),
				}
				if s.Status == runner.StatusFailed {
					o.Failures = s.Expect
				}
				o.Response = observedResponse(s)
				out = append(out, o)
			}
		}
		return out
	}
}

func observedResponse(s *runner.StepRecord) any {
	response := decoded(s.Response)
	if s.HTTPStatus == 0 && s.Transport == nil {
		return response
	}
	code, message := "", ""
	if s.Transport != nil {
		code, message = s.Transport.Code, s.Transport.Message
	}
	outcome := chain.TransportOutcome(s.HTTPStatus, code, message)
	obj, ok := response.(map[string]any)
	if !ok {
		return outcome
	}
	merged := maps.Clone(obj)
	merged[chain.TransportPrefix] = outcome[chain.TransportPrefix]
	return merged
}

func preferClosureRepro(e *env, lib *contract.Library, byName map[string]*chain.Chain, hits []chain.WhichChain) {
	keepFlag := " -keep " + chain.SliceKeepWrites
	for i := range hits {
		h := &hits[i]
		c := byName[h.Chain]
		if c == nil || !strings.Contains(h.Command, keepFlag) {
			continue
		}
		var best *chain.WhichStep
		for j := range h.Matches {
			if h.Matches[j].Step == h.Best {
				best = &h.Matches[j]
			}
		}
		if best == nil || best.Observed == nil {
			continue
		}
		rec, err := e.store.LoadRun(c.Name, best.Observed.Run)
		if err != nil {
			continue
		}
		o := chain.SliceOptions{RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib), KeyField: contract.KeyFieldFor(lib)}
		plain, err := chain.Slice(c, h.Best, o)
		if err != nil {
			continue
		}
		o.Keep = []string{chain.SliceKeepWrites}
		kept, err := chain.Slice(c, h.Best, o)
		if err != nil || len(kept.Kept) <= len(plain.Kept) {
			continue
		}
		closure := "shrt chain slice " + c.Name + " -step " + h.Best
		if len(plain.FreshVars) > 0 {
			closure += " " + freshFlags(slices.Sorted(slices.Values(plain.FreshVars)))
		}
		if related, _ := relatedDroppedWrites(plain, rec); len(related) > 0 {
			h.CommandSteps = len(kept.Kept)
			continue
		}
		h.Fallback, h.FallbackSteps = h.Command, len(kept.Kept)
		h.Command, h.CommandSteps = closure, len(plain.Kept)
	}
}

func freshFlags(names []string) string {
	flags := make([]string, 0, len(names))
	for _, name := range names {
		flags = append(flags, "-var "+name+"=<fresh>")
	}
	return strings.Join(flags, " ")
}

func freshVarsOf(e *env, lib *contract.Library) func(*chain.Chain, string) []string {
	opts := chain.SliceOptions{RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib), KeyField: contract.KeyFieldFor(lib)}
	login := isLoginStep(e)
	return func(c *chain.Chain, step string) []string {
		o := opts
		if s, ok := c.Step(step); ok && !chain.IsReadOnlyCall(s.Call) && !login(s) && !chain.IsAuthProbe(s) {
			o.Keep = []string{chain.SliceKeepWrites}
		}
		res, err := chain.Slice(c, step, o)
		if err != nil {
			return nil
		}
		return res.FreshVars
	}
}

func printWhich(hits []chain.WhichChain, q chain.WhichQuery, target string, verbose bool, situation func(string, string) string) {
	steps, rows, at, usual := 0, make([][]string, len(hits)), map[string]int{}, whichUsualCode(hits, q)
	for i, h := range hits {
		steps += len(h.Matches)
		at[h.Chain] = i
		for _, m := range h.Matches {
			rows[i] = append(rows[i], whichRow(h, m, q, usual, situation))
		}
	}
	folds := make([]int, len(hits))
	for i, h := range hits {
		cut := strings.LastIndex(h.Chain, "-slice-")
		if j, ok := at[h.Chain[:max(cut, 0)]]; cut > 0 && ok && (h.Runs > 0) == (hits[j].Runs > 0) &&
			!slices.ContainsFunc(rows[i], func(r string) bool { return !slices.Contains(rows[j], r) }) {
			folds[j]++
			rows[i] = nil
		}
	}
	fmt.Printf("%s, %s %s", plural(len(hits), "chain"), plural(steps, "step"), describeWhichQuery(q))
	if q.Code == "" && usual != "" {
		fmt.Printf("; a row asserts %s unless it says otherwise", usual)
	}
	fmt.Println()
	for i, h := range hits {
		if rows[i] != nil {
			fmt.Printf("%s\n  %s\n", whichHead(h, folds[i]), strings.Join(rows[i], "\n  "))
		}
	}
	if verbose {
		fmt.Printf("Each chain heads with the command that reproduces its first step that did not pass, else its first row.\n"+
			"A plain slice keeps the writes on the step's entities; -keep writes keeps every earlier write.\n"+
			"A row is a step: the code it asserts if not the usual one, the state it acted on (-rpc), auth probe, and the verdict\n"+
			"of the newest local run that reached it, only if it did not pass or no run reached it. Runs are machine-local;\n"+
			"only those against %s count. -code also matches a code seen with it in a run or contract failure;\n"+
			"a reason alone matches only a step asserting no code. Same rows in N slices: chains <chain>-slice-<step> with these rows.\n", target)
	}
}

func whichHead(h chain.WhichChain, folds int) string {
	notes, alt := []string{}, ""
	if h.CommandSteps > 0 {
		notes = append(notes, fmt.Sprintf("%d of %d steps", h.CommandSteps, h.Steps))
	}
	if h.Fallback != "" && strings.Replace(h.Fallback, " -keep "+chain.SliceKeepWrites, "", 1) == h.Command {
		notes = append(notes, "if it does not reproduce, add -keep writes")
	} else if h.Fallback != "" {
		alt = fmt.Sprintf("\n  else: %s  (%d steps)", h.Fallback, h.FallbackSteps)
	}
	if h.Runs == 0 {
		notes = append(notes, "no local runs")
	}
	if folds > 0 {
		notes = append(notes, "same rows in "+plural(folds, "slice"))
	}
	if len(notes) > 0 {
		return h.Command + "  (" + strings.Join(notes, "; ") + ")" + alt
	}
	return h.Command + alt
}

func whichUsualCode(hits []chain.WhichChain, q chain.WhichQuery) string {
	if q.Code != "" {
		return q.Code
	}
	counts, usual, rows := map[string]int{}, "", 0
	for _, h := range hits {
		for _, m := range h.Matches {
			rows++
			if a, ok := chain.PrimaryAssertionFor(m.Asserts, q); ok {
				if counts[a.Value]++; counts[a.Value] > counts[usual] {
					usual = a.Value
				}
			}
		}
	}
	if counts[usual] < 4 || 2*counts[usual] <= rows {
		return ""
	}
	return usual
}

func whichRow(h chain.WhichChain, m chain.WhichStep, q chain.WhichQuery, usual string, situation func(string, string) string) string {
	parts := []string{m.Step}
	a, asserted := chain.PrimaryAssertionFor(m.Asserts, q)
	switch {
	case m.ByReason != "":
		parts = append(parts, "asserts only reason "+m.ByReason)
	case !asserted && usual != "":
		parts = append(parts, "asserts no code")
	case asserted && !strings.EqualFold(a.Value, usual):
		parts = append(parts, "asserts "+a.Value)
	}
	if sit := situation(h.Chain, m.Step); sit != "" && q.RPC != "" {
		parts = append(parts, sit)
	}
	if m.Kind == chain.WhichKindAuthProbe {
		parts = append(parts, "auth probe")
	}
	switch o := m.Observed; {
	case o == nil && h.Runs > 0:
		parts = append(parts, "no run reached it")
	case o == nil:
	case len(o.Failures) > 0:
		parts = append(parts, shownStatus(o.Status)+" "+whichFailures(o.Failures))
	case o.Status != runner.StatusPassed:
		parts = append(parts, shownStatus(o.Status)+" got "+whichGot(o))
	case o.Asserted != "" && (o.Path != o.Asserted || !strings.EqualFold(o.Code, a.Value)):
		parts = append(parts, "got "+whichGot(o))
	}
	if n := m.Newest; n != nil && n.StoppedAt != "" {
		parts = append(parts, "newest run stopped at "+n.StoppedAt)
	} else if n != nil {
		parts = append(parts, "newest run: step "+n.Status)
	}
	return strings.Join(parts, "  ")
}

func whichFailures(fs []chain.ExpectResult) string {
	lead := 0
	for i, f := range fs {
		if _, held := blockedBy(f); !held {
			lead = i
			break
		}
	}
	out := chain.DescribeFailure(fs[lead])
	if up, held := blockedBy(fs[lead]); held {
		out = fs[lead].Path + " not evaluated: reads failed step " + up
	}
	also := []string{}
	for _, f := range fs {
		if f.Path != fs[lead].Path && !slices.Contains(also, f.Path) {
			also = append(also, f.Path)
		}
	}
	if len(also) > 0 {
		out += ", also " + chain.ListSome(also, 2)
	}
	return out
}

func whichGot(o *chain.WhichEvidence) string {
	switch {
	case o.Asserted != "" && o.Path != o.Asserted && o.Code != "":
		return o.Code + " at " + o.Path
	case o.Asserted != "" && o.Code == "":
		return "nothing at " + o.Asserted
	case o.Code == "":
		return "no code"
	}
	return o.Code
}

func shownStatus(status string) string {
	if status != runner.StatusPassed {
		return strings.ToUpper(status)
	}
	return status
}

func siblingPins(c *chain.Chain, s chain.WhichUnasserted) []string {
	parent, out := s.Path[:strings.LastIndex(s.Path, ".")+1], []string{}
	if st, ok := c.Step(s.Step); ok {
		for _, x := range st.Expect {
			if x.Equals != nil && x.Path != s.Path && strings.HasPrefix(x.Path, parent) && !strings.Contains(x.Path[len(parent):], ".") && chain.IsCodePath(x.Path) {
				out = append(out, fmt.Sprintf("%s equals %v", x.Path, x.Equals))
			}
		}
	}
	return out
}

func baselinedAt(e *env, s chain.WhichUnasserted) string {
	spot, err := e.store.LoadSafeSpot(s.Chain)
	if err != nil || !spot.DigestMatches() || e.otherTarget(spot.Target) {
		return ""
	}
	for _, st := range spot.Steps {
		if st.ID != s.Step || len(st.Response) == 0 {
			continue
		}
		v, ok := chain.Get(decoded(st.Response), s.Path)
		if !ok || !strings.EqualFold(fmt.Sprint(v), s.Code) {
			return ""
		}
		masks := pathmask.NewMasker(append(append(append([]string{}, spot.Volatile...), st.Volatile...), currentVolatile(e, s.Chain)...))
		if masks.Masks(s.Path) {
			return ""
		}
		return rel(e.cfg.Root, e.store.SafeSpotPath(s.Chain))
	}
	return ""
}

func printObservedUnasserted(e *env, byName map[string]*chain.Chain, q chain.WhichQuery, seen []chain.WhichUnasserted) {
	fmt.Printf("no chain in %s %s; run records saw it on %s not asserting it:\n", e.chainsDir(), describeWhichQuery(q), plural(len(seen), "step"))
	for _, s := range seen {
		status := ""
		if s.Status != runner.StatusPassed {
			status = "  " + shownStatus(s.Status)
		}
		fmt.Printf("%s  %s  %s  got %s at %s%s\n", s.Chain, s.Step, shortRPC(s.Call), s.Code, s.Path, status)
		pins := siblingPins(byName[s.Chain], s)
		if len(pins) > 0 {
			fmt.Printf("  pins the same detail: %s, so a change fails shrt run\n", strings.Join(pins, "; "))
		}
		if spot := baselinedAt(e, s); spot != "" {
			fmt.Printf("  baselined by safe spot %s, so shrt verify reports a change\n", spot)
		} else if len(pins) == 0 {
			fmt.Println("  neither pinned nor baselined: a change goes unnoticed")
		}
		fmt.Printf("  reproduce: %s\n", s.Command)
	}
	fmt.Printf("To fail shrt run on a change and be listed here, assert %s at the path above.\n", q.Code)
}
