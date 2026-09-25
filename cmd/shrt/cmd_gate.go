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
	"github.com/N4darae/shrt/namecase"
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
	Request      string     `json:"request,omitempty"`
}

type gateItem struct {
	Step string `json:"step"`
	Call string `json:"call"`
	Path string `json:"path"`
	Rule string `json:"rule,omitempty"`
	Want string `json:"want"`
	Got  string `json:"got"`

	Suspect     string `json:"suspect,omitempty"`
	SuspectStep string `json:"suspect_step,omitempty"`
	KnockOn     bool   `json:"knock_on,omitempty"`
	Own         string `json:"own,omitempty"`
	Class       string `json:"class,omitempty"`
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
	a := runAttribution(e, rec)
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
			want, got := gatePair(ex.Want, ex.Got)
			side.Items = append(side.Items, a.item(gateItem{Step: st.ID, Call: st.Call, Path: ex.Path, Rule: ex.Rule, Want: want, Got: got}))
		}
		if !found && st.Error != "" && !pinnedStep(c, st.ID) {
			why, _, _ := strings.Cut(st.Error, "\n")
			side.Items = append(side.Items, a.item(gateItem{Step: st.ID, Call: st.Call, Path: "(" + st.Status + ")", Got: capText(why, 160)}))
		}
	}
	if len(side.Items) > 0 {
		side.Request = requestLine(rec, side.Items[0].Step, a.of(side.Items[0].Step, side.Items[0].Path))
	}
	return side
}

func badSteps(rec *runner.Record) map[string]bool {
	bad := map[string]bool{}
	for _, st := range rec.Steps {
		if st != nil && st.Status != runner.StatusPassed && st.Status != runner.StatusSkipped {
			bad[st.ID] = true
		}
	}
	return bad
}

func runAttribution(e *env, rec *runner.Record) attribution {
	return attribution{e: e, rec: rec, bad: badSteps(rec),
		unchanged: func(step, path string) bool {
			st, ok := rec.Step(step)
			if !ok || st == nil {
				return false
			}
			for _, ex := range st.Expect {
				if namecase.Equal(ex.Path, path) {
					return ex.Passed
				}
			}
			return false
		},
		reordered: func(step, path string) bool {
			st, ok := rec.Step(step)
			return ok && st != nil && runner.ReorderedPaths(st)[path]
		},
	}
}

func verifyAttribution(e *env, rec *runner.Record, report *diff.Report) attribution {
	bad := map[string]bool{}
	for _, c := range report.Changes {
		if c.Kind != diff.KindNotReached {
			bad[c.Step] = true
		}
	}
	return attribution{e: e, rec: rec, bad: bad,
		unchanged: func(step, path string) bool {
			for _, c := range report.Changes {
				if c.Step != step || c.Kind == diff.KindNotReached {
					continue
				}
				if c.Kind == diff.KindStatus || c.Path == path || strings.HasPrefix(path, c.Path+".") || strings.HasPrefix(c.Path, path+".") {
					return false
				}
			}
			return true
		},
		reordered: func(step, path string) bool {
			return report.Class(diff.Change{Step: step, Path: path}) == "order changed"
		},
	}
}

func (a attribution) item(it gateItem) gateItem {
	path := it.Path
	if strings.HasPrefix(path, "(") {
		path = ""
	}
	b := a.of(it.Step, path)
	it.Own = b.own
	if b.write >= 0 {
		it.Suspect, it.SuspectStep, it.KnockOn = a.rec.Steps[b.write].Call, a.rec.Steps[b.write].ID, b.knock
	}
	return it
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
	a := verifyAttribution(e, rec, report)
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached || c.Kind == diff.KindStatus && changed[c.Step] {
			continue
		}
		call := ""
		if st, ok := rec.Step(c.Step); ok && st != nil {
			call = st.Call
		}
		want, got := gatePair(c.Want, c.Got)
		it := a.item(gateItem{Step: c.Step, Call: call, Path: c.Path, Want: want, Got: got})
		it.Class = report.Class(c)
		side.Items = append(side.Items, it)
	}
	if len(side.Items) > 0 {
		side.Request = requestLine(rec, side.Items[0].Step, a.of(side.Items[0].Step, side.Items[0].Path))
	}
	return side
}

