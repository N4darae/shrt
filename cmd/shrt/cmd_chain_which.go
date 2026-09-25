package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"sort"
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
	setUsage(fs, "usage: shrt chain which [-rpc <rpc>] [-code <n>] [-json]   which chains, or local run records, exercise an rpc or a failure code",
		"\nexit codes:\n  0  a chain or run record matched\n  1  nothing matched, or bad flags (neither -rpc nor -code, an unknown rpc)\n")
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q\n\nusage: shrt chain which [-rpc <rpc>] [-code <n>] [-json]", rest[0])
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
	keepRelatedWritesInPinnedRepro(e, lib, chains, hits)
	if len(hits) == 0 {
		seen := chain.WhichObservedUnasserted(chains, q, opts)
		if len(seen) == 0 {
			return fmt.Errorf("no chain in %s %s, and no local run record observed it\nnothing to slice: the corpus does not exercise it yet", e.chainsDir(), describeWhichQuery(q))
		}
		if *asJSON {
			return emitJSON(map[string]any{"chains": hits, "observed_unasserted": seen})
		}
		printObservedUnasserted(e.chainsDir(), q, seen, whichCoverOf(e, chains, seen))
		return nil
	}
	if *asJSON {
		return emitJSON(hits)
	}
	printWhich(hits, q, e.targetURL())
	return nil
}

func describeWhichQuery(q chain.WhichQuery) string {
	parts := []string{}
	if q.RPC != "" {
		parts = append(parts, "calls "+q.RPC)
	}
	if q.Code != "" {
		asserts := "asserts " + q.Code
		reasons, codes := []string{}, []string{}
		for _, a := range q.Aliases {
			if q.IsNumericCode() && !chain.IsDigits(a) {
				reasons = append(reasons, a)
			} else {
				codes = append(codes, a)
			}
		}
		if len(codes) > 0 {
			asserts += " or " + strings.Join(codes, " or ") + " (seen with it in a run record or a contract failure, so the same refusal)"
		}
		if len(reasons) > 0 {
			asserts += ", or only the reason " + strings.Join(reasons, " or ") + " (seen with it) on a step that asserts no code; a step asserting another code with that reason is not " + q.Code
		}
		parts = append(parts, asserts)
	}
	return strings.Join(parts, " and ")
}

func sliceSizeOf(e *env, lib *contract.Library) func(*chain.Chain, string) (int, bool) {
	opts := chain.SliceOptions{Mode: chain.SliceModeClosure, RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib)}
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
	var response any
	if len(s.Response) > 0 {
		_ = json.Unmarshal(s.Response, &response)
	}
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
	merged := make(map[string]any, len(obj)+1)
	for k, v := range obj {
		merged[k] = v
	}
	merged[chain.TransportPrefix] = outcome[chain.TransportPrefix]
	return merged
}

func keepRelatedWritesInPinnedRepro(e *env, lib *contract.Library, chains []*chain.Chain, hits []chain.WhichChain) {
	byName := map[string]*chain.Chain{}
	for _, c := range chains {
		byName[c.Name] = c
	}
	for i := range hits {
		h := &hits[i]
		c := byName[h.Chain]
		var best *chain.WhichStep
		for j := range h.Matches {
			if h.Matches[j].Step == h.Best {
				best = &h.Matches[j]
				break
			}
		}
		if c == nil || best == nil || best.Observed == nil || !strings.Contains(h.Command, " -mode pin -run ") {
			continue
		}
		run := best.Observed.Run
		rec, err := e.store.LoadRun(c.Name, run)
		if err != nil {
			continue
		}
		o := chain.SliceOptions{Mode: chain.SliceModePin, RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib), RunID: rec.RunID,
			Value: recordValues(rec), RunVars: recordVars(rec), Refused: refusedIn(rec), Performed: performedIn(rec)}
		res, err := chain.Slice(c, h.Best, o)
		if err != nil {
			continue
		}
		if related, _ := relatedDroppedWrites(res, rec); len(related) == 0 {
			continue
		}
		o.Keep = []string{chain.SliceKeepWrites}
		kept, err := chain.Slice(c, h.Best, o)
		if err != nil {
			continue
		}
		cmd := "shrt chain slice " + c.Name + " -step " + h.Best + " -mode pin -run " + run + " -keep " + chain.SliceKeepWrites
		names := append([]string{}, kept.FreshVars...)
		sort.Strings(names)
		for _, name := range names {
			cmd += " -var " + name + "=<fresh>"
		}
		h.Command = cmd
	}
}

