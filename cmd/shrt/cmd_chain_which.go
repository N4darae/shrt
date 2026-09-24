package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/runner"
)

func chainWhich(args []string) error {
	fs := flag.NewFlagSet("chain which", flag.ContinueOnError)
	rpc := fs.String("rpc", "", "chains with a step calling this rpc, as package.Service/Rpc, Service/Rpc or a bare Rpc")
	code := fs.String("code", "", "chains asserting this app_code, envelope code or failure reason")
	asJSON := fs.Bool("json", false, "emit JSON")
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
	opts := chain.WhichOptions{
		RPCOf:        rpcOf(e),
		SliceOf:      sliceSizeOf(e),
		Observations: runObservations(e),
		FreshVars:    freshVarsOf(e),
	}
	hits := chain.Which(chains, q, opts)
	if len(hits) == 0 {
		seen := chain.WhichObservedUnasserted(chains, q, opts)
		if len(seen) == 0 {
			return fmt.Errorf("no chain in %s %s, and no local run record observed it\nnothing to slice: the corpus does not exercise it yet", e.chainsDir(), describeWhichQuery(q))
		}
		if *asJSON {
			return emitJSON(map[string]any{"chains": hits, "observed_unasserted": seen})
		}
		printObservedUnasserted(e.chainsDir(), q, seen)
		return nil
	}
	if *asJSON {
		return emitJSON(hits)
	}
	printWhich(hits, q)
	return nil
}

func describeWhichQuery(q chain.WhichQuery) string {
	parts := []string{}
	if q.RPC != "" {
		parts = append(parts, "calls "+q.RPC)
	}
	if q.Code != "" {
		parts = append(parts, "asserts "+q.Code)
	}
	return strings.Join(parts, " and ")
}

func sliceSizeOf(e *env) func(*chain.Chain, string) (int, bool) {
	opts := chain.SliceOptions{Mode: chain.SliceModeClosure, RPCOf: rpcOf(e)}
	if lib, _, err := e.library(); err == nil && lib != nil {
		opts.Prereqs = contract.PrereqsFor(lib)
	}
	return func(c *chain.Chain, step string) (int, bool) {
		res, err := chain.Slice(c, step, opts)
		if err != nil {
			return 0, false
		}
		return len(res.Kept), true
	}
}

func runObservations(e *env) func(string) []chain.Observation {
	return func(name string) []chain.Observation {
		ids, err := e.store.ListRuns(name)
		if err != nil || len(ids) == 0 {
			return nil
		}
		out := []chain.Observation{}
		for _, id := range ids {
			rec, err := e.store.LoadRun(name, id)
			if err != nil {
				continue
			}
			for _, s := range rec.Steps {
				o := chain.Observation{
					Run:     rec.RunID,
					Step:    s.ID,
					Status:  s.Status,
					Reached: s.Status == runner.StatusPassed || s.Status == runner.StatusFailed,
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

func freshVarsOf(e *env) func(*chain.Chain, string, string) []string {
	opts := chain.SliceOptions{Mode: chain.SliceModeClosure, RPCOf: rpcOf(e)}
	if lib, _, err := e.library(); err == nil && lib != nil {
		opts.Prereqs = contract.PrereqsFor(lib)
	}
	return func(c *chain.Chain, step, run string) []string {
		o := opts
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

func printWhich(hits []chain.WhichChain, q chain.WhichQuery) {
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
			if m.Observed == nil {
				fmt.Println(line)
				if m.Newest != nil {
					fmt.Printf("    no local run reached it; newest run %s: step %s\n", m.Newest.Run, m.Newest.Status)
				}
				continue
			}
			fmt.Println(line)
			fmt.Printf("    %s\n", whichSeenCell(m.Observed))
			for _, f := range m.Observed.Failures {
				fmt.Printf("    failed: %s\n", chain.DescribeFailure(f))
			}
			if m.Newest != nil {
				fmt.Printf("    newest run %s did not reach it: step %s\n", m.Newest.Run, m.Newest.Status)
			}
		}
		fmt.Printf("  reproduce: %s\n", h.Command)
	}
	fmt.Printf("\n%d chain(s), %d with a local run record that reached a matching step.\n", len(hits), observed)
	fmt.Printf("%s is what the chain claims; %s cites the newest local run record that reached the step, and \"got\" is\n"+
		"what its recorded response carried at the asserted path. A step marked FAILED did not produce what it asserts,\n"+
		"and its failing expectations follow. Steps whose newest reaching run contradicts the assertion rank last.\n"+
		"Run records are machine-local.\n",
		whichMarkClaim, whichMarkSeen)
	fmt.Println("slice k/n is the closure slice, the mode-independent cost; -mode pin can only be smaller.")
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
	return fmt.Sprintf("run %s got %s, step %s", o.Run, got, status)
}

func runCount(n int) string {
	if n == 0 {
		return "no local runs"
	}
	return fmt.Sprintf("%d local run(s)", n)
}

func whichCodeCell(m chain.WhichStep, q chain.WhichQuery) string {
	if a, ok := chain.PrimaryAssertion(m.Asserts, q.Code); ok {
		return a.Value
	}
	if q.Code != "" {
		return q.Code
	}
	return "-"
}

func printObservedUnasserted(dir string, q chain.WhichQuery, seen []chain.WhichUnasserted) {
	fmt.Printf("no chain in %s %s, but local run records observed it on %d step(s) that do not assert it:\n\n", dir, describeWhichQuery(q), len(seen))
	for _, s := range seen {
		status := s.Status
		if status != runner.StatusPassed {
			status = strings.ToUpper(status)
		}
		fmt.Printf("%s  step %d %s  %s\n    run %s got %s at %s, step %s\n    reproduce: %s\n",
			s.Chain, s.Index, s.Step, shortCall(s.Call), s.Run, s.Code, s.Path, status, s.Command)
	}
	fmt.Printf("\nThe backend answered %s there, but no expectation pins it, so a change to that answer goes unnoticed.\n"+
		"Assert it where it is the point of the step (path: the path above, equals: %s), and chain which lists the step.\n"+
		"Run records are machine-local.\n", q.Code, q.Code)
}
