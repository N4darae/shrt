package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{name: "gate", summary: "run every chain and verify every safe spot, grouping what failed", run: runGate})
}

const gateReportEnv = "SHRT_GATE_REPORT"

const gateExitCodes = "\nexit codes:\n" +
	"  0  every chain passed (or failed exactly as its kept_red pins) and every safe spot verified\n" +
	"  1  a chain failed, a FINDING, tokens of one auth profile refused early twice, or the hollow ratchet\n" +
	"  3  no verdict: a run or verify exited 3 twice (backend down, restarting, refusing auth); re-run\n"

type gateSidecar struct {
	KeptRed      string     `json:"kept_red,omitempty"`
	EarlyProfile string     `json:"early_profile,omitempty"`
	Items        []gateItem `json:"items,omitempty"`
}

type gateItem struct {
	Step   string `json:"step"`
	Call   string `json:"call"`
	Path   string `json:"path"`
	Rule   string `json:"rule,omitempty"`
	Want   string `json:"want"`
	Got    string `json:"got"`
	Writer string `json:"writer,omitempty"`
}

type gateOutcome struct {
	stdout, stderr string
	code           int
	side           gateSidecar
}

var gateExec = func(ctx context.Context, args []string) gateOutcome {
	self, err := os.Executable()
	if err != nil {
		return gateOutcome{stderr: err.Error(), code: 1}
	}
	side, err := os.CreateTemp("", "shrt-gate-*.json")
	if err != nil {
		return gateOutcome{stderr: err.Error(), code: 1}
	}
	_ = side.Close()
	defer os.Remove(side.Name())
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Env = append(os.Environ(), gateReportEnv+"="+side.Name())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	out := gateOutcome{}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return gateOutcome{stderr: err.Error(), code: 1}
		}
		out.code = exit.ExitCode()
	}
	out.stdout, out.stderr = stdout.String(), stderr.String()
	if raw, err := os.ReadFile(side.Name()); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.side)
	}
	return out
}

var gateSleep = func(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func writeGateSidecar(side gateSidecar) {
	path := os.Getenv(gateReportEnv)
	if path == "" {
		return
	}
	if raw, err := json.Marshal(side); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}

func runSidecar(e *env, c *chain.Chain, rec *runner.Record) gateSidecar {
	side := gateSidecar{KeptRed: rec.KeptRed, EarlyProfile: earlyProfile(e, rec)}
	pinned := map[string]bool{}
	for _, p := range c.KeptRed {
		pinned[p.Step+" "+p.Path] = true
	}
	for _, st := range rec.Steps {
		if st == nil || st.Status == runner.StatusPassed || st.Status == runner.StatusSkipped {
			continue
		}
		found := false
		for _, ex := range st.Expect {
			if ex.Passed || ex.Rule == "unevaluated" || pinned[st.ID+" "+ex.Path] {
				continue
			}
			found = true
			side.Items = append(side.Items, gateItem{Step: st.ID, Call: st.Call, Path: ex.Path, Rule: ex.Rule, Want: gateValue(ex.Want), Got: gateValue(ex.Got), Writer: entityWriter(rec, st.ID)})
		}
		if !found && st.Error != "" && !pinnedStep(c, st.ID) {
			why, _, _ := strings.Cut(st.Error, "\n")
			side.Items = append(side.Items, gateItem{Step: st.ID, Call: st.Call, Path: "(" + st.Status + ")", Got: capText(why, 160)})
		}
	}
	return side
}

func (it gateItem) verdict() string {
	return it.Path + " " + chain.WantGot(it.Rule, it.Want, it.Got)
}

func pinnedStep(c *chain.Chain, step string) bool {
	for _, p := range c.KeptRed {
		if p.Step == step {
			return true
		}
	}
	return false
}

func verifySidecar(e *env, rec *runner.Record, report *diff.Report) gateSidecar {
	side := gateSidecar{EarlyProfile: earlyProfile(e, rec)}
	changed := map[string]bool{}
	for _, c := range report.Changes {
		if c.Kind != diff.KindNotReached && c.Kind != diff.KindStatus {
			changed[c.Step] = true
		}
	}
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached || c.Kind == diff.KindStatus && changed[c.Step] {
			continue
		}
		call := ""
		if st, ok := rec.Step(c.Step); ok && st != nil {
			call = st.Call
		}
		side.Items = append(side.Items, gateItem{Step: c.Step, Call: call, Path: c.Path, Want: gateValue(c.Want), Got: gateValue(c.Got), Writer: entityWriter(rec, c.Step)})
	}
	return side
}

