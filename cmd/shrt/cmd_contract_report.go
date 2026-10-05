package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
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
	FirstOnly []string `json:"first_message_only,omitempty"`
	NoChain   []string `json:"no_chain,omitempty"`

	SingleItem []contract.SingleItemRepeat `json:"single_item,omitempty"`
	ProbeGaps  []contract.ProbeGap         `json:"probe_gaps,omitempty"`
	EmptyGaps  []contract.EmptyFilterGap   `json:"empty_filter_gaps,omitempty"`
	LoginGaps  []contract.LoginFailureGap  `json:"login_failure_gaps,omitempty"`
	StateGaps  []contract.StateGap         `json:"state_gaps,omitempty"`
}

func contractStatus(args []string) error {
	fs := flag.NewFlagSet("contract status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	showGaps := fs.Bool("gaps", false, "list the gaps instead of the table: no contract, no path to, one item, same resource, no repeat, no empty filter, no login probe, no chain, no state, no role probe, no profile probe, no token, first message, streaming; -v explains each kind in full")
	phase := fs.String("phase", contract.PhaseAll, "score only one phase: happy (what a working chain needs), failure (refusal curation), or all")
	verbose := fs.Bool("v", false, "explain each column and the scoring under the table; with -gaps, each kind of gap in full")
	setUsage(fs, "usage: shrt contract status [-v] [-gaps] [-phase happy|failure|all] [-json]   contract-entry coverage per domain",
		"\nexit codes:\n  0  the table, or with -gaps the gap list, was printed\n"+
			"  1  a flag or -phase that cannot be parsed, a contract overlay that does not parse, or a setup that\n"+
			"     cannot load (no .shrt/config.yaml, a missing descriptor)\n")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if err := contract.CheckPhase(*phase); err != nil {
		return err
	}
	e, lib, err := loadLibrary()
	if err != nil {
		return err
	}
	todos := map[string]int{}
	for _, i := range contract.LintTodos(lib) {
		todos[i.Domain]++
	}

	quality := contract.MeasurePhase(lib, e.cat, "", *phase)
	gaps, scores := quality.GapsByDomain(), quality.ScoreByDomain()
	reached, plans := reachableRPCs(lib, e.cat)
	chains, _, err := chain.LoadDirPartial(e.chainsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stateGaps := byRPC(contract.StateGaps(chains, plans, lib, e.cat), func(g contract.StateGap) string { return g.RPC })
	single := byRPC(contract.SingleItemRepeats(chains, e.cat), func(r contract.SingleItemRepeat) string { return r.RPC })
	called := calledRPCs(e, chains)
	logins := loginRPCs(e)
	probeGaps := byRPC(contract.AuthProbeGaps(chains, lib, e.cat, planOptions(e)), func(g contract.ProbeGap) string { return g.RPC })
	emptyGaps := byRPC(contract.EmptyFilterGaps(chains, lib, e.cat), func(g contract.EmptyFilterGap) string { return g.RPC })
	loginGaps := byRPC(contract.LoginFailureGaps(chains, lib, e.cat, sortedKeys(logins)), func(g contract.LoginFailureGap) string { return g.RPC })
	byDomain := contract.Domains(e.cat.Methods())
	rows := []statusRow{}
	totals := statusRow{Domain: "TOTAL"}
	for _, d := range contract.DomainNames(e.cat.Methods()) {
		r := statusRow{Domain: d, Total: len(byDomain[d]), Todos: todos[d], Gaps: gaps[d], Score: scores[d]}
		for _, m := range byDomain[d] {
			r.SingleItem = append(r.SingleItem, single[m.FullName]...)
			r.ProbeGaps = append(r.ProbeGaps, probeGaps[m.FullName]...)
			r.EmptyGaps = append(r.EmptyGaps, emptyGaps[m.FullName]...)
			r.LoginGaps = append(r.LoginGaps, loginGaps[m.FullName]...)
			r.StateGaps = append(r.StateGaps, stateGaps[m.FullName]...)
			if m.StreamRefusal() == "" && !called[m.FullName] {
				r.NoChain = append(r.NoChain, noChainLine(m))
			}
			if m.ServerStreaming && m.StreamRefusal() == "" {
				r.FirstOnly = append(r.FirstOnly, m.FullName)
			}
			c, ok := lib.Get(m.FullName)
			if !ok && m.StreamRefusal() != "" {
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
			case m.StreamRefusal() != "":
				r.Streaming = append(r.Streaming, m.FullName)
			case reached[m.FullName]:
				r.Reached++
			case logins[m.FullName]:
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
		printStatusGaps(rows, *verbose)
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
	if *verbose {
		fmt.Print("\nCONTRACT counts ENTRIES: a scaffold with nothing filled in counts, so it sits at its ceiling\n" +
			"the moment one lands and cannot tell you a contract is usable. VERIFIED counts the entries a\n" +
			"human set to 'status: verified'.\n" +
			"REACHED counts the rpcs that appear in some MULTI-STEP 'shrt contract plan' — as the target or\n" +
			"as a dependency of one. An rpc below it plans as a single step: nothing it needs is declared,\n" +
			"and nothing declares it as a producer. That is correct for a login or a read taking no id from\n" +
			"elsewhere, and a missing 'needs:' or 'from:' for a write that cannot run on its own. '-gaps'\n" +
			"lists them as 'no path to'; only you can say which kind each one is. A client- or bidi-streaming\n" +
			"rpc is never REACHED: shrt cannot call it.\n" +
			"GAPS and SCORE measure the entries themselves, and score OMISSION as well as vagueness" +
			phaseNote(*phase, " (scoring the %s phase only)") + ":\n" +
			scoringTerms(*phase) +
			"Per-rpc detail: shrt contract quality [-domain <domain>] [-phase happy]\n")
	} else {
		fmt.Println("\nwhat each column counts, and how GAPS and SCORE are scored: shrt contract status -v")
	}
	counts := map[string]int{}
	for _, r := range contract.SingleItemRepeats(chains, e.cat) {
		switch {
		case r.NoRepeat:
			counts["no repeat"]++
		case r.SameResource:
			counts["same resource"]++
		default:
			counts["one item"]++
		}
	}
	for _, r := range rows {
		counts["no chain"] += len(r.NoChain)
		counts["no state"] += len(r.StateGaps)
		for _, g := range r.ProbeGaps {
			counts[cmp.Or(map[string]string{"role": "no role probe", "parity": "no profile probe"}[g.Kind], "no token")]++
		}
	}
	for _, s := range statusSummaries {
		if n := counts[s.kind]; n > 0 {
			fmt.Printf("\n%d %s: shrt contract status -gaps lists them as '%s'\n", n, s.text, s.kind)
		}
	}
	return nil
}

var statusSummaries = []struct{ kind, text string }{
	{"one item", "repeated request field(s) are sent with at most one item by every chain that sends them, so per-item logic goes untested"},
	{"same resource", "repeated request field(s) are sent with two or more items only when every item points at the same " +
		"resource, so per-item logic that reads each item's own resource goes untested"},
	{"no repeat", "repeated request field(s) are never sent with one resource on two applied items, so logic that merges, " +
		"deduplicates or counts once per resource goes untested"},
	{"no chain", "rpc(s) are called by no chain, so no run or gate exercises them"},
	{"no state", "write/state pair(s) are called by no chain from a state the write's plan calls it from, or not with every item count it sends there, " +
		"so logic that depends on the state it starts from goes untested"},
	{"no profile probe", "rpc/profile pair(s) whose contract lets every role call the rpc are never called as that profile, so a role check added by mistake passes every gate"},
	{"no role probe", "role-gated rpc/profile pair(s) are never called as a profile lacking the role, so a dropped role check passes every gate"},
	{"no token", "chained rpc(s) are never called without a token or with auth: invalid"},
}

func byRPC[T any](items []T, rpc func(T) string) map[string][]T {
	out := map[string][]T{}
	for _, it := range items {
		out[rpc(it)] = append(out[rpc(it)], it)
	}
	return out
}

var gapNext = []struct{ kind, next string }{
	{"no contract", "shrt contract init <domain>"},
	{"no path to", "a needs: or from:"},
	{"one item", "a step sending two items with different values"},
	{"same resource", "shrt contract plan <rpc>"},
	{"no repeat", "shrt contract plan <rpc>"},
	{"no empty filter", "shrt contract plan <rpc>"},
	{"no login probe", "shrt contract plan <rpc>"},
	{"no chain", "shrt contract plan <rpc>"},
	{"no state", "shrt contract plan <rpc> -write <file>.yaml"},
	{"no role probe", "shrt contract plan <rpc>"},
	{"no profile probe", "shrt contract plan <rpc>"},
	{"no token", "shrt contract plan <rpc>"},
	{"streaming", "a test of your own"},
}

func printStatusGaps(rows []statusRow, verbose bool) {
	found := map[string]bool{}
	gap := func(kind, format string, args ...any) {
		found[kind] = true
		fmt.Printf(format, args...)
	}
	for _, r := range rows {
		for _, u := range r.Uncovered {
			gap("no contract", "no contract  %s\n", u)
		}
		for _, o := range r.Orphans {
			gap("no path to", "no path to   %s\n", o)
		}
	}
	for _, r := range rows {
		for _, one := range r.SingleItem {
			if one.NoRepeat {
				gap("no repeat", "no repeat    %s %s: no chain sends one resource on two items that both apply; a refused item or step does not count (%s)\n",
					one.RPC, one.Field, strings.Join(clip(one.Chains, 4), ", "))
				continue
			}
			if one.SameResource {
				gap("same resource", "same resource %s %s: every chain that sends two or more items points them all at %s (%s)\n",
					one.RPC, one.Field, one.Resource, strings.Join(clip(one.Chains, 4), ", "))
				continue
			}
			items := "1 item"
			if one.Most != 1 {
				items = fmt.Sprintf("%d items", one.Most)
			}
			gap("one item", "one item     %s %s: at most %s in every chain that sends it (%s)\n",
				one.RPC, one.Field, items, strings.Join(clip(one.Chains, 4), ", "))
		}
	}
	for _, r := range rows {
		for _, g := range r.EmptyGaps {
			gap("no empty filter", "no empty filter %s %s: its contract says an empty %s lists all, and every chain sends it set (%s)\n",
				g.RPC, g.Field, g.Field, strings.Join(clip(g.Chains, 4), ", "))
		}
	}
	for _, r := range rows {
		for _, g := range r.LoginGaps {
			gap("no login probe", "no login probe %s: its contract declares %s, and no chain expects it; the config's own logins only succeed\n", g.RPC, g.Failure)
		}
	}
	for _, r := range rows {
		for _, line := range r.NoChain {
			gap("no chain", "no chain     %s\n", line)
		}
	}
	for _, r := range rows {
		for _, g := range r.StateGaps {
			gap("no state", "no state     %s, though %s: %s\n", g.Line(), cmp.Or(g.Why, "its plan calls it so"), gapPlan(g.RPC))
		}
	}
	for _, r := range rows {
		for _, g := range r.ProbeGaps {
			if g.Kind == "role" {
				gap("no role probe", "no role probe %s: requires %s, and no chain calls it as profile %s\n", g.RPC, g.Roles, g.Profile)
			} else if g.Kind == "parity" {
				gap("no profile probe", "no profile probe %s: its contract lets every role call it, and no chain calls it as profile %s\n", g.RPC, g.Profile)
			} else {
				gap("no token", "no token     %s: no chain calls it with skip_auth: true or auth: invalid\n", g.RPC)
			}
		}
	}
	for _, r := range rows {
		for _, rpc := range r.FirstOnly {
			gap("first message", "%s\n", contract.StreamingGap(rpc))
		}
	}
	for _, r := range rows {
		for _, st := range r.Streaming {
			gap("streaming", "streaming    %s: client- or bidi-streaming, shrt cannot call it; not covered by any chain\n", st)
		}
	}
	if len(found) == 0 {
		fmt.Println("no gaps: every rpc has a contract, every callable one appears in some multi-step plan and is called by " +
			"some chain, every repeated message field a chain sends is sent with two or more items, pointing at different resources somewhere and at one resource twice somewhere, every list filter whose contract says empty lists all is sent empty somewhere, every failure a login's contract declares is expected somewhere, " +
			"every write is called from each state its plan calls it from, with each item count, " +
			"and every chained rpc is called without a token and, when role-gated, as each profile lacking the role")
		return
	}
	if verbose {
		fmt.Print(statusGapLegend)
		return
	}
	var kinds grouped[string]
	for _, m := range gapNext {
		if found[m.kind] {
			kinds.add(m.next, m.kind)
		}
	}
	parts := []string{}
	for _, next := range kinds.keys {
		parts = append(parts, next+" ("+strings.Join(kinds.of[next], ", ")+")")
	}
	if len(parts) > 0 {
		fmt.Println("next: " + strings.Join(parts, "; "))
	}
}

const statusGapLegend = "\nno contract  the rpc has no entry in .shrt/contracts/: shrt contract init <domain>\n" +
	"no path to   it has a contract, but appears in no multi-step plan: nothing it needs is declared and\n" +
	"             nothing declares it as a producer. Right for a read taking no id from elsewhere; a\n" +
	"             missing 'needs:' or 'from:' for a write that cannot run on its own. A login the config's\n" +
	"             auth calls needs no path and is never listed.\n" +
	"one item     a repeated message field in a request, and no chain sends it with two or more items, so\n" +
	"             per-item logic (a total summed over lines, a check on the second item) is never\n" +
	"             exercised and a regression there passes every gate. Add a step, or a chain, that sends\n" +
	"             two items with different values and asserts what depends on both.\n" +
	"same resource a repeated message field that chains send with two or more items, but every item\n" +
	"             of every such step points at the same resource (the same ${step...} reference or\n" +
	"             literal id), so logic that uses each item's own resource (a price per line) is never\n" +
	"             exercised: a backend that applies the first item's product to every line passes. Point\n" +
	"             the second item at a second producer step with different values (shrt contract plan\n" +
	"             scaffolds one).\n" +
	"no repeat    a repeated message field whose items carry a resource, but no chain ever sends one\n" +
	"             resource on two items that both apply (a refused item or step does not count), so a\n" +
	"             backend that merges, deduplicates or counts once per resource (stock taken once for a\n" +
	"             product on two lines) passes. Send the same resource on two items with different\n" +
	"             quantities and assert what depends on both (shrt contract plan scaffolds\n" +
	"             <step>_same_<noun>_twice).\n" +
	"no empty filter the contract says an empty (or absent) value of a list's filter lists everything,\n" +
	"             but every chain sends it set, so a backend whose empty filter returns nothing passes.\n" +
	"             Send it empty and assert the fixtures the chain created are among the items by id\n" +
	"             (includes:); shrt contract plan <list rpc> scaffolds <step>_empty_<field>.\n" +
	"no login probe a failure the login rpc's contract declares (BadCredentials) that no chain step\n" +
	"             expects. The config's auth: block calls the login in every run, but only ever with the\n" +
	"             right credentials, so that counts as calling it, not as probing its failures or the role\n" +
	"             it returns: shrt contract plan <login rpc> scaffolds <step>_bad_password and one login\n" +
	"             per profile asserting its role.\n" +
	"no chain     no chain under paths.chains calls the rpc (a login the config's auth calls counts as\n" +
	"             called), so no run, verify or gate exercises it, and a repeated field it takes is never\n" +
	"             sent at all. Write a chain that calls it: shrt contract plan <rpc>.\n" +
	"no state     the write's own plan calls it on a record in a state (an order in PENDING), or with an item\n" +
	"             count there, that no chain does; chains are read step by step for the state each record\n" +
	"             is in. A needs: whose write only takes the record to the state a restore: names leaves\n" +
	"             the state before it valid too, and plan calls the write from both. A backend that\n" +
	"             refuses or miscounts from the state no chain reaches passes every gate. shrt gate -repro\n" +
	"             plans and runs it when a gated chain calls the write; shrt contract plan <rpc> -write\n" +
	"             <file>.yaml writes the plan to .shrt/scratch/, never over a chain, to run by path.\n" +
	"no role probe the contract's requires_role names a role the profile's name is not, and no chain calls the\n" +
	"             rpc with auth: <profile>, so a role check that was dropped passes every gate. shrt contract\n" +
	"             plan <rpc> scaffolds <step>_as_<profile> expecting the declared denial, with reads proving\n" +
	"             it changed nothing.\n" +
	"no profile probe the contract lets every role call the rpc and no chain calls it with auth: <profile>, so\n" +
	"             a role check added by mistake passes every gate. shrt contract plan <rpc> scaffolds\n" +
	"             <step>_as_<profile> asserting what the default profile's call answered or left.\n" +
	"no token     no chain calls the rpc with skip_auth: true or auth: invalid, so an rpc that stopped\n" +
	"             checking the token passes. A plan scaffolds one pair for each target rpc.\n" +
	"streaming    shrt cannot call a client- or bidi-streaming rpc, so no chain covers it and a defect in it\n" +
	"             (a missing auth check, say) passes every gate. Cover it with a test of your own. A\n" +
	"             server-streaming rpc is called like a unary one and listed under 'no chain' and 'no token'\n" +
	"             until a chain calls it; its token probe matters most, as a unary-only auth interceptor skips it.\n"

func loginRPCs(e *env) map[string]bool {
	out := map[string]bool{}
	for _, p := range e.cfg.AuthProfiles() {
		if p == nil || p.Call == "" {
			continue
		}
		if m, err := e.cat.Lookup(p.Call); err == nil {
			out[m.FullName] = true
		}
	}
	return out
}

func calledRPCs(e *env, chains []*chain.Chain) map[string]bool {
	out := loginRPCs(e)
	for _, c := range chains {
		if c == nil {
			continue
		}
		for _, s := range c.Steps {
			if s == nil {
				continue
			}
			if m, err := e.cat.Lookup(s.Call); err == nil {
				out[m.FullName] = true
			}
		}
	}
	return out
}

func noChainLine(m *catalog.Method) string {
	repeated := []string{}
	for _, f := range catalog.DescribeMessage(m.Input()).Fields {
		if f.Repeated && f.MapKey == "" && (f.Kind == "message" || f.Kind == "group") {
			repeated = append(repeated, f.Name)
		}
	}
	if len(repeated) == 0 {
		return m.FullName
	}
	return m.FullName + " (repeated request field(s) no chain sends at all: " + strings.Join(repeated, ", ") + ")"
}

func reachableRPCs(lib *contract.Library, cat *catalog.Catalog) (map[string]bool, map[string]*contract.Plan) {
	reached, plans := map[string]bool{}, map[string]*contract.Plan{}
	for _, m := range cat.Methods() {
		p, err := contract.BuildPlan(m.FullName, lib, cat, "reachability")
		if err != nil {
			continue
		}
		plans[m.FullName] = p
		if len(p.Order) < 2 {
			continue
		}
		for _, node := range p.Order {
			rpc, _ := contract.SplitNode(node)
			reached[rpc] = true
		}
	}
	return reached, plans
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
			"     being lowered, or the baseline file is missing or unreadable; also a contract error\n"+
			"     (contract lint lists it: no score is given), an overlay that does\n"+
			"     not parse, or bad flags\n")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if err := contract.CheckPhase(*phase); err != nil {
		return err
	}
	e, lib, err := loadLibrary()
	if err != nil {
		return err
	}
	lintErrors := 0
	for _, i := range contract.LintAll(lib, e.cat, e.cfg.AuthProfileNames()) {
		if i.Severity == contract.SeverityError && (*only == "" || i.Domain == *only || (i.Domain == "" && strings.Contains(i.Message, *only))) {
			lintErrors++
		}
	}
	if lintErrors > 0 {
		return fmt.Errorf("%d contract error(s), which 'shrt contract lint' lists: quality scores the gaps of contracts that lint, "+
			"and an error is a gap no score measures, so it gives no score; fix them, then run it again", lintErrors)
	}
	report := contract.MeasurePhase(lib, e.cat, *only, *phase)

	if *gate && *phase != "" && *phase != contract.PhaseAll {
		return fmt.Errorf("-gate scores every phase, and %s records no phase, so pinning it to a "+
			"-phase %s number would ratchet the full score against a partial one — drop -phase, or "+
			"keep the phase view for reading rather than gating", *baseline, *phase)
	}
	if *asJSON {
		if err := emitJSON(report); err != nil || !*gate {
			return err
		}
	}
	if *gate {
		return qualityGate(report, *baseline)
	}

	if len(report.RPCs) == 0 {
		where := "this library"
		if *only != "" {
			where = "domain " + *only
		}
		fmt.Printf("contract quality%s — score 0: no rpc in %s has a measurable gap\n", phaseNote(*phase, " (%s phase)"), where)
		return nil
	}

	fmt.Printf("contract quality%s — %d rpc(s) with a measurable gap, total score %d\n\n",
		phaseNote(*phase, " (%s phase)"), len(report.RPCs), report.TotalScore)
	fmt.Printf("%5s  %-70s gap\n", "score", "rpc")
	shown := report.RPCs
	if *limit > 0 && len(shown) > *limit {
		shown = shown[:*limit]
	}
	for _, r := range shown {
		fmt.Printf("%5d  %-70s %s\n", r.Score, chain.RPCName(r.RPC), strings.Join(qualityGap(r, *phase), " | "))
	}
	if len(shown) < len(report.RPCs) {
		fmt.Printf("  … %d more\n", len(report.RPCs)-len(shown))
	}
	return nil
}

func phaseNote(phase, format string) string {
	if phase == "" || phase == contract.PhaseAll {
		return ""
	}
	return fmt.Sprintf(format, phase)
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

func qualityGapsNow(report contract.QualityReport, withUncovered bool) string {
	rows := []contract.QualityRPC{}
	for _, r := range report.RPCs {
		if r.Score > 0 && (withUncovered || !r.NoContract) {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Score > rows[j].Score })
	parts := []string{}
	for i, r := range rows {
		if i == 6 {
			parts = append(parts, fmt.Sprintf("and %d more rpc(s)", len(rows)-i))
			break
		}
		gap := strings.Join(qualityGap(r, report.Phase), "; ")
		if r.NoContract {
			gap = "no overlay covers it"
		}
		parts = append(parts, fmt.Sprintf("%s (%d): %s", chain.RPCName(r.RPC), r.Score, gap))
	}
	return strings.Join(parts, "; ")
}

func undocumentedHint(report contract.QualityReport) string {
	for _, r := range report.RPCs {
		if len(r.UndocumentedFields) > 0 {
			return " An undocumented field is usually one the descriptor gained since the baseline (a new proto field), " +
				"not a contract that got vaguer: document it under the rpc's fields: in its overlay (a note, or where its value comes from), " +
				"and the score comes back down."
		}
	}
	return ""
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
			uncovered = append(uncovered, chain.RPCName(r.RPC))
			charged += r.Score
		}
	}
	switch {
	case verdict == store.RatchetWorse && len(uncovered) > 0:
		rest := ""
		if charged < total {
			rest = fmt.Sprintf(" The other %d: %s.", total-charged, qualityGapsNow(report, false))
		}
		return fmt.Errorf("contract quality: score %d is worse than the baseline %d, and %d of it is charged to %d rpc(s) no overlay covers: %s.\n"+
			"An overlay or an entry was deleted, or the descriptor gained rpcs: restore it, or write one with 'shrt contract init <domain>'.%s",
			total, want, charged, len(uncovered), strings.Join(clip(uncovered, 8), ", "), rest)
	case verdict == store.RatchetWorse:
		return fmt.Errorf("contract quality: score %d is worse than the baseline %d. The gaps now (the baseline holds a total, "+
			"not which gaps it counted): %s.%s\nRun 'shrt contract quality' for the full table", total, want, qualityGapsNow(report, true), undocumentedHint(report))
	case verdict == store.RatchetBetter:
		return fmt.Errorf("contract quality: score %d beats the baseline %d — "+
			"lower %s to %d to keep the ratchet tight", total, want, baselinePath, total)
	}
	fmt.Printf("contract quality: score %d, at the baseline\n", total)
	return nil
}