func freshVarsOf(e *env, lib *contract.Library) func(*chain.Chain, string, string) []string {
	opts := chain.SliceOptions{Mode: chain.SliceModeClosure, RPCOf: rpcOf(e), Prereqs: contract.PrereqsFor(lib)}
	login := isLoginStep(e)
	return func(c *chain.Chain, step, run string) []string {
		o := opts
		if s, ok := c.Step(step); ok && run == "" && !chain.IsReadOnlyCall(s.Call) && !login(s) && !chain.IsAuthProbe(s) {
			o.Keep = []string{chain.SliceKeepWrites}
		}
		if run != "" {
			rec, err := e.store.LoadRun(c.Name, run)
			if err != nil {
				return nil
			}
			o.Mode, o.RunID, o.Value, o.RunVars = chain.SliceModePin, rec.RunID, recordValues(rec), recordVars(rec)
			o.Refused, o.Performed = refusedIn(rec), performedIn(rec)
		}
		res, err := chain.Slice(c, step, o)
		if err != nil {
			return nil
		}
		return res.FreshVars
	}
}

const (
	whichMarkSeen  = "OBSERVED"
	whichMarkClaim = "asserted"
)

func printWhich(hits []chain.WhichChain, q chain.WhichQuery, target string) {
	steps, observed := 0, 0
	for _, h := range hits {
		steps += len(h.Matches)
		if h.Observed {
			observed++
		}
	}
	fmt.Printf("%d chain(s), %d matching step(s)\n  %s\n", len(hits), steps, describeWhichQuery(q))
	for _, h := range hits {
		fmt.Println()
		fmt.Printf("%s  %d steps, %s\n", h.Chain, h.Steps, runCount(h.Runs))
		idW, codeW := 0, 0
		for _, m := range h.Matches {
			if n := len(m.Step); n > idW {
				idW = n
			}
			if n := len(whichCodeCell(m, q)); n > codeW {
				codeW = n
			}
		}
		for _, m := range h.Matches {
			mark := whichMarkClaim
			if m.Observed != nil {
				mark = whichMarkSeen
			}
			line := fmt.Sprintf("  %s  %-*s  asserts %-*s  slice %d/%d",
				mark, idW, m.Step, codeW, whichCodeCell(m, q), m.SliceSteps, h.Steps)
			if m.Kind == chain.WhichKindAuthProbe {
				line += "  auth probe"
			}
			if m.Observed == nil {
				fmt.Println(line)
				if m.ByReason != "" {
					fmt.Printf("    %s\n", byReasonNote(m, q))
				}
				if m.Newest != nil {
					fmt.Printf("    no local run reached it; newest run %s: %s\n", m.Newest.Run, whyNewestUnreached(m.Newest))
				}
				continue
			}
			fmt.Println(line)
			if m.ByReason != "" {
				fmt.Printf("    %s\n", byReasonNote(m, q))
			}
			fmt.Printf("    %s\n", whichSeenCell(m.Observed))
			for _, f := range m.Observed.Failures {
				fmt.Printf("    failed: %s\n", chain.DescribeFailure(f))
				if note, ok := blockedNote(h.Chain, m.Step, m.Observed.Run, f); ok {
					fmt.Printf("      %s\n", note)
				}
			}
			if m.Newest != nil {
				fmt.Printf("    newest run %s did not reach it: %s\n", m.Newest.Run, whyNewestUnreached(m.Newest))
			}
		}
		fmt.Printf("  reproduce: %s\n", h.Command)
	}
	fmt.Printf("\n%d chain(s), %d with a local run record that reached a matching step.\n", len(hits), observed)
	fmt.Printf("%s is what the chain claims; %s cites the newest local run record that reached the step, and \"got\" is\n"+
		"what its recorded response carried at the asserted path. A step marked FAILED did not produce what it asserts,\n"+
		"and its failing expectations follow. Under -rpc alone, a step whose newest reaching run FAILED there ranks first:\n"+
		"during an incident that is the one to slice. Under -code, one whose newest reaching run contradicts the assertion ranks last,\n"+
		"though its reproduce: line slices a step that FAILED when the chain has one.\n"+
		"Run records are machine-local, and only those recorded against this target (%s) are cited.\n",
		whichMarkClaim, whichMarkSeen, target)
	fmt.Println("slice k/n is the closure slice, the mode-independent cost; -mode pin can only be smaller.")
	fmt.Println("A write step is reproduced in closure mode with -keep writes, which creates what it needs afresh and keeps every earlier\n" +
		"write its state may depend on: -mode pin would re-send the write against the entities the recorded run created, which that\n" +
		"run already changed (a confirm answers AlreadyConfirmed). A step marked auth probe (skip_auth, auth: invalid, or a\n" +
		"transport refusal such as unauthenticated) is refused before it writes anything, so it is sliced plainly: earlier\n" +
		"writes do not change its verdict, and -keep writes would only add steps.")
}

func byReasonNote(m chain.WhichStep, q chain.WhichQuery) string {
	return fmt.Sprintf("matched by reason %s only: the step asserts no code, so it may expect a code other than %s that has the same reason", m.ByReason, q.Code)
}

func whyNewestUnreached(n *chain.WhichNewest) string {
	if n.StoppedAt != "" {
		return fmt.Sprintf("run stopped at step %s (%s)", n.StoppedAt, n.StoppedStatus)
	}
	return "step " + n.Status
}