var gateRef = regexp.MustCompile(`\$\{\s*([A-Za-z0-9_-]+)\.`)

func entityWriter(rec *runner.Record, step string) string {
	at := -1
	for i, st := range rec.Steps {
		if st != nil && st.ID == step {
			at = i
		}
	}
	if at < 0 || !chain.IsReadOnlyCall(rec.Steps[at].Call) {
		return ""
	}
	entities := map[string]bool{}
	for _, ref := range rec.Steps[at].BodyRefs {
		for _, m := range gateRef.FindAllStringSubmatch(ref, -1) {
			entities[m[1]] = true
		}
	}
	for i := at - 1; i >= 0; i-- {
		if st := rec.Steps[i]; st != nil && entities[st.ID] && !chain.IsReadOnlyCall(st.Call) {
			return st.Call
		}
	}
	return ""
}

func earlyProfile(e *env, rec *runner.Record) string {
	life := examineTokenLifetime(e, rec)
	if life == nil || life.finding() {
		return ""
	}
	return life.profile(life.first)
}

func gateValue(v any) string { return capText(compactValue(v), 60) }

type gateChain struct {
	name      string
	file      string
	spot      bool
	verdict   string
	first     string
	notes     []string
	items     []gateItem
	failed    bool
	noVerdict bool
}

func runGate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	wait := fs.Duration("retry-wait", 20*time.Second, "wait before re-running a run or verify that exited 3")
	hollowBaseline := fs.String("hollow-baseline", ".shrt/hollow-baseline", "`file` for the chain hollow ratchet; empty skips it")
	setUsage(fs, "usage: shrt gate [<chain>...] [flags]   run each chain, verify each safe spot (a fresh -var tag each), group what failed", gateExitCodes)
	only, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	chains, err := gateChains(e, only)
	if err != nil {
		return err
	}
	if len(chains) == 0 {
		return fmt.Errorf("no chains in %s and no safe spots in %s", rel(e.cfg.Root, e.chainsDir()), rel(e.cfg.Root, e.store.SafeSpotsDir))
	}
	tag := fmt.Sprintf("ci%d%d", time.Now().Unix(), rand.IntN(100000))
	width := 0
	for _, g := range chains {
		width = max(width, len(g.name))
	}
	early := map[string]int{}
	for _, g := range chains {
		readsTag := false
		if c, err := e.resolveChain(g.name); err == nil {
			readsTag = len(c.UnusedVarNames(map[string]any{"tag": ""})) == 0
		}
		steps := []string{}
		if g.file != "" {
			steps = append(steps, "run")
		}
		if g.spot {
			steps = append(steps, "verify")
		}
		for _, what := range steps {
			out := gateAttempt(ctx, what, g.name, tag, readsTag, *wait)
			if out.side.EarlyProfile != "" {
				early[out.side.EarlyProfile]++
			}
			g.absorb(what, out)
		}
		fmt.Println(g.line(width))
		for _, n := range g.notes {
			fmt.Println("  " + n)
		}
	}
	failed, unverified := 0, 0
	for _, g := range chains {
		if g.failed {
			failed++
		} else if g.noVerdict {
			unverified++
		}
	}
	findings := []string{}
	if *hollowBaseline != "" && len(only) == 0 {
		out := gateExec(ctx, []string{"chain", "hollow", "-gate", "-baseline", *hollowBaseline})
		if out.code != 0 {
			findings = append(findings, "hollow ratchet: "+lastLine(out.stderr+"\n"+out.stdout))
		}
	}
	profiles := []string{}
	for p := range early {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	for _, p := range profiles {
		if early[p] > 1 {
			findings = append(findings, fmt.Sprintf("FINDING: tokens of auth profile %s were refused early in %d runs of this gate: "+
				"the backend ends sessions long before the expiry its login states", p, early[p]))
		} else {
			fmt.Printf("note: a token of auth profile %s was refused early once; a deploy before this gate explains one\n", p)
		}
	}
	printGateGroups(chains)
	for _, f := range findings {
		fmt.Println(f)
	}
	switch {
	case failed > 0 || len(findings) > 0:
		return exitWith(1, "FAIL: %d of %d chain(s) failed%s; details: shrt verify <chain>, or shrt run <chain> -keep-going",
			failed, len(chains), gateAlso(unverified, len(findings)))
	case unverified > 0:
		return exitWith(3, "NO VERDICT: %d of %d chain(s) could not be verified (exit 3 twice: backend down, restarting or refusing auth); re-run once it is up",
			unverified, len(chains))
	}
	fmt.Printf("gate: PASS: %d chain(s)\n", len(chains))
	return nil
}