var gateRef = regexp.MustCompile(`\$\{\s*(?:steps\.)?([A-Za-z0-9_-]+)\.`)

func earlyProfile(e *env, rec *runner.Record) string {
	life := examineTokenLifetime(e, rec)
	if life == nil || life.finding() {
		return ""
	}
	return life.profile(life.first)
}

func gatePair(want, got any) (string, string) {
	return capPair(compactValue(want), compactValue(got), 60)
}

func capPair(want, got string, n int) (string, string) {
	d := 0
	for d < len(want) && d < len(got) && want[d] == got[d] {
		d++
	}
	return capAround(want, d, n), capAround(got, d, n)
}

func capAround(s string, d, n int) string {
	if len(s) <= n || d < n-n/3 {
		return capText(s, n)
	}
	body := s[d-n/3:]
	if len(body)+3 <= n {
		return "..." + body
	}
	return "..." + body[:n-6] + "..."
}

type gateChain struct {
	name      string
	file      string
	spot      bool
	verdict   string
	first     string
	request   string
	notes     []string
	items     []gateItem
	class     string
	firstAt   string
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
		if g.request != "" && g.failed {
			fmt.Println("  " + g.request)
		}
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
		if f := gateHollow(ctx, *hollowBaseline); f != "" {
			findings = append(findings, f)
		}
	}
	profiles := []string{}
	for p := range early {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	once := []string{}
	for _, p := range profiles {
		if early[p] > 1 {
			findings = append(findings, fmt.Sprintf("FINDING: tokens of auth profile %s were refused early in %d runs of this gate: "+
				"the backend ends sessions long before the expiry its login states", p, early[p]))
		} else {
			once = append(once, p)
		}
	}
	switch len(once) {
	case 0:
	case 1:
		fmt.Printf("note: a token of auth profile %s was refused early once; a deploy before this gate explains one\n", once[0])
	default:
		fmt.Printf("note: a token of each of auth profiles %s was refused early once; a deploy before this gate explains that\n", strings.Join(once, ", "))
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

func noteKey(line string) string {
	label, rest, _ := strings.Cut(line, ": ")
	for _, sep := range []string{": ", " took "} {
		if i := strings.Index(rest, sep); i >= 0 {
			rest = rest[:i]
		}
	}
	return label + ": " + strings.Replace(rest, "repeated failure", "intermittent failure", 1)
}

func (g *gateChain) absorb(what string, out gateOutcome) {
	for _, line := range strings.Split(out.stdout, "\n") {
		line = strings.TrimSpace(line)
		if !gateNotable.MatchString(line) {
			continue
		}
		seen := false
		for i, n := range g.notes {
			if noteKey(n) == noteKey(line) {
				seen = true
				if strings.Contains(line, "repeated failure") {
					g.notes[i] = capText(line, 240)
				}
			}
		}
		if !seen {
			g.notes = append(g.notes, capText(line, 240))
		}
	}
	why := strings.TrimPrefix(errorLine(out.stderr, "shrt "+what+": "), "chain "+g.name+": ")
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
			g.first, g.request = "", ""
		}
		g.failed = true
		if g.first == "" {
			if len(out.side.Items) > 0 {
				it := out.side.Items[0]
				g.first = fmt.Sprintf("%s (%s) %s", it.Step, shortRPC(it.Call), it.verdict())
				g.firstAt = it.Step + " " + it.Path
				g.request = out.side.Request
			} else {
				g.first = capText(what+": "+why, 200)
			}
		}
		if what == "verify" {
			g.adoptBlame(out.side)
		}
		g.items = append(g.items, out.side.Items...)
		if what == "verify" && g.class == "" && len(out.side.Items) > 0 {
			g.class = out.side.Items[0].Class
			for _, it := range out.side.Items {
				if it.Step+" "+it.Path == g.firstAt {
					g.class = it.Class
					break
				}
			}
		}
	}
}

