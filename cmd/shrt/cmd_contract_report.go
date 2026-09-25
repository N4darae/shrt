package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/store"
)

type statusRow struct {
	Domain    string   `json:"domain"`
	Total     int      `json:"total"`
	Covered   int      `json:"covered"`
	Reached   int      `json:"reached"`
	Verified  int      `json:"verified"`
	Todos     int      `json:"todos"`
	Gaps      int      `json:"gaps"`
	Score     int      `json:"score"`
	Uncovered []string `json:"uncovered,omitempty"`
	Orphans   []string `json:"orphans,omitempty"`
	Streaming []string `json:"streaming,omitempty"`

	SingleItem []contract.SingleItemRepeat `json:"single_item,omitempty"`
}

func contractStatus(args []string) error {
	fs := flag.NewFlagSet("contract status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	showGaps := fs.Bool("gaps", false, "list the gaps instead of the table: 'no contract' (no overlay entry), 'no path to' (a contract, but in no multi-step plan) and 'one item' (a repeated message field every chain sends with at most one item), then streaming rpcs, which are out of scope")
	phase := fs.String("phase", contract.PhaseAll, "score only one phase: happy (what a working chain needs), failure (refusal curation), or all")
	setUsage(fs, "usage: shrt contract status [-gaps] [-phase happy|failure|all] [-json]   contract-entry coverage per domain",
		"\nexit codes:\n  0  the table, or with -gaps the gap list, was printed\n"+
			"  1  a flag or -phase that cannot be parsed, a contract overlay that does not parse, or a setup that\n"+
			"     cannot load (no .shrt/config.yaml, a missing descriptor)\n")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if !contract.ValidPhase(*phase) {
		return fmt.Errorf("unknown -phase %q, want %s, %s or %s",
			*phase, contract.PhaseHappy, contract.PhaseFailure, contract.PhaseAll)
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, err := e.library()
	if err != nil {
		return err
	}
	todos := map[string]int{}
	for _, i := range contract.LintTodos(lib) {
		todos[i.Domain]++
	}

	quality := contract.MeasurePhase(lib, e.cat, "", *phase)
	gaps, scores := quality.GapsByDomain(), quality.ScoreByDomain()
	reached := reachableRPCs(lib, e.cat)
	chains, _, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	single := map[string][]contract.SingleItemRepeat{}
	for _, r := range contract.SingleItemRepeats(chains, e.cat) {
		single[r.RPC] = append(single[r.RPC], r)
	}
	byDomain := contract.Domains(e.cat.Methods())
	rows := []statusRow{}
	totals := statusRow{Domain: "TOTAL"}
	for _, d := range contract.DomainNames(e.cat.Methods()) {
		r := statusRow{Domain: d, Total: len(byDomain[d]), Todos: todos[d], Gaps: gaps[d], Score: scores[d]}
		for _, m := range byDomain[d] {
			r.SingleItem = append(r.SingleItem, single[m.FullName]...)
			c, ok := lib.Get(m.FullName)
			if !ok && m.Streaming() {
				r.Streaming = append(r.Streaming, m.FullName)
				continue
			}
			if !ok {
				r.Uncovered = append(r.Uncovered, m.FullName)
				continue
			}
			r.Covered++
			if c.Status == contract.StatusVerified {
				r.Verified++
			}
			switch {
			case m.Streaming():
				r.Streaming = append(r.Streaming, m.FullName)
			case reached[m.FullName]:
				r.Reached++
			default:
				r.Orphans = append(r.Orphans, m.FullName)
			}
		}
		totals.Total += r.Total
		totals.Covered += r.Covered
		totals.Reached += r.Reached
		totals.Verified += r.Verified
		totals.Todos += r.Todos
		totals.Gaps += r.Gaps
		totals.Score += r.Score
		rows = append(rows, r)
	}
	if *asJSON {
		return emitJSON(append(rows, totals))
	}
	if *showGaps {
		printStatusGaps(rows)
		return nil
	}
	const statusFormat = "%-14s %6s %9s %8s %9s %7s %6s %6s\n"
	fmt.Printf(statusFormat, "DOMAIN", "RPCS", "CONTRACT", "REACHED", "VERIFIED", "TODOS", "GAPS", "SCORE")
	line := func(r statusRow) {
		fmt.Printf("%-14s %6d %9d %8d %9d %7d %6d %6d\n",
			r.Domain, r.Total, r.Covered, r.Reached, r.Verified, r.Todos, r.Gaps, r.Score)
	}
	for _, r := range rows {
		line(r)
	}
	line(totals)
	fmt.Print("\nCONTRACT counts ENTRIES: a scaffold with nothing filled in counts, so it sits at its ceiling\n" +
		"the moment one lands and cannot tell you a contract is usable. VERIFIED counts the entries a\n" +
		"human set to 'status: verified'.\n" +
		"REACHED counts the rpcs that appear in some MULTI-STEP 'shrt contract plan' — as the target or\n" +
		"as a dependency of one. An rpc below it plans as a single step: nothing it needs is declared,\n" +
		"and nothing declares it as a producer. That is correct for a login or a read taking no id from\n" +
		"elsewhere, and a missing 'needs:' or 'from:' for a write that cannot run on its own. '-gaps'\n" +
		"lists them as 'no path to'; only you can say which kind each one is. A streaming rpc is never\n" +
		"REACHED: shrt is unary-only, so no plan can call it.\n" +
		"GAPS and SCORE measure the entries themselves, and score OMISSION as well as vagueness" +
		phaseScope(*phase) + ":\n" +
		scoringTerms(*phase) +
		"Per-rpc detail: shrt contract quality [-domain <domain>] [-phase happy]\n")
	if n := len(contract.SingleItemRepeats(chains, e.cat)); n > 0 {
		fmt.Printf("\n%d repeated request field(s) are sent with at most one item by every chain that sends them, "+
			"so per-item logic goes untested: shrt contract status -gaps lists them as 'one item'\n", n)
	}
	return nil
}

func printStatusGaps(rows []statusRow) {
	n := 0
	for _, r := range rows {
		for _, u := range r.Uncovered {
			fmt.Printf("no contract  %s\n", u)
			n++
		}
		for _, o := range r.Orphans {
			fmt.Printf("no path to   %s\n", o)
			n++
		}
	}
	for _, r := range rows {
		for _, one := range r.SingleItem {
			items := "1 item"
			if one.Most != 1 {
				items = fmt.Sprintf("%d items", one.Most)
			}
			fmt.Printf("one item     %s %s: at most %s in every chain that sends it (%s)\n",
				one.RPC, one.Field, items, strings.Join(clip(one.Chains, 4), ", "))
			n++
		}
	}
	for _, r := range rows {
		for _, st := range r.Streaming {
			fmt.Printf("streaming    %s  (out of scope: shrt is unary-only; not a gap, never REACHED)\n", st)
		}
	}
	if n == 0 {
		fmt.Println("no gaps: every rpc has a contract, every unary one appears in some multi-step plan, and " +
			"every repeated message field a chain sends is sent with two or more items somewhere")
		return
	}
	fmt.Print("\nno contract  the rpc has no entry in .shrt/contracts/: shrt contract init <domain>\n" +
		"no path to   it has a contract, but appears in no multi-step plan: nothing it needs is declared and\n" +
		"             nothing declares it as a producer. Right for a login or a read taking no id from elsewhere;\n" +
		"             a missing 'needs:' or 'from:' for a write that cannot run on its own.\n" +
		"one item     a repeated message field in a request, and no chain sends it with two or more items, so\n" +
		"             per-item logic (a total summed over lines, a check on the second item) is never\n" +
		"             exercised and a regression there passes every gate. Add a step, or a chain, that sends\n" +
		"             two items with different values and asserts what depends on both.\n")
}

func reachableRPCs(lib *contract.Library, cat *catalog.Catalog) map[string]bool {
	reached := map[string]bool{}
	for _, m := range cat.Methods() {
		p, err := contract.BuildPlan(m.FullName, lib, cat, "reachability")
		if err != nil || len(p.Order) < 2 {
			continue
		}
		for _, node := range p.Order {
			rpc, _ := contract.SplitNode(node)
			reached[rpc] = true
		}
	}
	return reached
}

func contractQuality(args []string) error {
	fs := flag.NewFlagSet("contract quality", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	only := fs.String("domain", "", "measure only this domain")
	gate := fs.Bool("gate", false, "ratchet the total score against -baseline: the score counts gaps, so lower is better; fail if it rises, and fail if it falls without the baseline being lowered")
	baseline := fs.String("baseline", "", "file holding the score the ratchet holds to")
	limit := fs.Int("limit", 40, "list at most this many rpcs, worst first")
	phase := fs.String("phase", contract.PhaseAll, "score only one phase: happy (what a working chain needs), failure (refusal curation), or all")
	setUsage(fs, "usage: shrt contract quality [-domain <d>] [-phase happy|failure|all] [-gate -baseline <file>] [-json]",
		"\nexit codes:\n"+
			"  0  the score was printed; under -gate, the score equals the baseline\n"+
			"  1  under -gate: the score is worse (higher) than the baseline, better without the baseline\n"+
			"     being lowered, or the baseline file is missing or unreadable; also an overlay that does\n"+
			"     not parse, or bad flags\n")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if !contract.ValidPhase(*phase) {
		return fmt.Errorf("unknown -phase %q, want %s, %s or %s",
			*phase, contract.PhaseHappy, contract.PhaseFailure, contract.PhaseAll)
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	lib, err := e.library()
	if err != nil {
		return err
	}
	report := contract.MeasurePhase(lib, e.cat, *only, *phase)

	if *gate && *phase != "" && *phase != contract.PhaseAll {
		return fmt.Errorf("-gate scores every phase, and %s records no phase, so pinning it to a "+
			"-phase %s number would ratchet the full score against a partial one — drop -phase, or "+
			"keep the phase view for reading rather than gating", *baseline, *phase)
	}
	if *asJSON {
		if err := emitJSON(report); err != nil {
			return err
		}
		if *gate {
			return qualityGate(report, *baseline)
		}
		return nil
	}
	if *gate {
		return qualityGate(report, *baseline)
	}

	if len(report.RPCs) == 0 {
		where := "this library"
		if *only != "" {
			where = "domain " + *only
		}
		fmt.Printf("contract quality%s — score 0: no rpc in %s has a measurable gap\n", phaseLabel(*phase), where)
		return nil
	}

	fmt.Printf("contract quality%s — %d rpc(s) with a measurable gap, total score %d\n\n",
		phaseLabel(*phase), len(report.RPCs), report.TotalScore)
	fmt.Printf("%5s  %-70s gap\n", "score", "rpc")
	shown := report.RPCs
	if *limit > 0 && len(shown) > *limit {
		shown = shown[:*limit]
	}
	for _, r := range shown {
		fmt.Printf("%5d  %-70s %s\n", r.Score, rpcTail(r.RPC), strings.Join(qualityGap(r, *phase), " | "))
	}
	if len(shown) < len(report.RPCs) {
		fmt.Printf("  … %d more\n", len(report.RPCs)-len(shown))
	}
	return nil
}

func phaseLabel(phase string) string {
	if phase == "" || phase == contract.PhaseAll {
		return ""
	}
	return " (" + phase + " phase)"
}

func phaseScope(phase string) string {
	if phase == "" || phase == contract.PhaseAll {
		return ""
	}
	return " (scoring the " + phase + " phase only)"
}

func scoringTerms(phase string) string {
	var b strings.Builder
	for _, t := range contract.QualityTerms() {
		if !t.InPhase(phase) {
			continue
		}
		fmt.Fprintf(&b, "  %d per %s [%s]\n", t.Weight, t.Label, t.Phase)
	}
	return b.String()
}

func qualityGap(r contract.QualityRPC, phase string) []string {
	bits := contract.GapReasons(r, phase)
	if phase == contract.PhaseAll {
		if n := len(r.UnfilledTodos); n > 0 {
			bits = append(bits, fmt.Sprintf("%d unfilled TODO: %s", n, strings.Join(clip(r.UnfilledTodos, 6), ", ")))
		}
	}
	return bits
}

func clip(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return append(append([]string{}, values[:max]...), "…")
}

func qualityGate(report contract.QualityReport, baselinePath string) error {
	total := report.TotalScore
	want, verdict, err := store.Ratchet(baselinePath, total)
	if err != nil {
		return err
	}
	uncovered, charged := []string{}, 0
	for _, r := range report.RPCs {
		if r.NoContract {
			uncovered = append(uncovered, rpcTail(r.RPC))
			charged += r.Score
		}
	}
	switch {
	case verdict == store.RatchetWorse && len(uncovered) > 0:
		rest := ""
		if charged < total {
			rest = fmt.Sprintf(" The other %d: run 'shrt contract quality' to see what got vaguer.", total-charged)
		}
		return fmt.Errorf("contract quality: score %d is worse than the baseline %d, and %d of it is charged to %d rpc(s) no overlay covers: %s.\n"+
			"An overlay or an entry was deleted, or the descriptor gained rpcs: restore it, or write one with 'shrt contract init <domain>'.%s",
			total, want, charged, len(uncovered), strings.Join(clip(uncovered, 8), ", "), rest)
	case verdict == store.RatchetWorse:
		return fmt.Errorf("contract quality: score %d is worse than the baseline %d. "+
			"Run 'shrt contract quality' to see what got vaguer", total, want)
	case verdict == store.RatchetBetter:
		return fmt.Errorf("contract quality: score %d beats the baseline %d — "+
			"lower %s to %d to keep the ratchet tight", total, want, baselinePath, total)
	}
	fmt.Printf("contract quality: score %d, at the baseline\n", total)
	return nil
}
