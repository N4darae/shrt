package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/contract"
	"github.com/N4darae/shrt/store"
)

func contractStatus(args []string) error {
	fs := flag.NewFlagSet("contract status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	showGaps := fs.Bool("gaps", false, "list the rpcs that have no curated contract")
	phase := fs.String("phase", contract.PhaseAll, "score only one phase: happy (what a working chain needs), failure (refusal curation), or all")
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
	lib, brokenOverlays, err := e.library()
	if err != nil {
		return err
	}
	if len(brokenOverlays) > 0 {
		return fmt.Errorf("%d overlay(s) did not parse, so every rpc they cover is counted as having no "+
			"contract at all. This table would report that as coverage you do not have — run "+
			"'shrt contract lint' to see which, and fix them before reading these numbers", len(brokenOverlays))
	}
	todos := map[string]int{}
	for _, i := range contract.LintTodos(lib) {
		todos[i.Domain]++
	}

	type row struct {
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
	}
	quality := contract.MeasurePhase(lib, e.cat, "", *phase)
	gaps, scores := quality.GapsByDomain(), quality.ScoreByDomain()
	reached := reachableRPCs(lib, e.cat)
	byDomain := contract.Domains(e.cat.Methods())
	rows := []row{}
	totals := row{Domain: "TOTAL"}
	for _, d := range contract.DomainNames(e.cat.Methods()) {
		r := row{Domain: d, Total: len(byDomain[d]), Todos: todos[d], Gaps: gaps[d], Score: scores[d]}
		for _, m := range byDomain[d] {
			c, ok := lib.Get(m.FullName)
			if !ok {
				r.Uncovered = append(r.Uncovered, m.FullName)
				continue
			}
			r.Covered++
			if c.Status == contract.StatusVerified {
				r.Verified++
			}
			if reached[m.FullName] {
				r.Reached++
			} else {
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
	const statusFormat = "%-14s %6s %9s %8s %9s %7s %6s %6s\n"
	fmt.Printf(statusFormat, "DOMAIN", "RPCS", "CONTRACT", "REACHED", "VERIFIED", "TODOS", "GAPS", "SCORE")
	line := func(r row) {
		fmt.Printf("%-14s %6d %9d %8d %9d %7d %6d %6d\n",
			r.Domain, r.Total, r.Covered, r.Reached, r.Verified, r.Todos, r.Gaps, r.Score)
	}
	for _, r := range rows {
		line(r)
	}
	line(totals)
	fmt.Print("\nCONTRACT, REACHED and VERIFIED count ENTRIES: a scaffold with nothing filled in counts.\n" +
		"They sit at their ceiling the moment one lands, so they cannot tell you a contract is usable.\n" +
		"REACHED counts the rpcs that appear in some MULTI-STEP 'shrt contract plan' — as the target or\n" +
		"as a dependency of one. An rpc below it plans as a single step: nothing it needs is declared,\n" +
		"and nothing declares it as a producer. That is correct for a login or a read taking no id from\n" +
		"elsewhere, and a missing 'needs:' or 'from:' for a write that cannot run on its own. '-gaps'\n" +
		"lists them as 'no path to'; only you can say which kind each one is.\n" +
		"GAPS and SCORE measure the entries themselves, and score OMISSION as well as vagueness" +
		phaseScope(*phase) + ":\n" +
		scoringTerms(*phase) +
		"Per-rpc detail: shrt contract quality [-domain <domain>] [-phase happy]\n")
	if *showGaps {
		fmt.Println()
		for _, r := range rows {
			for _, u := range r.Uncovered {
				fmt.Printf("  no contract  %s\n", u)
			}
			for _, o := range r.Orphans {
				fmt.Printf("  no path to   %s\n", o)
			}
		}
	}
	return nil
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
	gate := fs.Bool("gate", false, "ratchet the total score against -baseline: fail if it rises, and fail if it falls without the baseline being lowered")
	baseline := fs.String("baseline", "", "file holding the score the ratchet holds to")
	limit := fs.Int("limit", 40, "list at most this many rpcs, worst first")
	phase := fs.String("phase", contract.PhaseAll, "score only one phase: happy (what a working chain needs), failure (refusal curation), or all")
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
	lib, brokenOverlays, err := e.library()
	if err != nil {
		return err
	}
	if len(brokenOverlays) > 0 {
		return fmt.Errorf("%d overlay(s) did not parse, so every rpc they cover is absent from this score. "+
			"A score computed over half a library is not a lower score, it is a different question — run "+
			"'shrt contract lint' to see which, and fix them before reading this number", len(brokenOverlays))
	}
	report := contract.MeasurePhase(lib, e.cat, *only, *phase)

	if *asJSON {
		return emitJSON(report)
	}
	if *gate {
		if *phase != "" && *phase != contract.PhaseAll {
			return fmt.Errorf("-gate scores every phase, and %s records no phase, so pinning it to a "+
				"-phase %s number would ratchet the full score against a partial one — drop -phase, or "+
				"keep the phase view for reading rather than gating", *baseline, *phase)
		}
		return qualityGate(report.TotalScore, *baseline)
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

func qualityGate(total int, baselinePath string) error {
	want, verdict, err := store.Ratchet(baselinePath, total)
	if err != nil {
		return err
	}
	switch verdict {
	case store.RatchetWorse:
		return fmt.Errorf("contract quality: score %d is worse than the baseline %d. "+
			"Run 'shrt contract quality' to see what got vaguer", total, want)
	case store.RatchetBetter:
		return fmt.Errorf("contract quality: score %d beats the baseline %d — "+
			"lower %s to %d to keep the ratchet tight", total, want, baselinePath, total)
	}
	fmt.Printf("contract quality: score %d, at the baseline\n", total)
	return nil
}