func (g *gateChain) adoptBlame(side gateSidecar) {
	own := map[string]string{}
	for _, it := range side.Items {
		if it.Own != "" {
			own[it.Step+" "+it.Path] = it.Own
		}
	}
	for i, it := range g.items {
		if why := own[it.Step+" "+it.Path]; why != "" && it.Own == "" {
			g.items[i].Own, g.items[i].Suspect, g.items[i].SuspectStep, g.items[i].KnockOn = why, "", "", false
		}
	}
	if len(side.Items) > 0 && own[g.firstAt] != "" && side.Items[0].Step+" "+side.Items[0].Path == g.firstAt {
		g.request = side.Request
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
		line += "  "
		if g.class != "" && g.failed {
			line += g.class + ": "
		}
		line += g.first
	}
	return strings.TrimRight(line, " ")
}

var gateIndex = regexp.MustCompile(`\.\d+(\.|$)`)

type gateGroup struct {
	rpc, suspect  string
	own           string
	knockOn       bool
	steps, chains map[string]bool
	paths         []string
	example       string
	reads         map[string]bool
	readRPCs      []string
	readPaths     map[string][]string
}

func (gr *gateGroup) addPath(list *[]string, p string) {
	if !containsName(*list, p) {
		*list = append(*list, p)
	}
}

func printGateGroups(chains []*gateChain) {
	groups := map[string]*gateGroup{}
	order := []string{}
	group := func(key string) *gateGroup {
		if groups[key] == nil {
			groups[key] = &gateGroup{steps: map[string]bool{}, chains: map[string]bool{}, reads: map[string]bool{}, readPaths: map[string][]string{}}
			order = append(order, key)
		}
		return groups[key]
	}
	readAfter := map[string][]string{}
	for _, g := range chains {
		for _, it := range g.items {
			if key := methodName(it.Call) + " " + gateIndex.ReplaceAllString(it.Path, "[]$1"); it.Suspect != "" && !it.KnockOn && it.Own == "" &&
				!containsName(readAfter[key], methodName(it.Suspect)) {
				readAfter[key] = append(readAfter[key], methodName(it.Suspect))
			}
		}
	}
	for _, g := range chains {
		for _, it := range g.items {
			path := gateIndex.ReplaceAllString(it.Path, "[]$1")
			if writes := readAfter[methodName(it.Call)+" "+path]; len(writes) > 1 && it.Suspect != "" && !it.KnockOn && it.Own == "" {
				it.Own = fmt.Sprintf("%s changes %s after %d different writes (%s)", methodName(it.Call), path, len(writes), capList(writes, 3))
			}
			step := g.name + " " + it.Step
			example := fmt.Sprintf("%s %s %s %s", g.name, it.Step, path, chain.WantGot(it.Rule, it.Want, it.Got))
			switch {
			case it.Own != "":
				gr := group("o " + shortRPC(it.Call) + " " + it.Own)
				gr.rpc, gr.own = shortRPC(it.Call), it.Own
				gr.steps[step] = true
				gr.chains[g.name] = true
				gr.addPath(&gr.paths, path)
				if gr.example == "" {
					gr.example = example
				}
			case it.Suspect != "" && !it.KnockOn:
				gr := group("w " + shortRPC(it.Suspect))
				gr.rpc = shortRPC(it.Suspect)
				gr.reads[step] = true
				gr.chains[g.name] = true
				read := methodName(it.Call)
				gr.addPath(&gr.readRPCs, read)
				paths := gr.readPaths[read]
				gr.addPath(&paths, path)
				gr.readPaths[read] = paths
				if gr.suspect == "" {
					gr.suspect = g.name + " " + it.SuspectStep
				}
			case it.Suspect == "" && !chain.IsReadOnlyCall(it.Call):
				gr := group("w " + shortRPC(it.Call))
				gr.rpc = shortRPC(it.Call)
				gr.steps[step] = true
				gr.chains[g.name] = true
				gr.addPath(&gr.paths, path)
				if gr.example == "" {
					gr.example = example
				}
			default:
				gr := group("r " + shortRPC(it.Call))
				gr.rpc, gr.knockOn = shortRPC(it.Call), it.KnockOn
				if it.KnockOn {
					gr.suspect = shortRPC(it.Suspect)
				}
				gr.steps[step] = true
				gr.chains[g.name] = true
				gr.addPath(&gr.paths, path)
				if gr.example == "" {
					gr.example = example
				}
			}
		}
	}
	if len(order) == 0 {
		return
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if aw, bw := !strings.HasPrefix(order[i], "r "), !strings.HasPrefix(order[j], "r "); aw != bw {
			return aw
		}
		if len(a.chains) != len(b.chains) {
			return len(a.chains) > len(b.chains)
		}
		return len(a.steps)+len(a.reads) > len(b.steps)+len(b.reads)
	})
	fmt.Println("failures by suspect rpc (the read itself, or the failing or changed write it observes), then the rest:")
	lines := 0
	for i, key := range order {
		gr := groups[key]
		if lines >= 10 {
			fmt.Printf("  and %d more\n", len(order)-i)
			break
		}
		switch {
		case len(gr.steps) > 0:
			tail := ""
			if gr.own != "" {
				tail = "; suspect the read: " + gr.own
			} else if gr.knockOn {
				tail = "; a knock-on of " + gr.suspect
			} else if strings.HasPrefix(key, "r ") {
				tail = "; no suspect write"
			}
			fmt.Printf("  %s: %d step(s) in %d chain(s), paths %s%s; e.g. %s\n", gr.rpc, len(gr.steps), len(gr.chains), capList(gr.paths, 3), tail, gr.example)
		default:
			fmt.Printf("  %s: passed itself, but reads after it failed or changed; e.g. %s\n", gr.rpc, gr.suspect)
		}
		lines++
		if len(gr.reads) > 0 {
			parts := []string{}
			for _, read := range gr.readRPCs {
				parts = append(parts, read+" "+capList(gr.readPaths[read], 3))
			}
			fmt.Printf("    +%d read(s): %s\n", len(gr.reads), strings.Join(parts, "; "))
			lines++
		}
	}
}

