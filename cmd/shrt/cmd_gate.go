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
	"strconv"
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
	KeptRed      string              `json:"kept_red,omitempty"`
	PinsHeld     bool                `json:"pins_held,omitempty"`
	EarlyProfile string              `json:"early_profile,omitempty"`
	EarlyAge     time.Duration       `json:"early_age,omitempty"`
	EarlyStated  time.Duration       `json:"early_stated,omitempty"`
	Reads        map[string]gateRead `json:"reads,omitempty"`
	Items        []gateItem          `json:"items,omitempty"`
	Sent         map[string]string   `json:"sent,omitempty"`
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
	Cascade     string `json:"cascade,omitempty"`
	Why         string `json:"why,omitempty"`
	Firm        bool   `json:"firm,omitempty"`
	Class       string `json:"class,omitempty"`
	Length      string `json:"length,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Variant     string `json:"variant,omitempty"`
	Pinned      string `json:"pinned,omitempty"`
	Inputs      string `json:"inputs,omitempty"`

	or string
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
	side := earlySidecar(e, rec)
	changedPins, held := runner.PinChanges(c, rec)
	side.KeptRed, side.PinsHeld = rec.KeptRed, held || runner.PinsHeld(c, rec)
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
			was, moved := changedPins[st.ID+" "+ex.Path]
			if ex.Passed || ex.Rule == "unevaluated" || pinned[st.ID+" "+ex.Path] && !moved {
				continue
			}
			found = true
			want, got := gatePair(ex.Want, ex.Got)
			it := a.item(gateItem{Step: st.ID, Call: st.Call, Path: ex.Path, Rule: ex.Rule, Want: want, Got: got})
			if moved {
				it.Pinned, _ = gatePair(was, ex.Got)
			}
			it.Length = pastEnd(st, ex.Path)
			side.Items = append(side.Items, it)
		}
		if !found && st.Error != "" && !pinnedStep(c, st.ID) {
			why, _, _ := strings.Cut(st.Error, "\n")
			side.Items = append(side.Items, a.item(gateItem{Step: st.ID, Call: st.Call, Path: "(" + st.Status + ")", Got: capText(why, 160)}))
		}
	}
	side.Sent = firstSent(rec, side.Items)
	return side
}

func pastEnd(st *runner.StepRecord, path string) string {
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return ""
	}
	segs := chain.SplitPath(path)
	for k := 1; k < len(segs); k++ {
		idx, err := strconv.Atoi(segs[k])
		if err != nil {
			continue
		}
		list := strings.Join(segs[:k], ".")
		v, _ := chain.Get(body, list)
		items, ok := v.([]any)
		if !ok || idx < len(items) {
			return ""
		}
		want := idx + 1
		for _, ex := range st.Expect {
			if ex.Rule == "exists" && ex.Want == false {
				continue
			}
			if rest, ok := strings.CutPrefix(ex.Path, list+"."); ok {
				head, _, _ := strings.Cut(rest, ".")
				if n, err := strconv.Atoi(head); err == nil {
					want = max(want, n+1)
				}
			}
		}
		return fmt.Sprintf("%s length want=%d got=%d", list, want, len(items))
	}
	return ""
}

func firstSent(rec *runner.Record, items []gateItem) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out, roots := map[string]string{}, map[string]bool{}
	for _, it := range items {
		if roots[rootOf(it)] || len(roots) == 5 {
			continue
		}
		roots[rootOf(it)] = true
		for _, step := range []string{it.Step, it.SuspectStep} {
			if st, ok := rec.Step(step); ok && st != nil {
				if sent := sentText(st); sent != "" {
					out[step] = sent
				}
			}
		}
	}
	return out
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
		changed: func(step string) []string {
			st, ok := rec.Step(step)
			if !ok || st == nil {
				return nil
			}
			var out []string
			for _, ex := range st.Expect {
				if !ex.Passed && ex.Rule != "unevaluated" {
					out = append(out, ex.Path)
				}
			}
			return out
		},
		resized: func(step, path string) string {
			st, ok := rec.Step(step)
			if !ok || st == nil {
				return ""
			}
			return listUnder(runResized(st), path)
		},
		was: func(step, path string) (any, bool) {
			st, ok := rec.Step(step)
			if !ok || st == nil {
				return nil, false
			}
			for _, ex := range st.Expect {
				if !ex.Passed && ex.Rule == "equals" && namecase.Equal(ex.Path, path) {
					return ex.Want, true
				}
			}
			return nil, false
		},
	}
}

func runResized(st *runner.StepRecord) []string {
	var body any
	if json.Unmarshal(st.Response, &body) != nil {
		return nil
	}
	var out []string
	for _, ex := range st.Expect {
		if ex.Passed || ex.Rule == "unevaluated" {
			continue
		}
		segs := chain.SplitPath(ex.Path)
		for k := 1; k < len(segs); k++ {
			if _, err := strconv.Atoi(segs[k]); err != nil {
				continue
			}
			list := strings.Join(segs[:k], ".")
			rest := strings.Join(segs[k+1:], ".")
			v, _ := chain.Get(body, list)
			items, isList := v.([]any)
			if !isList {
				break
			}
			member := rest == "" && ex.Rule == "exists"
			if rest != "" && ex.Rule == "equals" && diff.IDNamedPath(rest) {
				member = true
				for _, it := range items {
					if got, ok := chain.Get(it, rest); ok && compactValue(got) == compactValue(ex.Want) {
						member = false
					}
				}
			}
			if member {
				out = append(out, list)
			}
			break
		}
	}
	return out
}

func listUnder(lists []string, path string) string {
	for _, l := range lists {
		if path == l || strings.HasPrefix(path, l+".") {
			return l
		}
	}
	return ""
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
		changed: func(step string) []string {
			var out []string
			for _, c := range report.Changes {
				if c.Step == step && c.Kind != diff.KindNotReached && c.Kind != diff.KindStatus {
					out = append(out, c.Path)
				}
			}
			return out
		},
		resized: func(step, path string) string {
			var lists []string
			for _, c := range report.Changes {
				if c.Step == step && (c.Kind == diff.KindLength || c.Kind == diff.KindMembership) {
					lists = append(lists, c.Path)
				}
			}
			return listUnder(lists, path)
		},
		was: func(step, path string) (any, bool) {
			for _, c := range report.Changes {
				if c.Step == step && c.Path == path && c.Kind == diff.KindChanged {
					return c.Want, true
				}
			}
			return nil, false
		},
	}
}

func (a attribution) item(it gateItem) gateItem {
	path := it.Path
	if strings.HasPrefix(path, "(") {
		path = ""
	}
	b := a.of(it.Step, path)
	it.Own, it.Cascade, it.Why, it.Firm = b.own, b.cascade, b.why, b.firm
	if b.write < 0 && b.own == "" {
		it.Inputs = a.inputs(it.Step, path)
	}
	if b.write >= 0 {
		w := a.rec.Steps[b.write]
		it.Suspect, it.SuspectStep, it.KnockOn = w.Call, w.ID, b.knock
		if !a.root(w.ID) {
			it.Variant = variantOf(w)
		}
	}
	if st, ok := a.rec.Step(it.Step); ok && st != nil {
		switch {
		case a.flipped(st) != "":
			it.Kind = "refused"
		case path != "" && a.reordered != nil && a.reordered(it.Step, path):
			it.Kind = "order"
		case path != "" && a.resized != nil && a.resized(it.Step, path) != "":
			it.Kind = "membership"
		}
	}
	return it
}

func (a attribution) root(step string) bool {
	if a.changed == nil {
		return false
	}
	for _, p := range a.changed(step) {
		if a.of(step, p).write < 0 {
			return true
		}
	}
	return false
}

func variantOf(w *runner.StepRecord) string {
	var parts []string
	if p := profileOf(w); p != "default" {
		parts = append(parts, "as "+p)
	}
	if why := refusalOf(w); why != "" {
		parts = append(parts, "refused ("+why+")")
	}
	return strings.Join(parts, ", ")
}

func (it gateItem) suspectKey() string {
	if it.Variant == "" {
		return shortRPC(it.Suspect)
	}
	return shortRPC(it.Suspect) + " " + it.Variant
}

func (it gateItem) shown() (string, string) {
	if it.Kind == "order" {
		return listOf(it.Path), "same items in another order"
	}
	return gateIndex.ReplaceAllString(it.Path, "[]$1"), it.wantGot()
}

func listOf(path string) string {
	segs := chain.SplitPath(path)
	for i, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil && i > 0 {
			return strings.Join(segs[:i], ".")
		}
	}
	return path
}

func (it gateItem) verdict() string {
	return it.Path + " " + it.wantGot()
}

func (it gateItem) headline() string {
	switch {
	case it.Length != "":
		return it.Length
	case it.Kind == "order":
		path, what := it.shown()
		return path + " " + what
	case it.Inputs != "":
		return it.verdict() + "; " + it.Inputs
	}
	return it.verdict()
}

func (it gateItem) wantGot() string {
	if strings.HasPrefix(it.Path, "(") {
		return it.Got
	}
	return chain.WantGot(it.Rule, it.Want, it.Got)
}

func pinnedStep(c *chain.Chain, step string) bool {
	for _, p := range c.KeptRed {
		if p.Step == step {
			return true
		}
	}
	return false
}

func verifySidecar(e *env, rec *runner.Record, report *diff.Report, latency []diff.LatencyFlag) gateSidecar {
	side := earlySidecar(e, rec)
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
		if c.Kind == diff.KindLength || c.Kind == diff.KindMembership {
			if why, _, _ := strings.Cut(c.Detail, "; per-item ids not listed"); why != "" {
				got = capText(got+" ("+why+")", 160)
			}
		}
		it := a.item(gateItem{Step: c.Step, Call: call, Path: c.Path, Want: want, Got: got})
		it.Class = report.Class(c)
		for _, l := range report.Changes {
			if l.Kind == diff.KindLength && l.Step == c.Step && (l.Path == c.Path || strings.HasPrefix(c.Path, l.Path+".")) {
				it.Length = fmt.Sprintf("%s length want=%s got=%s", l.Path, compactValue(l.Want), compactValue(l.Got))
				break
			}
		}
		side.Items = append(side.Items, it)
	}
	if latencyPolicy(e).Fail {
		side.Items = append(side.Items, latencyItems(latency)...)
	}
	side.Sent = firstSent(rec, side.Items)
	return side
}

func latencyItems(flags []diff.LatencyFlag) []gateItem {
	var out []gateItem
	for _, f := range confirmedLatency(flags) {
		out = append(out, gateItem{Step: f.Step, Call: f.Call, Path: "latency", Want: fmt.Sprintf("%dms", f.BeforeMS), Got: fmt.Sprintf("%dms", f.AfterMS),
			Class: "latency", Own: methodName(f.Call) + " is slower than in the safe spot's run, confirmed by re-measurement or the previous run"})
	}
	return out
}

var gateRef = regexp.MustCompile(`\$\{\s*(?:steps\.)?([A-Za-z0-9_-]+)\.`)

func earlySidecar(e *env, rec *runner.Record) gateSidecar {
	side := gateSidecar{Reads: sessionReads(e, rec)}
	life := examineTokenLifetime(e, rec)
	if life == nil || life.finding() {
		return side
	}
	side.EarlyProfile = life.profile(life.first)
	side.EarlyAge, _ = life.first.r.Age()
	side.EarlyStated, _ = life.first.r.Stated()
	return side
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
	sent      map[string]string
	notes     []string
	items     []gateItem
	class     string
	firstAt   string
	failed    bool
	noVerdict bool
	pinsHeld  bool
	reported  bool
}

func runGate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	wait := fs.Duration("retry-wait", 20*time.Second, "wait before re-running a run or verify that exited 3")
	verbose := fs.Bool("v", false, "under each failing chain, every changed step and path; at the end, each distinct change once")
	noSessionCheck := fs.Bool("no-session-check", false, "after a token refused early once, do not hold a fresh one to tell a restart from sessions that end early")
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
	earlyAt := map[string]gateEarly{}
	reads := map[string]gateRead{}
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
			if p := out.side.EarlyProfile; p != "" {
				early[p]++
				if _, ok := earlyAt[p]; !ok {
					earlyAt[p] = gateEarly{age: out.side.EarlyAge, stated: out.side.EarlyStated}
				}
			}
			for p, read := range out.side.Reads {
				if _, ok := reads[p]; !ok {
					reads[p] = read
				}
			}
			g.absorb(what, out)
		}
	}
	settleGate(chains)
	headlineGate(chains)
	shown := map[string]bool{}
	for _, g := range chains {
		fmt.Println(g.line(width))
		if req := g.suspectLine(); req != "" && g.failed {
			fmt.Println("  " + req)
		}
		for _, n := range g.notes {
			key := noteKey(n)
			if shown[key] {
				fmt.Println("  " + key + ", as above")
				continue
			}
			shown[key] = true
			fmt.Println("  " + n)
		}
		if *verbose && g.failed {
			g.printChanges()
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
	once, check := []string{}, []string{}
	for _, p := range profiles {
		if early[p] <= 1 && !*noSessionCheck {
			check = append(check, p)
		}
	}
	checked := checkSessions(ctx, e, check, earlyAt, reads)
	for _, p := range profiles {
		if early[p] > 1 {
			findings = append(findings, fmt.Sprintf("FINDING: tokens of auth profile %s were refused early in %d runs of this gate: "+
				"the backend ends sessions long before the expiry its login states", p, early[p]))
			continue
		}
		if c, ok := checked[p]; ok {
			if c.finding {
				findings = append(findings, c.line)
			} else {
				fmt.Println(c.line)
			}
			continue
		}
		once = append(once, p)
	}
	switch len(once) {
	case 0:
	case 1:
		fmt.Printf("note: a token of auth profile %s was refused early once: a restart since it was cached, or sessions that end early; repeated on the re-login tokens of later runs it becomes a FINDING\n", once[0])
	default:
		fmt.Printf("note: a token of each of auth profiles %s was refused early once: a restart since they were cached, or sessions that end early; repeated on the re-login tokens of later runs it becomes a FINDING\n", strings.Join(once, ", "))
	}
	printGateGroups(chains)
	if *verbose {
		printDistinct(chains)
	}
	for _, f := range findings {
		fmt.Println(f)
	}
	if len(only) == 0 {
		if line := gateCoverage(e); line != "" {
			fmt.Println(line)
		}
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
					g.notes[i] = capNote(line, 240)
				}
			}
		}
		if !seen {
			g.notes = append(g.notes, capNote(line, 240))
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
			g.first, g.sent = "", nil
		}
		g.failed = true
		if g.first == "" {
			if len(out.side.Items) > 0 {
				it := out.side.Items[0]
				g.first = fmt.Sprintf("%s (%s) %s", it.Step, shortRPC(it.Call), it.headline())
				g.firstAt = it.Step + " " + it.Path
			} else {
				g.first = capText(what+": "+strings.TrimPrefix(why, "kept red, but it did not fail as pinned: "), 200)
			}
		}
		if what == "run" && out.side.KeptRed == runner.KeptRedNotAsPinned {
			g.class, g.pinsHeld = "not as pinned", out.side.PinsHeld
		}
		if what == "verify" {
			g.adoptBlame(out.side)
		}
		g.items = append(g.items, out.side.Items...)
		for step, sent := range out.side.Sent {
			if g.sent == nil {
				g.sent = map[string]string{}
			}
			if g.sent[step] == "" {
				g.sent[step] = sent
			}
		}
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

func capNote(line string, n int) string {
	parts := strings.Split(line, "; ")
	out := capText(parts[0], 2*n)
	for _, p := range parts[1:] {
		if len(out)+2+len(p) > n {
			return out + "; ..."
		}
		out += "; " + p
	}
	return out
}

func (g *gateChain) intermittentFirst() bool {
	it, ok := g.firstItem()
	if !ok {
		return false
	}
	for _, n := range g.notes {
		if head, _, _ := strings.Cut(strings.TrimPrefix(n, "FINDING: intermittent failure at "), ": "); head != n && containsName(strings.Split(head, ", "), shortRPC(it.Call)) {
			return true
		}
	}
	return false
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
}

func (g *gateChain) movedPin() *gateItem {
	for i := range g.items {
		if g.items[i].Pinned != "" {
			return &g.items[i]
		}
	}
	return nil
}

func (g *gateChain) firstItem() (gateItem, bool) {
	for _, it := range g.items {
		if it.Step+" "+it.Path == g.firstAt {
			return it, true
		}
	}
	return gateItem{}, false
}

func (g *gateChain) suspectLine() string {
	it, ok := g.firstItem()
	if !ok || g.reported {
		return ""
	}
	step, lead := it.Step, it.Step
	switch {
	case it.Own != "":
		lead = fmt.Sprintf("suspect read %s (%s)", it.Step, shortRPC(it.Call))
	case it.SuspectStep != "":
		step, lead = it.SuspectStep, fmt.Sprintf("suspect write %s (%s)", it.SuspectStep, shortRPC(it.Suspect))
	}
	if g.sent[step] == "" {
		return ""
	}
	return lead + g.sent[step]
}

func (g *gateChain) printChanges() {
	seen := map[string]bool{}
	var paths, because []string
	steps := map[string][]string{}
	example := map[string]string{}
	cascades := map[string]int{}
	for _, it := range g.items {
		if seen[it.Step+" "+it.Path] {
			continue
		}
		seen[it.Step+" "+it.Path] = true
		if it.Cascade != "" {
			if cascades[it.Cascade] == 0 {
				because = append(because, it.Cascade)
			}
			cascades[it.Cascade]++
			continue
		}
		path, eg := it.shown()
		if it.Variant != "" {
			path += " after " + methodName(it.suspectKey())
		}
		if steps[path] == nil {
			paths = append(paths, path)
			example[path] = eg
		}
		if !containsName(steps[path], it.Step) {
			steps[path] = append(steps[path], it.Step)
		}
	}
	var sets []string
	together := map[string][]string{}
	for _, p := range paths {
		key := strings.Join(steps[p], " ")
		if together[key] == nil {
			sets = append(sets, key)
		}
		together[key] = append(together[key], p)
	}
	for _, key := range sets {
		ps := together[key]
		eg := example[ps[0]]
		if len(ps) > 1 {
			eg = ps[0] + " " + eg
		}
		fmt.Printf("    %s at %d step(s) (%s); e.g. %s\n", capList(ps, 4), len(steps[ps[0]]), capList(steps[ps[0]], 3), eg)
	}
	for _, c := range because {
		fmt.Printf("    %d step(s) %s\n", cascades[c], c)
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
		switch {
		case g.failed && g.intermittentFirst():
			line += "intermittent: "
		case g.class != "" && g.failed:
			line += g.class + ": "
		}
		line += g.first
	}
	return strings.TrimRight(line, " ")
}

var gateIndex = regexp.MustCompile(`\.\d+(\.|$)`)

type gateGroup struct {
	rpc, suspect  string
	write         bool
	own           []string
	why           []string
	knockOn       bool
	steps, chains map[string]bool
	paths         []string
	example       string
	reads         map[string]bool
	readRPCs      []string
	readPaths     map[string][]string
	cascades      []string
	cascade       map[string]map[string]bool
}

func (gr *gateGroup) addPath(list *[]string, p string) {
	if !containsName(*list, p) {
		*list = append(*list, p)
	}
}

func settleGate(chains []*gateChain) {
	readAfter := map[string][]string{}
	for _, g := range chains {
		for _, it := range g.items {
			if key := methodName(it.Call) + " " + gateIndex.ReplaceAllString(it.Path, "[]$1"); it.Suspect != "" && !it.KnockOn && it.Own == "" &&
				it.Cascade == "" && it.Pinned == "" && chain.IsReadOnlyCall(it.Call) && !containsName(readAfter[key], methodName(it.Suspect)) {
				readAfter[key] = append(readAfter[key], methodName(it.Suspect))
			}
		}
	}
	for _, g := range chains {
		for i, it := range g.items {
			path := gateIndex.ReplaceAllString(it.Path, "[]$1")
			writes := readAfter[methodName(it.Call)+" "+path]
			if it.Pinned != "" && it.Suspect != "" && it.Own == "" && it.Cascade == "" && len(writes) > 0 && !containsName(writes, methodName(it.Suspect)) {
				g.items[i].or = capList(writes, 2)
			}
			if len(writes) > 1 && it.Suspect != "" && !it.KnockOn && it.Own == "" && it.Cascade == "" && !it.Firm && it.Pinned == "" {
				g.items[i].Own = fmt.Sprintf("%s changes %s after %d different writes (%s)", methodName(it.Call), path, len(writes), capList(writes, 3))
				g.items[i].Suspect, g.items[i].SuspectStep, g.items[i].Why = "", "", ""
			}
		}
	}
}

func printGateGroups(chains []*gateChain) {
	settleGate(chains)
	groups := map[string]*gateGroup{}
	order := []string{}
	group := func(rpc string) *gateGroup {
		if groups[rpc] == nil {
			groups[rpc] = &gateGroup{rpc: rpc, steps: map[string]bool{}, chains: map[string]bool{}, reads: map[string]bool{},
				readPaths: map[string][]string{}, cascade: map[string]map[string]bool{}}
			order = append(order, rpc)
		}
		return groups[rpc]
	}
	for _, g := range chains {
		for _, it := range g.items {
			path, eg := it.shown()
			step := g.name + " " + it.Step
			example := fmt.Sprintf("%s %s %s %s", g.name, it.Step, path, eg)
			own := func(gr *gateGroup) {
				gr.steps[step] = true
				gr.chains[g.name] = true
				gr.addPath(&gr.paths, path)
				if gr.example == "" {
					gr.example = example
				}
			}
			switch {
			case it.or != "":
				continue
			case it.Own != "":
				gr := group(shortRPC(it.Call))
				gr.addPath(&gr.own, it.Own)
				own(gr)
			case it.Suspect != "" && it.Cascade != "":
				gr := group(it.suspectKey())
				gr.write = gr.write || !chain.IsReadOnlyCall(it.Suspect)
				gr.chains[g.name] = true
				if gr.cascade[it.Cascade] == nil {
					gr.cascade[it.Cascade] = map[string]bool{}
					gr.cascades = append(gr.cascades, it.Cascade)
				}
				gr.cascade[it.Cascade][step] = true
				gr.cascade[it.Cascade]["\x00"+g.name] = true
				if gr.suspect == "" {
					gr.suspect = g.name + " " + it.SuspectStep
				}
			case it.Suspect != "" && !it.KnockOn:
				gr := group(it.suspectKey())
				gr.write = true
				gr.reads[step] = true
				gr.chains[g.name] = true
				if it.Why != "" {
					gr.addPath(&gr.why, it.Why)
				}
				read := methodName(it.Call)
				gr.addPath(&gr.readRPCs, read)
				paths := gr.readPaths[read]
				gr.addPath(&paths, path)
				gr.readPaths[read] = paths
				if gr.suspect == "" {
					gr.suspect = g.name + " " + it.SuspectStep
				}
			case it.Suspect == "" && !chain.IsReadOnlyCall(it.Call):
				gr := group(shortRPC(it.Call))
				gr.write = true
				own(gr)
			default:
				gr := group(shortRPC(it.Call))
				if it.KnockOn {
					gr.knockOn, gr.suspect = true, shortRPC(it.Suspect)
				}
				own(gr)
			}
		}
	}
	if len(order) == 0 {
		return
	}
	suspect := func(gr *gateGroup) bool { return gr.write || len(gr.own) > 0 }
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]], groups[order[j]]
		if suspect(a) != suspect(b) {
			return suspect(a)
		}
		if len(a.chains) != len(b.chains) {
			return len(a.chains) > len(b.chains)
		}
		return len(a.steps)+len(a.reads) > len(b.steps)+len(b.reads)
	})
	fmt.Println("failures by suspect rpc (the read itself, or the failing or changed write it observes), then the rest:")
	for _, key := range order {
		gr := groups[key]
		switch {
		case len(gr.steps) > 0:
			tail := ""
			switch {
			case len(gr.own) > 0:
				tail = "; suspect the read: " + gr.own[0] + otherReasons(len(gr.own)-1)
			case gr.write:
			case gr.knockOn:
				tail = "; a knock-on of " + gr.suspect
			default:
				tail = "; no suspect write"
			}
			fmt.Printf("  %s: %d step(s) in %d chain(s), paths %s%s; e.g. %s\n", gr.rpc, len(gr.steps), len(gr.chains), capList(gr.paths, 3), tail, gr.example)
		case len(gr.why) > 0:
			fmt.Printf("  %s: suspect the write: %s%s; e.g. %s\n", gr.rpc, gr.why[0], otherReasons(len(gr.why)-1), gr.suspect)
		case len(gr.reads) > 0:
			fmt.Printf("  %s: passed itself, but steps after it failed or changed; e.g. %s\n", gr.rpc, gr.suspect)
		default:
			fmt.Printf("  %s: passed itself, but steps reading it went unevaluated; e.g. %s\n", gr.rpc, gr.suspect)
		}
		if len(gr.reads) > 0 {
			parts := []string{}
			for _, read := range gr.readRPCs {
				parts = append(parts, read+" "+capList(gr.readPaths[read], 3))
			}
			fmt.Printf("    +%d step(s) after it: %s\n", len(gr.reads), capList(parts, 4))
		}
		for _, c := range gr.cascades {
			steps, in := 0, 0
			for k := range gr.cascade[c] {
				if strings.HasPrefix(k, "\x00") {
					in++
				} else {
					steps++
				}
			}
			fmt.Printf("    +%d step(s) in %d chain(s) %s\n", steps, in, c)
		}
	}
}

func rootOf(it gateItem) string {
	if it.Suspect != "" && it.Own == "" {
		return it.suspectKey()
	}
	return shortRPC(it.Call)
}

func leafOf(path string) string {
	segs := chain.SplitPath(path)
	for i := len(segs) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(segs[i]); err != nil {
			return segs[i]
		}
	}
	return path
}

func headlineGate(chains []*gateChain) {
	label := map[string]string{}
	for _, g := range chains {
		for _, it := range g.items {
			if r := rootOf(it); label[r] == "" && it.Suspect == "" {
				path, _ := it.shown()
				label[r] = methodName(r) + " " + leafOf(path)
			}
		}
	}
	name := func(r string) string {
		if label[r] != "" {
			return label[r]
		}
		return methodName(r)
	}
	reported := map[string]bool{}
	for _, g := range chains {
		if !g.failed || len(g.items) == 0 {
			continue
		}
		var head *gateItem
		steps, from := map[string]bool{}, []string{}
		for i, it := range g.items {
			r := rootOf(it)
			switch {
			case reported[r]:
				steps[it.Step] = true
				if !containsName(from, name(r)) {
					from = append(from, name(r))
				}
			case head == nil:
				head = &g.items[i]
			}
		}
		for i, it := range g.items {
			if head != nil && head.Kind == "refused" && it.Step == head.Step && it.Path == chain.EnvelopePath() {
				head = &g.items[i]
			}
		}
		if pin := g.movedPin(); pin != nil {
			r := rootOf(*pin)
			text := fmt.Sprintf("%s %s pinned got=%s, now got=%s", pin.Step, pin.Path, pin.Pinned, pin.Got)
			switch {
			case pin.Own != "":
				text += "; suspect the read: " + pin.Own
			case pin.or != "":
				text += "; suspect " + methodName(r) + ", or " + pin.or + " as at other steps reading " + methodName(pin.Call) + " " + leafOf(pin.Path)
			case pin.Suspect != "":
				text += "; suspect " + methodName(r)
			}
			switch {
			case pin.or != "":
				g.reported = true
			case reported[r]:
				text, g.reported = text+", reported above", true
			default:
				g.firstAt, reported[r] = pin.Step+" "+pin.Path, true
			}
			g.class, g.first = "not as pinned", text
			continue
		}
		if len(from) == 0 {
			if it, ok := g.firstItem(); ok {
				reported[rootOf(it)] = true
			}
			continue
		}
		ref := fmt.Sprintf("%d step(s) from %s, reported above", len(steps), strings.Join(from, ", "))
		switch {
		case head == nil && g.pinsHeld:
			g.class, g.first, g.reported = "kept red, drifted", "pinned defect unchanged; setup drift from "+strings.Join(from, ", ")+" (reported above)", true
		case head == nil:
			g.first, g.reported = ref, true
		default:
			g.first = fmt.Sprintf("%s (%s) %s (+%s)", head.Step, shortRPC(head.Call), head.headline(), ref)
			g.firstAt = head.Step + " " + head.Path
			if head.Class != "" && g.class != "not as pinned" {
				g.class = head.Class
			}
			reported[rootOf(*head)] = true
		}
	}
}

func printDistinct(chains []*gateChain) {
	type row struct {
		example       string
		exact         bool
		steps, chains map[string]bool
	}
	own := func(it gateItem) bool { return it.Suspect == "" || it.Own != "" }
	roots := map[string]bool{}
	for _, g := range chains {
		for _, it := range g.items {
			if own(it) {
				path, _ := it.shown()
				roots[shortRPC(it.Call)+" "+leafOf(path)] = true
			}
		}
	}
	rows, order := map[string]*row{}, []string{}
	for _, g := range chains {
		for _, it := range g.items {
			path, kind := gateIndex.ReplaceAllString(it.Path, "[]$1"), "value"
			switch it.Kind {
			case "refused":
				path, kind = chain.EnvelopePath(), "refused"
			case "order":
				path, kind = listOf(it.Path), "order"
			case "membership":
				path, _, _ = strings.Cut(path, "[]")
				kind = "membership"
			}
			key := shortRPC(it.Call) + " " + path + " " + kind
			switch {
			case own(it):
			case it.Cascade != "" || it.KnockOn || it.or != "" || roots[shortRPC(it.Suspect)+" "+leafOf(path)]:
				continue
			default:
				key = it.suspectKey() + " -> " + methodName(it.Call) + " " + path + " " + kind
			}
			if rows[key] == nil {
				rows[key] = &row{steps: map[string]bool{}, chains: map[string]bool{}}
				order = append(order, key)
			}
			if rows[key].example == "" || kind == "refused" && it.Path == path && !rows[key].exact {
				_, eg := it.shown()
				rows[key].example, rows[key].exact = g.name+" "+it.Step+" "+path+" "+eg, it.Path == path
				if kind != "order" {
					rows[key].example = g.name + " " + it.Step + " " + it.verdict()
				}
				if !own(it) && g.sent[it.SuspectStep] != "" {
					rows[key].example += "; " + it.SuspectStep + g.sent[it.SuspectStep]
				}
			}
			rows[key].steps[g.name+" "+it.Step] = true
			rows[key].chains[g.name] = true
		}
	}
	if len(order) == 0 {
		return
	}
	fmt.Println("distinct changes (suspect rpc, path, kind):")
	for _, key := range order {
		r := rows[key]
		fmt.Printf("  %s: %d step(s) in %d chain(s); e.g. %s\n", key, len(r.steps), len(r.chains), r.example)
	}
}

func otherReasons(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (+%d other reason(s))", n)
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