func gateAlso(unverified, findings int) string {
	out := ""
	if unverified > 0 {
		out += fmt.Sprintf(", %d no verdict", unverified)
	}
	if findings > 0 {
		out += fmt.Sprintf(", %d finding(s)", findings)
	}
	return out
}

func gateChains(e *env, only []string) ([]*gateChain, error) {
	byName := map[string]*gateChain{}
	get := func(name string) *gateChain {
		if byName[name] == nil {
			byName[name] = &gateChain{name: name}
		}
		return byName[name]
	}
	for _, ext := range []string{"*.yaml", "*.yml"} {
		files, err := filepath.Glob(filepath.Join(e.chainsDir(), ext))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			get(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))).file = f
		}
	}
	spots, err := filepath.Glob(filepath.Join(e.store.SafeSpotsDir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, s := range spots {
		get(strings.TrimSuffix(filepath.Base(s), ".json")).spot = true
	}
	for _, n := range only {
		if byName[n] == nil {
			return nil, fmt.Errorf("no chain or safe spot named %q", n)
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		if len(only) == 0 || containsName(only, n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]*gateChain, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out, nil
}

func gateAttempt(ctx context.Context, what, name, tag string, readsTag bool, wait time.Duration) gateOutcome {
	prefix := ""
	if what == "verify" {
		prefix = "v-"
	}
	var out gateOutcome
	for try := 1; try <= 2; try++ {
		args := []string{what, name, "-quiet"}
		if readsTag {
			args = append(args, "-var", fmt.Sprintf("tag=%s-%s%s-%d", tag, prefix, name, try))
		}
		out = gateExec(ctx, args)
		if out.code != 3 || try == 2 || ctx.Err() != nil {
			break
		}
		fmt.Fprintf(os.Stderr, "gate: %s %s: no verdict (exit 3); retrying once in %s\n", what, name, wait)
		gateSleep(ctx, wait)
	}
	return out
}

var gateNotable = regexp.MustCompile(`^(FINDING|REGRESSION|CHAIN DEFECT|LATENCY|WARNING)\b`)

func (g *gateChain) absorb(what string, out gateOutcome) {
	for _, line := range strings.Split(out.stdout, "\n") {
		line = strings.TrimSpace(line)
		if gateNotable.MatchString(line) && !containsName(g.notes, capText(line, 240)) {
			g.notes = append(g.notes, capText(line, 240))
		}
	}
	why := errorLine(out.stderr, "shrt "+what+": ")
	switch {
	case out.code == 0:
		if what == "run" && out.side.KeptRed == runner.KeptRedAsPinned && g.verdict == "" {
			g.verdict = "KEPT RED"
		}
	case out.code == 3:
		g.noVerdict = true
		if g.first == "" && !g.failed {
			g.first = capText(what+": "+why, 200)
			if len(out.side.Items) > 0 {
				it := out.side.Items[0]
				g.first = capText(fmt.Sprintf("%s: %s %s", what, it.Step, it.Got), 200)
			}
		}
	default:
		if !g.failed {
			g.first = ""
		}
		g.failed = true
		if g.first == "" {
			if len(out.side.Items) > 0 {
				it := out.side.Items[0]
				g.first = fmt.Sprintf("%s (%s) %s", it.Step, shortRPC(it.Call), it.verdict())
			} else {
				g.first = capText(what+": "+why, 200)
			}
		}
		g.items = append(g.items, out.side.Items...)
	}
}

func (g *gateChain) line(width int) string {
	verdict := "PASS"
	switch {
	case g.failed:
		verdict = "FAIL"
	case g.noVerdict:
		verdict = "NO VERDICT"
	case g.verdict != "":
		verdict = g.verdict
	}
	line := fmt.Sprintf("%-10s %-*s", verdict, width, g.name)
	if g.first != "" && verdict != "PASS" && verdict != "KEPT RED" {
		line += "  " + g.first
	}
	return strings.TrimRight(line, " ")
}

var gateIndex = regexp.MustCompile(`\.\d+(\.|$)`)

type gateGroup struct {
	rpc, path string
	steps     map[string]bool
	chains    map[string]bool
	writers   map[string]int
	example   string
}

func printGateGroups(chains []*gateChain) {
	groups := map[string]*gateGroup{}
	order := []string{}
	for _, g := range chains {
		for _, it := range g.items {
			path := gateIndex.ReplaceAllString(it.Path, "[]$1")
			key := shortRPC(it.Call) + " " + path
			gr := groups[key]
			if gr == nil {
				gr = &gateGroup{rpc: shortRPC(it.Call), path: path, steps: map[string]bool{}, chains: map[string]bool{}, writers: map[string]int{},
					example: fmt.Sprintf("%s %s %s", g.name, it.Step, chain.WantGot(it.Rule, it.Want, it.Got))}
				groups[key] = gr
				order = append(order, key)
			}
			if !gr.steps[g.name+" "+it.Step] && it.Writer != "" {
				gr.writers[shortRPC(it.Writer)]++
			}
			gr.steps[g.name+" "+it.Step] = true
			gr.chains[g.name] = true
		}
	}
	if len(order) == 0 {
		return
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if len(a.chains) != len(b.chains) {
			return len(a.chains) > len(b.chains)
		}
		return len(a.steps) > len(b.steps)
	})
	fmt.Println("failures by rpc and path, most widespread first:")
	for i, key := range order {
		if i == 12 {
			fmt.Printf("  and %d more\n", len(order)-i)
			break
		}
		gr := groups[key]
		fmt.Printf("  %s %s: %d step(s) in %d chain(s), e.g. %s%s\n", gr.rpc, gr.path, len(gr.steps), len(gr.chains), gr.example, writersNote(gr.writers))
	}
}

func errorLine(stderr, prefix string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return rest
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	return line
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func writersNote(writers map[string]int) string {
	if len(writers) == 0 {
		return ""
	}
	names := make([]string, 0, len(writers))
	for w := range writers {
		names = append(names, w)
	}
	sort.Slice(names, func(i, j int) bool {
		if writers[names[i]] != writers[names[j]] {
			return writers[names[i]] > writers[names[j]]
		}
		return names[i] < names[j]
	})
	parts := []string{}
	for i, w := range names {
		if i == 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s x%d", w, writers[w]))
	}
	return "; its entity from " + strings.Join(parts, ", ")
}