func methodName(call string) string {
	return call[strings.LastIndex(call, "/")+1:]
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

func gateHollow(ctx context.Context, baseline string) string {
	if _, err := os.Stat(baseline); errors.Is(err, os.ErrNotExist) {
		if os.Getenv("CI") != "" {
			return "hollow ratchet: " + baseline + " is missing; run shrt gate once outside CI, which writes it, and commit it"
		}
		out := gateExec(ctx, []string{"chain", "hollow", "-json"})
		var rep struct {
			Reported *int `json:"reported"`
		}
		if out.code != 0 || json.Unmarshal([]byte(out.stdout), &rep) != nil || rep.Reported == nil {
			return "hollow ratchet: " + gateError(out, "shrt chain: ")
		}
		if err := os.WriteFile(baseline, []byte(fmt.Sprintf("%d\n", *rep.Reported)), 0o644); err != nil {
			return "hollow ratchet: " + err.Error()
		}
		fmt.Printf("hollow ratchet: %s did not exist; wrote today's count, %d, to it: commit it\n", baseline, *rep.Reported)
		return ""
	}
	out := gateExec(ctx, []string{"chain", "hollow", "-gate", "-baseline", baseline})
	if out.code == 0 {
		return ""
	}
	return "hollow ratchet: " + gateError(out, "shrt chain: ")
}

func gateError(out gateOutcome, prefix string) string {
	if strings.TrimSpace(out.stderr) != "" {
		return strings.TrimPrefix(errorLine(out.stderr, prefix), "hollow: ")
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out.stdout), "\n")
	return line
}