func whichSeenCell(o *chain.WhichEvidence) string {
	status := o.Status
	if status != runner.StatusPassed {
		status = strings.ToUpper(status)
	}
	got := o.Code
	switch {
	case o.Asserted != "" && o.Path != o.Asserted && o.Code != "":
		got = fmt.Sprintf("%s at %s (nothing at %s)", o.Code, o.Path, o.Asserted)
	case o.Asserted != "" && o.Code == "":
		got = "nothing at " + o.Asserted
	case o.Code == "":
		got = "no code"
	}
	paths := []string{}
	for _, f := range o.Failures {
		if !slices.Contains(paths, f.Path) {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) > 0 {
		return fmt.Sprintf("run %s got %s, step %s on %s", o.Run, got, status, strings.Join(paths, ", "))
	}
	return fmt.Sprintf("run %s got %s, step %s", o.Run, got, status)
}

func runCount(n int) string {
	if n == 0 {
		return "no local runs"
	}
	return fmt.Sprintf("%d local run(s)", n)
}

func whichCodeCell(m chain.WhichStep, q chain.WhichQuery) string {
	if a, ok := chain.PrimaryAssertionFor(m.Asserts, q); ok {
		return a.Value
	}
	if q.Code != "" {
		return q.Code
	}
	return "-"
}

type whichCover struct {
	siblings  []string
	baselined string
}

func whichCoverOf(e *env, chains []*chain.Chain, seen []chain.WhichUnasserted) []whichCover {
	out := make([]whichCover, len(seen))
	for i, s := range seen {
		parent := ""
		if j := strings.LastIndex(s.Path, "."); j > 0 {
			parent = s.Path[:j+1]
		}
		for _, c := range chains {
			if c.Name != s.Chain {
				continue
			}
			for _, st := range c.Steps {
				if st == nil || st.ID != s.Step {
					continue
				}
				for _, x := range st.Expect {
					if x.Equals != nil && x.Path != s.Path && strings.HasPrefix(x.Path, parent) && !strings.Contains(x.Path[len(parent):], ".") &&
						chain.IsCodePath(x.Path) {
						out[i].siblings = append(out[i].siblings, fmt.Sprintf("%s equals %v", x.Path, x.Equals))
					}
				}
			}
		}
		out[i].baselined = baselinedAt(e, s)
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
		var body any
		if json.Unmarshal(st.Response, &body) != nil {
			return ""
		}
		v, ok := chain.Get(body, s.Path)
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

func printObservedUnasserted(dir string, q chain.WhichQuery, seen []chain.WhichUnasserted, cover []whichCover) {
	fmt.Printf("no chain in %s %s, but local run records observed it on %d step(s) that do not assert it:\n\n", dir, describeWhichQuery(q), len(seen))
	baselined, bare := 0, 0
	for i, s := range seen {
		status := s.Status
		if status != runner.StatusPassed {
			status = strings.ToUpper(status)
		}
		fmt.Printf("%s  step %d %s  %s\n    run %s got %s at %s, step %s\n",
			s.Chain, s.Index, s.Step, shortCall(s.Call), s.Run, s.Code, s.Path, status)
		if list := cover[i].siblings; len(list) > 0 {
			fmt.Printf("    pins the same detail: %s, so a different refusal there fails shrt run\n", strings.Join(list, "; "))
		}
		if spot := cover[i].baselined; spot != "" {
			baselined++
			fmt.Printf("    baselined: safe spot %s holds %s at %s, so shrt verify reports a change to it\n", spot, s.Code, s.Path)
		} else if len(cover[i].siblings) == 0 {
			bare++
		}
		fmt.Printf("    reproduce: %s\n", s.Command)
	}
	switch {
	case bare == 0 && baselined == len(seen):
		fmt.Printf("\nNo expectation pins %s itself, but each step above is baselined by its safe spot, so shrt verify catches a change to it;\n"+
			"shrt run alone does not. chain which lists only steps that assert the code: to be listed, and to fail shrt run as well,\n"+
			"assert it where it is the point of the step (path: the path above, equals: %s).\n", q.Code, q.Code)
	case bare == 0:
		fmt.Printf("\nNo expectation pins %s itself; a change is caught only as each line above says (by a sibling expectation on the same\n"+
			"detail, or by shrt verify against a safe spot). To pin it, assert it where it is the point of the step (path: the path above,\n"+
			"equals: %s), and chain which lists the step.\n", q.Code, q.Code)
	case bare < len(seen):
		fmt.Printf("\nNo expectation pins %s itself. Where a line above says baselined or pins the same detail, a change is caught as it says;\n"+
			"at the %d other step(s) a change to that answer goes unnoticed. Assert it where it is the point of the step\n"+
			"(path: the path above, equals: %s), and chain which lists the step.\n", q.Code, bare, q.Code)
	default:
		fmt.Printf("\nThe backend answered %s there, but no expectation pins it and no safe spot baselines it, so a change to that answer goes unnoticed.\n"+
			"Assert it where it is the point of the step (path: the path above, equals: %s), and chain which lists the step.\n", q.Code, q.Code)
	}
	fmt.Println("Run records are machine-local.")
}
