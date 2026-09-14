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
	hits := chain.Which(chains, q, chain.WhichOptions{
		RPCOf:        rpcOf(e),
		SliceOf:      sliceSizeOf(e),
		Observations: runObservations(e),
	})
	if len(hits) == 0 {
		return fmt.Errorf("no chain in %s %s\nnothing to slice: the corpus does not exercise it yet", e.chainsDir(), describeWhichQuery(q))
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
					Reached: s.Transport == nil && (s.Status == runner.StatusPassed || s.Status == runner.StatusFailed),
				}
				if len(s.Response) > 0 {
					_ = json.Unmarshal(s.Response, &o.Response)
				}
				out = append(out, o)
			}
		}
		return out
	}
}

const (
	whichLineWidth = 110
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
				continue
			}
			seen := whichSeenCell(m.Observed)
			if len(line)+2+len(seen) <= whichLineWidth {
				fmt.Printf("%s  %s\n", line, seen)
				continue
			}
			fmt.Println(line)
			fmt.Printf("  %s\n", seen)
		}
		fmt.Printf("  reproduce: %s\n", h.Command)
	}
	fmt.Printf("\n%d chain(s), %d with a local run record that reached a matching step.\n", len(hits), observed)
	fmt.Printf("%s is what the chain claims; %s means a local run record reached the step, and \"got\" is the code\n"+
		"its recorded response carried. A step marked FAILED did not produce what it asserts. Run records are machine-local.\n",
		whichMarkClaim, whichMarkSeen)
	fmt.Println("slice k/n is the closure slice, the mode-independent cost; -mode pin can only be smaller.")
}

func whichSeenCell(o *chain.WhichEvidence) string {
	got := o.Code
	if got == "" {
		got = "no code"
	}
	status := o.Status
	if status != runner.StatusPassed {
		status = strings.ToUpper(status)
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
	if q.Code != "" {
		for _, a := range m.Asserts {
			if strings.EqualFold(a.Value, q.Code) {
				return a.Value
			}
		}
		return q.Code
	}
	if len(m.Asserts) == 0 {
		return "-"
	}
	for _, a := range m.Asserts {
		if isAllDigits(a.Value) {
			return a.Value
		}
	}
	return m.Asserts[0].Value
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}
