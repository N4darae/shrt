package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{name: "gate", summary: "verify every chain with a safe spot, run the rest, grouping what failed", run: runGate})
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
	RunToo       bool                `json:"run_too,omitempty"`
	Flaky        []gateFlaky         `json:"flaky,omitempty"`
	FlakyOnly    bool                `json:"flaky_only,omitempty"`
	Errors       []gateFlaky         `json:"errors,omitempty"`
	Notes        []string            `json:"notes,omitempty"`
	Latency      []diff.LatencyFlag  `json:"latency,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type gateFlaky struct {
	Call     string   `json:"call"`
	Failed   int      `json:"failed"`
	Calls    int      `json:"calls"`
	Every    int      `json:"every,omitempty"`
	Repeated bool     `json:"repeated,omitempty"`
	Steps    []string `json:"steps,omitempty"`

	promoted bool
}

type gateItem struct {
	Step   string `json:"step"`
	Call   string `json:"call"`
	Path   string `json:"path"`
	Rule   string `json:"rule,omitempty"`
	Want   string `json:"want"`
	Got    string `json:"got"`
	Reason reason `json:"reason"`
	Class  string `json:"class,omitempty"`
	Length string `json:"length,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Pinned string `json:"pinned,omitempty"`
	Failed bool   `json:"failed,omitempty"`
	Passes bool   `json:"passes,omitempty"`

	from string
}

func (it gateItem) suspect() string {
	return it.Reason.blamed(it.Step)
}

func (it gateItem) rpc() string {
	call := it.Reason.rpc(it.Call)
	if call == "" && it.Path == "step" {
		call = it.Want
	}
	if call == "" {
		return "(no rpc)"
	}
	return shortRPC(call)
}

func (it gateItem) callNote() string {
	call := it.Call
	if call == "" && it.Path == "step" {
		call = it.Want
	}
	if call == "" {
		return ""
	}
	return " (" + shortRPC(call) + ")"
}

func (it gateItem) root() string {
	switch {
	case it.Reason.Kind == reasonStoredOrder:
		return it.rpc() + " " + leafOf(listOf(it.Path))
	case it.Reason.Kind != "" && !it.Reason.blames():
		return it.rpc() + " " + it.Reason.Kind
	}
	return it.rpc() + " " + leafOf(it.Path)
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

func writeGateSidecar(sidecar func() gateSidecar, err error) {
	path := os.Getenv(gateReportEnv)
	if path == "" {
		return
	}
	side := sidecar()
	if err != nil {
		side.Error, _, _ = strings.Cut(err.Error(), "\n")
	}
	if raw, err := json.Marshal(side); err == nil {
		_ = os.WriteFile(path, raw, 0o600)
	}
}

func runSidecar(e *env, c *chain.Chain, rec *runner.Record, drift []diff.Change, against *diff.RunReport, ref *runner.Record) gateSidecar {
	side := earlySidecar(e, rec)
	changedPins, held := runner.PinChanges(c, rec)
	side.KeptRed, side.PinsHeld = rec.KeptRed, held || runner.PinsHeld(c, rec)
	pinned, heldPins := map[string]bool{}, map[string]bool{}
	for _, p := range c.KeptRed {
		pinned[p.Step+" "+p.Path] = true
		if _, moved := changedPins[p.Step+" "+p.Path]; !moved {
			heldPins[p.Step+" "+p.Path] = true
		}
	}
	a := pinnedAttribution(e, rec, heldPins)
	var since []diff.Change
	if ref != nil && !rec.Passed() {
		since = diff.CompareRunsSkipping(ref, rec, currentVolatile(e, rec.Chain), requestFixtures(c)).Changes
		a.was = wasOr(a.was, since)
	}
	for _, st := range rec.Steps {
		if st == nil || st.Status == runner.StatusSkipped {
			continue
		}
		found := false
		for _, ex := range st.Expect {
			was, moved := changedPins[st.ID+" "+ex.Path]
			if !moved && (ex.Passed || ex.Rule == "unevaluated" || pinned[st.ID+" "+ex.Path]) {
				continue
			}
			found = found || !ex.Passed
			want, got := gatePair(ex.Want, ex.Got)
			it := a.item(gateItem{Step: st.ID, Call: st.Call, Path: ex.Path, Rule: ex.Rule, Want: want, Got: got, Passes: ex.Passed, Failed: !ex.Passed})
			if moved {
				it.Pinned, _ = gatePair(was, ex.Got)
			}
			it.Length = pastEnd(st, ex.Path)
			side.Items = append(side.Items, it)
		}
		if !found && st.Status != runner.StatusPassed && st.Error != "" && !slices.ContainsFunc(c.KeptRed, func(p chain.Pin) bool { return p.Step == st.ID }) {
			why, _, _ := strings.Cut(st.Error, "\n")
			side.Items = append(side.Items, a.item(gateItem{Step: st.ID, Call: st.Call, Path: "(" + st.Status + ")", Got: capText(why, 160)}))
		}
	}
	side.Items = withRootChanges(a, rec, side.Items, since)
	if against != nil {
		a = changesAttribution(e, rec, against.Changes)
	}
	for _, ch := range drift {
		if st, ok := rec.Step(ch.Step); ok && st != nil {
			want, got := gatePair(ch.Want, ch.Got)
			side.Items = append(side.Items, a.item(gateItem{Step: ch.Step, Call: st.Call, Path: ch.Path, Want: want, Got: got}))
		}
	}
	side.Sent = firstSent(e, rec, side.Items)
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

func firstSent(e *env, rec *runner.Record, items []gateItem) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out, roots := map[string]string{}, map[string]bool{}
	for _, it := range items {
		if roots[it.root()] || len(roots) == 5 {
			continue
		}
		roots[it.root()] = true
		for _, step := range []string{it.Step, it.Reason.Step, it.Reason.Read} {
			if st, ok := rec.Step(step); ok && st != nil {
				if sent := sentText(e, st); sent != "" {
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
	return pinnedAttribution(e, rec, nil)
}

func pinnedAttribution(e *env, rec *runner.Record, held map[string]bool) attribution {
	moved := func(st *runner.StepRecord) *runner.StepRecord {
		if len(held) == 0 {
			return st
		}
		cp := *st
		cp.Expect = nil
		for _, ex := range st.Expect {
			if !held[st.ID+" "+ex.Path] {
				cp.Expect = append(cp.Expect, ex)
			}
		}
		return &cp
	}
	return attribution{e: e, rec: rec, bad: badSteps(rec),
		bodies: map[*runner.StepRecord]stepBody{}, produced: map[int]map[string]string{},
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
			return ok && st != nil && runner.ReorderedPaths(moved(st))[path]
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
			return listUnder(runResized(moved(st)), path)
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

func withRootChanges(a attribution, rec *runner.Record, items []gateItem, since []diff.Change) []gateItem {
	has, need := map[string]bool{}, map[string]bool{}
	for _, it := range items {
		has[it.Step] = true
		need[it.suspect()] = it.suspect() != ""
	}
	for _, ch := range since {
		st, ok := rec.Step(ch.Step)
		if !need[ch.Step] || has[ch.Step] || ch.Kind != diff.KindChanged || !ok || st == nil {
			continue
		}
		want, got := gatePair(ch.Want, ch.Got)
		it := a.item(gateItem{Step: ch.Step, Call: st.Call, Path: ch.Path, Want: want, Got: got})
		at := len(items)
		for i, x := range items {
			if a.index(x.Step) > a.index(ch.Step) {
				at = i
				break
			}
		}
		items = append(items[:at], append([]gateItem{it}, items[at:]...)...)
	}
	return items
}

func wasOr(was func(step, path string) (any, bool), changes []diff.Change) func(step, path string) (any, bool) {
	return func(step, path string) (any, bool) {
		if v, ok := was(step, path); ok {
			return v, ok
		}
		for _, c := range changes {
			if c.Step == step && c.Path == path && c.Kind == diff.KindChanged {
				return c.Want, true
			}
		}
		return nil, false
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
				have := 0
				for _, it := range items {
					if got, ok := chain.Get(it, rest); ok && compactValue(got) == compactValue(ex.Want) {
						have++
					}
				}
				member = have < wantedAt(st.Expect, list, rest, ex.Want)
			}
			if member {
				out = append(out, list)
			}
			break
		}
	}
	return out
}

func wantedAt(expect []chain.ExpectResult, list, rest string, want any) int {
	n := 0
	for _, ex := range expect {
		segs := chain.SplitPath(ex.Path)
		for k := 1; k < len(segs); k++ {
			if _, err := strconv.Atoi(segs[k]); err == nil {
				if ex.Rule == "equals" && strings.Join(segs[:k], ".") == list && strings.Join(segs[k+1:], ".") == rest && compactValue(ex.Want) == compactValue(want) {
					n++
				}
				break
			}
		}
	}
	return n
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
	a := changesAttribution(e, rec, report.Changes)
	moved := report.Mover()
	a.reordered = func(step, path string) bool {
		return report.Class(diff.Change{Step: step, Path: path}) == "order changed" || moved(step, path)
	}
	return a
}

func changesAttribution(e *env, rec *runner.Record, changes []diff.Change) attribution {
	bad, changedAt := map[string]bool{}, map[string][]string{}
	for _, c := range changes {
		if c.Kind != diff.KindNotReached {
			bad[c.Step] = true
		}
		if c.Kind != diff.KindNotReached && c.Kind != diff.KindStatus {
			changedAt[c.Step] = append(changedAt[c.Step], c.Path)
		}
	}
	return attribution{e: e, rec: rec, bad: bad, ref: true,
		bodies: map[*runner.StepRecord]stepBody{}, produced: map[int]map[string]string{},
		unchanged: func(step, path string) bool {
			held := ""
			if st, ok := rec.Step(step); ok && st != nil {
				held, _ = heldBackBy(st)
			}
			for _, c := range changes {
				if c.Step != step || c.Kind == diff.KindNotReached || c.Kind == diff.KindStatus && held != "" {
					continue
				}
				if c.Kind == diff.KindStatus || c.Path == path || strings.HasPrefix(path, c.Path+".") || strings.HasPrefix(c.Path, path+".") {
					return false
				}
			}
			return true
		},
		reordered: func(step, path string) bool {
			return slices.ContainsFunc(changes, func(c diff.Change) bool {
				return c.Step == step && c.Kind == diff.KindOrder && (c.Path == path || strings.HasPrefix(path, c.Path+"."))
			})
		},
		changed: func(step string) []string {
			return changedAt[step]
		},
		resized: func(step, path string) string {
			var lists []string
			for _, c := range changes {
				if c.Step == step && (c.Kind == diff.KindLength || c.Kind == diff.KindMembership) {
					lists = append(lists, c.Path)
				}
			}
			return listUnder(lists, path)
		},
		was: func(step, path string) (any, bool) {
			for _, c := range changes {
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
	it.Reason = a.of(it.Step, path)
	if st, ok := a.rec.Step(it.Step); ok && st != nil {
		switch {
		case a.flipped(st) != "":
		case path != "" && a.reordered != nil && a.reordered(it.Step, path):
			it.Kind = "order"
		case path != "" && a.resized != nil && a.resized(it.Step, path) != "":
			it.Kind = "membership"
		}
	}
	return it
}

func asOf(e *env, w *runner.StepRecord) string {
	if p := profileAs(e, w); p != "" {
		return "as " + p
	}
	return ""
}

func profileAs(e *env, w *runner.StepRecord) string {
	p := profileOf(w)
	if p == "default" || p == runner.NoAuthProfile && e != nil && e.cfg != nil && e.cat != nil && loginRPCs(e)[w.Call] {
		return ""
	}
	return p
}

func (it gateItem) shown() (string, string) {
	if it.Kind == "order" {
		return listOf(it.Path), "same items in another order"
	}
	return gateIndex.ReplaceAllString(it.Path, "[]$1"), it.wantGot()
}

func listOf(path string) string {
	segs := chain.SplitPath(path)
	for i := len(segs) - 1; i > 0; i-- {
		if _, err := strconv.Atoi(segs[i]); err == nil {
			return gateIndex.ReplaceAllString(strings.Join(segs[:i], "."), "[]$1")
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
	}
	return it.verdict()
}

func (it gateItem) wantGot() string {
	if strings.HasPrefix(it.Path, "(") {
		return it.Got
	}
	if it.Pinned != "" {
		return fmt.Sprintf("pinned got=%s, now got=%s", it.Pinned, it.Got)
	}
	if it.Class == "latency" {
		was, _ := strconv.Atoi(strings.TrimSuffix(it.Want, "ms"))
		now, _ := strconv.Atoi(strings.TrimSuffix(it.Got, "ms"))
		return fmt.Sprintf("safe spot %s, now %s (%+dms)", it.Want, it.Got, now-was)
	}
	return chain.WantGot(it.Rule, it.Want, it.Got)
}

func verifyItems(e *env, rec *runner.Record, report *diff.Report) []gateItem {
	var items []gateItem
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
		call, rule, path, failed := "", "", c.Path, false
		want, got := gatePair(c.Want, c.Got)
		if st, ok := rec.Step(c.Step); ok && st != nil {
			call = st.Call
			for _, ex := range st.Expect {
				if !ex.Passed && ex.Rule != "unevaluated" && (c.Kind == diff.KindStatus || namecase.Equal(ex.Path, c.Path)) {
					rule, path, failed = ex.Rule, ex.Path, true
					want, got = gatePair(ex.Want, ex.Got)
					break
				}
			}
		}
		if (c.Kind == diff.KindLength || c.Kind == diff.KindMembership) && !failed {
			if c.Detail != "" {
				got = capText(got+" ("+c.Detail+")", 320)
			}
		}
		it := a.item(gateItem{Step: c.Step, Call: call, Path: path, Rule: rule, Want: want, Got: got, Failed: failed})
		it.Class = report.Class(c)
		for _, l := range report.Changes {
			if l.Kind == diff.KindLength && l.Detail != diff.VolatileFailed && l.Step == c.Step && (l.Path == c.Path || strings.HasPrefix(c.Path, l.Path+".")) {
				it.Length = fmt.Sprintf("%s length want=%s got=%s", l.Path, compactValue(l.Want), compactValue(l.Got))
				break
			}
		}
		items = append(items, it)
	}
	if first, _ := firstChange(report, rec); first != nil {
		at := func(it gateItem) bool {
			return it.Step == first.Step && (it.Path == first.Path || first.Kind == diff.KindStatus)
		}
		sort.SliceStable(items, func(i, j int) bool {
			return at(items[i]) && !at(items[j]) || items[i].Step == first.Step && items[j].Step != first.Step
		})
	}
	return items
}

func latencyItems(flags []diff.LatencyFlag) []gateItem {
	var out []gateItem
	for _, f := range confirmedLatency(flags) {
		out = append(out, gateItem{Step: f.Step, Call: f.Call, Path: "latency", Want: fmt.Sprintf("%dms", f.BeforeMS), Got: fmt.Sprintf("%dms", f.AfterMS),
			Class: "latency", Reason: reason{Kind: reasonSlow, Step: f.Step, RPC: f.Call}})
	}
	return out
}

func earlySidecar(e *env, rec *runner.Record) gateSidecar {
	side := gateSidecar{Reads: sessionReads(e, rec), Errors: serverErrors(rec)}
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
	return capPair(shownValue(want), shownValue(got), 60)
}

func shownValue(v any) string {
	if s, ok := v.(string); ok {
		return chain.EdgeQuoted(s)
	}
	return compactValue(v)
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
	keptRed   string
	flaky     map[string]gateFlaky
	flakyOnly bool
	otherFail bool
	flakyKind map[string]string
	errors    map[string]gateFlaky
	errored   map[string]string
	slow      map[string]bool
	echoOf    string
	slices    []string
	shown     []string
	waits     time.Duration
	took      time.Duration
	queued    string
}

func (g *gateChain) findingOnly() bool {
	return g.failed && g.flakyOnly && !g.otherFail
}

func (g *gateChain) flakyCalls() []string {
	return sortedKeys(g.flaky)
}

func (g *gateChain) flakyKindOf(call string) string {
	if k := g.flakyKind[call]; k != "" {
		return k
	}
	if g.flaky[call].Repeated {
		return "repeated"
	}
	return "intermittent"
}

func settleFlaky(chains []*gateChain) []string {
	type total struct {
		gateFlaky
		chains   int
		repeated bool
		judged   bool
		every    map[int]bool
	}
	totals, order := map[string]*total{}, []string{}
	for _, g := range chains {
		for _, call := range g.flakyCalls() {
			f := g.flaky[call]
			t := totals[call]
			if t == nil {
				t = &total{gateFlaky: gateFlaky{Call: call}, repeated: true, every: map[int]bool{}}
				totals[call] = t
				order = append(order, call)
			}
			t.Failed += f.Failed
			t.Calls += f.Calls
			t.chains++
			t.repeated = t.repeated && (f.Repeated || f.promoted)
			t.judged = t.judged || !f.promoted
			if f.Every > 1 {
				t.every[f.Every] = true
			}
		}
	}
	var out []string
	said := map[string]bool{}
	for _, call := range order {
		t := totals[call]
		kind := "intermittent"
		if t.repeated && t.judged {
			kind = "repeated"
		}
		for _, g := range chains {
			if _, ok := g.flaky[call]; ok {
				if g.flakyKind == nil {
					g.flakyKind = map[string]string{}
				}
				g.flakyKind[call] = kind
			}
		}
		r := t.gateFlaky
		if len(t.every) == 1 {
			for k := range t.every {
				r.Every = k
			}
		}
		line := fmt.Sprintf("FINDING: %s failure at %s (%s) in %d chain(s)", kind, shortRPC(call), r.text(), t.chains)
		if !said[kind] {
			said[kind] = true
			line += ": " + findingMeaning(kind == "repeated", "those steps")
		}
		out = append(out, line)
	}
	return out
}

func foldFlaky(chains []*gateChain) {
	found := map[string]bool{}
	for _, g := range chains {
		for call := range g.flaky {
			found[call] = true
		}
		for call, e := range g.errors {
			found[call] = found[call] || e.Every > 1
		}
	}
	explained := make([]map[string]bool, len(chains))
	for i, g := range chains {
		explained[i] = g.explainedBy(found)
	}
	folded := func(i int, it gateItem) bool {
		return explained[i][it.from+" "+it.Step] || explained[i][it.from+" "+it.suspect()]
	}
	listKey := func(it gateItem) string {
		return methodName(it.Call) + " " + gateIndex.ReplaceAllString(it.Path, "[]$1")
	}
	elsewhere := map[string]bool{}
	for i, g := range chains {
		for _, it := range g.items {
			if it.Kind == "membership" && !folded(i, it) {
				elsewhere[listKey(it)] = true
			}
		}
	}
	for i, g := range chains {
		kept := g.items[:0:0]
		for _, it := range g.items {
			if !folded(i, it) || it.Kind == "membership" && elsewhere[listKey(it)] {
				kept = append(kept, it)
			}
		}
		if len(kept) == len(g.items) || !g.failed {
			continue
		}
		if len(kept) == 0 {
			g.items, g.flakyOnly, g.otherFail = nil, true, false
			continue
		}
		g.items = kept
	}
}

func (g *gateChain) explainedBy(found map[string]bool) map[string]bool {
	for call, e := range g.errors {
		if _, ok := g.flaky[call]; !ok && found[call] {
			if g.flaky == nil {
				g.flaky = map[string]gateFlaky{}
			}
			e.promoted = true
			g.flaky[call] = e
		}
	}
	explained := map[string]bool{}
	for grew := len(g.errored) > 0; grew; {
		grew = false
		own := map[string]bool{}
		for _, it := range g.items {
			key := it.from + " " + it.Step
			if _, ok := own[key]; !ok {
				own[key] = true
			}
			call := g.errored[key]
			own[key] = own[key] && (call == it.Call && found[call] || explained[it.from+" "+it.suspect()])
		}
		for step, ok := range own {
			if ok && !explained[step] {
				explained[step], grew = true, true
			}
		}
	}
	return explained
}

func runGate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	wait := fs.Duration("retry-wait", 20*time.Second, "wait before re-running a run or verify that exited 3")
	verbose := fs.Bool("v", false, "under each failing chain, the suspect's request and every change with its want and got, as verify prints it; knock-on counts in the summary")
	noSessionCheck := fs.Bool("no-session-check", false, "after a token refused early once, do not hold a fresh one to tell a restart from sessions that end early")
	hollowBaseline := fs.String("hollow-baseline", ".shrt/hollow-baseline", "`file` for the chain hollow ratchet; empty skips it")
	setUsage(fs, "usage: shrt gate [<chain>...] [flags]   verify each chain with a safe spot, run the rest (a fresh -var tag each), group what failed", gateExitCodes)
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
	width := 0
	for _, g := range chains {
		width = max(width, len(g.name))
	}
	began := time.Now()
	outs := sendGate(ctx, e, chains, *wait)
	early := map[string]int{}
	earlyAt := map[string]gateEarly{}
	reads := map[string]gateRead{}
	for i, g := range chains {
		for _, what := range []string{"run", "verify"} {
			out, ok := outs[i][what]
			if !ok {
				continue
			}
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
	flakyFindings := settleGate(chains)
	for _, g := range chains {
		if g.echoOf != "" {
			continue
		}
		fmt.Println(g.line(width))
		if note := g.flakyNote(); note != "" {
			fmt.Println("  " + note)
		}
		for _, n := range g.notes {
			fmt.Println("  " + capText(n, 240))
		}
		if *verbose && g.failed && !g.findingOnly() {
			g.printChanges(e)
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
	findings := flakyFindings
	if *hollowBaseline != "" && len(only) == 0 {
		if f := gateHollow(ctx, *hollowBaseline); f != "" {
			findings = append(findings, f)
		}
	}
	profiles := sortedKeys(early)
	once, check := []string{}, []string{}
	for _, p := range profiles {
		if early[p] <= 1 && !*noSessionCheck {
			check = append(check, p)
		}
	}
	checked := checkSessions(ctx, e, check, earlyAt, reads, *verbose)
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
	printGateGroups(chains, *verbose)
	for _, f := range findings {
		fmt.Println(f)
	}
	if len(only) == 0 {
		if line := gateCoverage(e); line != "" {
			fmt.Println(line)
		}
	}
	if line := gateTime(chains, time.Since(began)); line != "" {
		fmt.Println(line)
	}
	switch {
	case failed > 0 || len(findings) > 0:
		next := "every changed value: shrt gate -v <chain>... (re-sends only those), or shrt verify <chain> -run latest (offline)"
		if *verbose {
			next = "next: shrt diff <chain> -step <id> (a step's request and response as recorded), shrt chain slice <chain> -without <step> -verify (is a suspect write the cause)"
		}
		return exitWith(1, "FAIL: %d of %d chain(s) failed%s; %s", failed, len(chains), gateAlso(unverified, len(findings)), next)
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
		out += fmt.Sprintf(", %d finding(s) listed above", findings)
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
		if len(only) == 0 || slices.Contains(only, n) {
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

func sendGate(ctx context.Context, e *env, chains []*gateChain, wait time.Duration) []map[string]gateOutcome {
	outs := make([]map[string]gateOutcome, len(chains))
	resolved := make([]*chain.Chain, len(chains))
	var mu sync.Mutex
	var beside sync.WaitGroup
	started := time.Now()
	for i, g := range chains {
		resolved[i], _ = e.resolveChain(g.name)
		if g.waits, g.queued = gateWaits(e, resolved[i]); g.waits == 0 {
			continue
		}
		if g.queued != "" {
			fmt.Fprintf(os.Stderr, "gate: %s waits %s by design (its wait: steps); it runs in turn (%s), so this gate takes at least that long\n", g.name, g.waits, g.queued)
			continue
		}
		fmt.Fprintf(os.Stderr, "gate: %s waits %s by design (its wait: steps); it starts now, beside the other chains, so this gate takes at least that long\n", g.name, g.waits)
		beside.Add(1)
		go func() {
			defer beside.Done()
			out := g.send(ctx, resolved[i], wait)
			mu.Lock()
			outs[i] = out
			mu.Unlock()
		}()
	}
	for i, g := range chains {
		if !g.beside() {
			outs[i] = g.send(ctx, resolved[i], wait)
		}
	}
	mu.Lock()
	for i, g := range chains {
		if g.beside() && outs[i] == nil {
			left := ""
			if d := g.waits - time.Since(started); d > 0 {
				left = fmt.Sprintf(", about %s left", d.Round(time.Second))
			}
			fmt.Fprintf(os.Stderr, "gate: the other chains are done; waiting for %s, which waits %s by design%s\n", g.name, g.waits, left)
		}
	}
	mu.Unlock()
	beside.Wait()
	return outs
}

func (g *gateChain) beside() bool {
	return g.waits > 0 && g.queued == ""
}

func gateWaits(e *env, c *chain.Chain) (time.Duration, string) {
	if c == nil {
		return 0, ""
	}
	var waits time.Duration
	queued, logins := "", loginRPCs(e)
	for _, s := range c.Steps {
		d, _ := s.WaitFor()
		waits += d
		m, err := e.cat.Lookup(s.Call)
		switch {
		case queued != "" || err == nil && logins[m.FullName]:
		case !chain.IsReadOnlyCall(s.Call):
			queued = s.ID + " writes"
		case !slices.Contains(s.SendReferences(), "vars."+chain.RunTagVar):
			queued = s.ID + " reads without ${vars." + chain.RunTagVar + "}"
		}
	}
	return waits, queued
}

func (g *gateChain) send(ctx context.Context, c *chain.Chain, wait time.Duration) map[string]gateOutcome {
	began := time.Now()
	defer func() { g.took = time.Since(began) }()
	readsTag, keptRed := false, false
	if c != nil {
		readsTag = len(c.UnusedVarNames(map[string]any{chain.RunTagVar: ""})) == 0
		keptRed = len(c.KeptRed) > 0
	}
	outs := map[string]gateOutcome{}
	if g.spot {
		outs["verify"] = gateAttempt(ctx, "verify", g.name, readsTag, wait)
	}
	if v, ok := outs["verify"]; g.file != "" && (!ok || keptRed || v.code == 3 || v.side.RunToo) {
		outs["run"] = gateAttempt(ctx, "run", g.name, readsTag, wait)
	}
	return outs
}

func gateTime(chains []*gateChain, total time.Duration) string {
	var slow []*gateChain
	for _, g := range chains {
		if g.waits > 0 || total >= 30*time.Second && 4*g.took >= total {
			slow = append(slow, g)
		}
	}
	if len(slow) == 0 {
		return ""
	}
	sort.SliceStable(slow, func(i, j int) bool { return slow[i].took > slow[j].took })
	var parts []string
	for _, g := range slow[:min(3, len(slow))] {
		part := g.name + " " + g.took.Round(time.Second).String()
		switch {
		case g.beside():
			part += fmt.Sprintf(" (waits %s by design, beside the other chains)", g.waits)
		case g.waits > 0:
			part += fmt.Sprintf(" (waits %s by design, in turn: %s)", g.waits, g.queued)
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("time: %s; slowest: %s", total.Round(time.Second), strings.Join(parts, ", "))
}

func gateAttempt(ctx context.Context, what, name string, readsTag bool, wait time.Duration) gateOutcome {
	var out gateOutcome
	for try := 1; try <= 2; try++ {
		args := []string{what, name, "-quiet"}
		if what == "run" {
			args = append(args, "-keep-going")
		}
		if readsTag {
			args = append(args, "-var", chain.RunTagVar+"="+chain.NewRunTag())
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

func (g *gateChain) absorb(what string, out gateOutcome) {
	for _, f := range out.side.Flaky {
		if g.flaky == nil {
			g.flaky = map[string]gateFlaky{}
		}
		g.flaky[f.Call] = f
	}
	if g.errors == nil {
		g.errors, g.errored, g.slow = map[string]gateFlaky{}, map[string]string{}, map[string]bool{}
	}
	for _, f := range append(append([]gateFlaky{}, out.side.Flaky...), out.side.Errors...) {
		for _, s := range f.Steps {
			g.errored[what+" "+s] = f.Call
		}
	}
	for _, f := range out.side.Errors {
		g.errors[f.Call] = f
	}
	for _, n := range out.side.Notes {
		if !slices.Contains(g.notes, n) {
			g.notes = append(g.notes, n)
		}
	}
	for _, f := range out.side.Latency {
		if !g.slow[f.Step] {
			g.slow[f.Step] = true
			g.notes = append(g.notes, f.Line())
		}
	}
	why := strings.TrimPrefix(out.side.Error, "chain "+g.name+": ")
	switch {
	case out.code == 0:
		if what == "run" && out.side.KeptRed != "" {
			g.keptRed = out.side.KeptRed
		}
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
	case out.side.FlakyOnly:
		g.failed, g.flakyOnly = true, true
	default:
		if !g.failed || !g.otherFail {
			g.first, g.sent = "", nil
		}
		g.failed, g.otherFail = true, true
		if what == "run" && out.side.KeptRed != "" {
			g.keptRed = out.side.KeptRed
		}
		if g.first == "" && (len(out.side.Items) == 0 || out.side.KeptRed == runner.KeptRedGone) {
			g.first = capText(what+": "+strings.TrimPrefix(why, "kept red, but it did not fail as pinned: "), 200)
		}
		if what == "run" && out.side.KeptRed == runner.KeptRedNotAsPinned {
			g.class, g.pinsHeld = "not as pinned", out.side.PinsHeld
		}
		for _, it := range out.side.Items {
			it.from = what
			g.items = append(g.items, it)
		}
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
		}
		if what == "verify" {
			g.shown = changeLines(out.stdout)
		}
	}
}

func (g *gateChain) intermittentFirst() string {
	it, ok := g.firstItem()
	if !ok {
		return ""
	}
	if f, ok := g.flaky[it.Call]; ok && slices.Contains(f.Steps, it.Step) {
		return g.flakyKindOf(it.Call)
	}
	return ""
}

func (g *gateChain) flakyLine() string {
	parts := []string{}
	for _, call := range g.flakyCalls() {
		f := g.flaky[call]
		parts = append(parts, fmt.Sprintf("%s %s", shortRPC(call), f.text()))
	}
	return g.flakyKindOf(g.flakyCalls()[0]) + ": " + strings.Join(parts, "; ")
}

func (g *gateChain) flakyNote() string {
	if len(g.flaky) == 0 || g.findingOnly() {
		return ""
	}
	rpcs := []string{}
	for _, call := range g.flakyCalls() {
		rpcs = append(rpcs, shortRPC(call))
	}
	return fmt.Sprintf("FINDING: %s failure at %s, below", g.flakyKindOf(g.flakyCalls()[0]), strings.Join(rpcs, ", "))
}

func (g *gateChain) firstItem() (gateItem, bool) {
	for _, it := range g.items {
		if it.Step+" "+it.Path == g.firstAt {
			return it, true
		}
	}
	return gateItem{}, false
}

func (g *gateChain) printChanges(e *env) {
	if it, ok := g.firstItem(); ok {
		if req := requestLine(it.Reason, it.Step, func(s string) string { return g.sent[s] }); req != "" {
			fmt.Println("  " + req)
		}
		if hint := tellApart(e, it.Reason, it.Path); hint != "" {
			fmt.Println("  " + hint)
		}
	}
	type row struct {
		key, line string
		steps, at []string
		more      int
	}
	var rows []*row
	byKey, seen := map[string]*row{}, map[string]bool{}
	add := func(key, step, id, line string) {
		if seen[step+" "+id] {
			return
		}
		seen[step+" "+id] = true
		r := byKey[key]
		if r == nil {
			r = &row{key: key, line: line}
			byKey[key], rows = r, append(rows, r)
		} else if r.more++; !slices.Contains(r.at, step) {
			r.at = append(r.at, step)
		}
		if !slices.Contains(r.steps, step) {
			r.steps = append(r.steps, step)
		}
	}
	for _, l := range g.shown {
		key, step := l, ""
		if m := verifyChangeLine.FindStringSubmatch(l); m != nil {
			key, step = m[2]+" "+gateIndex.ReplaceAllString(m[3], "[]$1"), m[1]
		}
		add(key, step, l, l)
	}
	for _, it := range g.items {
		path, _ := it.shown()
		switch {
		case len(g.shown) > 0:
		case it.Reason.Kind == reasonKnockOn:
			add("\x00"+it.Reason.String(), it.Step, "", it.Reason.String())
		default:
			line := "[" + it.Step + "] " + it.headline()
			add(path, it.Step, line, line)
		}
	}
	for _, r := range rows {
		switch {
		case strings.HasPrefix(r.key, "\x00"):
			fmt.Printf("    %d step(s) %s (%s)\n", len(r.steps), r.line, capList(r.steps, 3))
		case r.more > 0:
			fmt.Printf("    %s (and %d more at %s)\n", r.line, r.more, capList(r.at, 3))
		default:
			fmt.Println("    " + r.line)
		}
	}
}

var verifyChangeLine = regexp.MustCompile(`^\[([^\]]+)\] +(\S+) +(\S+)`)

func changeLines(stdout string) []string {
	var out []string
	for _, l := range strings.Split(stdout, "\n") {
		if rest, ok := strings.CutPrefix(l, "  ["); ok {
			out = append(out, "["+rest)
		}
	}
	return out
}

func (g *gateChain) line(width int) string {
	verdict := "PASS"
	switch {
	case g.findingOnly():
		return strings.TrimRight(fmt.Sprintf("%-10s %-*s  %s", "FINDING", width, g.name, g.flakyLine()), " ")
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
		case g.failed && g.intermittentFirst() != "":
			line += g.intermittentFirst() + ": "
		case g.class != "" && g.failed:
			line += g.class + ": "
		}
		line += g.first
	}
	if len(g.slices) > 0 {
		line += fmt.Sprintf(" (+%d slice(s) fail the same: %s)", len(g.slices), strings.Join(g.slices, ", "))
	}
	return strings.TrimRight(line, " ")
}

var gateIndex = regexp.MustCompile(`\.\d+(\.|$)`)

func settleGate(chains []*gateChain) []string {
	foldFlaky(chains)
	seen, keyOf, byName := map[string]string{}, groupKeys(chains), map[string]*gateChain{}
	for _, g := range chains {
		byName[g.name] = g
	}
	lacks := func(x string, g *gateChain, lead gateItem) (gateItem, bool) {
		has := map[string]bool{keyOf(g.name, lead): true}
		if byName[x] != nil && x != g.name {
			has = map[string]bool{}
			for _, it := range byName[x].items {
				has[keyOf(x, it)] = true
			}
		}
		for _, it := range g.items {
			if !it.Passes && it.Reason.Kind != "" && !has[keyOf(g.name, it)] {
				return it, true
			}
		}
		return gateItem{}, false
	}
	for _, g := range chains {
		if !g.failed || g.findingOnly() || len(g.items) == 0 {
			continue
		}
		for i := range g.items {
			r := &g.items[i].Reason
			for _, o := range r.Or {
				if seen[g.items[i].root()] == "" && seen[shortRPC(o.RPC)+" "+leafOf(g.items[i].Path)] != "" {
					r.Step, r.RPC, r.Profile = o.Step, o.RPC, o.Profile
				}
			}
		}
		lead, rank, verified := -1, -1, false
		for i, it := range g.items {
			r := 0
			if it.Pinned != "" {
				r += 32
			}
			if fl, ok := g.flaky[it.Call]; !ok || !slices.Contains(fl.Steps, it.Step) {
				r += 16
			}
			if it.from == "verify" && !verified {
				r, verified = r+8, true
			}
			if it.Failed {
				r += 4
			}
			if it.Reason.Kind != "" {
				r += 2
			}
			if r > rank {
				lead, rank = i, r
			}
		}
		it := g.items[lead]
		if it.Pinned != "" && g.keptRed == runner.KeptRedGone {
			g.items = nil
			continue
		}
		g.first, g.firstAt = fmt.Sprintf("%s%s %s", it.Step, it.callNote(), it.headline()), it.Step+" "+it.Path
		other, more := lacks(seen[it.root()], g, it)
		switch first := seen[it.root()]; {
		case first != "" && first != g.name && !more:
			named := methodName(it.rpc())
			if r := it.Reason; r.Kind == reasonUnclear && len(r.Or) == 0 && r.ReadRPC != "" {
				named += " or " + methodName(r.ReadRPC)
			}
			g.first += "; " + sameFault + first + " (" + named + ")"
		case it.Reason.Kind != "":
			g.first += "; " + it.Reason.in(said{row: true, head: &it})
		}
		if more {
			g.first += "; also " + other.Reason.in(said{row: true})
		}
		if seen[it.root()] == "" && it.Reason.Kind != "" && len(it.Reason.Or) == 0 {
			seen[it.root()] = g.name
		}
		switch {
		case it.Pinned != "":
			g.class = "not as pinned"
		case g.pinsHeld:
			g.class = "pins held, new change"
		case it.Class != "" && g.class != "not as pinned":
			g.class = it.Class
		}
	}
	foldSlices(chains)
	return settleFlaky(chains)
}

func foldSlices(chains []*gateChain) {
	byName := map[string]*gateChain{}
	for _, g := range chains {
		byName[g.name] = g
	}
	for _, g := range chains {
		i := strings.LastIndex(g.name, "-slice-")
		if i < 0 || !g.failed || g.findingOnly() || g.class == "not as pinned" {
			continue
		}
		p := byName[g.name[:i]]
		if p == nil || !p.failed || p.findingOnly() || p.echoOf != "" || g.pinsHeld && !p.failsAsAll(g) {
			continue
		}
		a, okA := g.firstItem()
		b, okB := p.firstItem()
		if okA && okB && a.Call == b.Call && a.Path == b.Path {
			g.echoOf, p.slices = p.name, append(p.slices, g.name)
		}
	}
}

func (g *gateChain) failsAsAll(slice *gateChain) bool {
	roots := map[string]bool{}
	for _, it := range g.items {
		roots[it.root()] = true
	}
	return !slices.ContainsFunc(slice.items, func(it gateItem) bool { return !it.Passes && !roots[it.root()] })
}

func groupKeys(chains []*gateChain) func(string, gateItem) string {
	keys := map[string]string{}
	keyOf := func(it gateItem, path string) string {
		if chain.IsEnvelopePath(path) {
			path = chain.EnvelopeField()
		}
		return it.rpc() + " " + listOf(path)
	}
	for _, g := range chains {
		for _, it := range g.items {
			at, path := it.Step, it.Path
			if s := it.suspect(); s != "" {
				at, path = s, it.Reason.Path
			}
			if !it.Passes && path != "" && keys[g.name+" "+at] == "" {
				keys[g.name+" "+at] = keyOf(it, path)
			}
		}
	}
	return func(name string, it gateItem) string {
		return cmp.Or(keys[name+" "+cmp.Or(it.suspect(), it.Step)], keyOf(it, it.Path))
	}
}

func unclearLabel(chains []*gateChain) func(gateItem) string {
	decisive := map[string]bool{}
	for _, g := range chains {
		for _, it := range g.items {
			if k := it.Reason.Kind; !it.Passes && (k != "" || !chain.IsReadOnlyCall(it.Call)) && k != reasonUnclear && k != reasonKnockOn {
				decisive[it.rpc()+" "+leafOf(it.Path)] = true
			}
		}
	}
	return func(it gateItem) string {
		r := it.Reason
		if r.Kind != reasonUnclear {
			return it.rpc()
		}
		var names []string
		add := func(call string) {
			if n := shortRPC(call); call != "" && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		if len(r.Or) > 0 {
			for _, o := range r.Or {
				add(o.RPC)
			}
		} else {
			add(r.RPC)
			add(r.ReadRPC)
		}
		for _, n := range names {
			if decisive[n+" "+leafOf(it.Path)] {
				return n
			}
		}
		switch {
		case len(names) < 2:
			return it.rpc()
		case len(r.Or) == 0:
			return names[0] + " or the read " + names[1]
		}
		return strings.Join(names, " or ")
	}
}

func printGateGroups(chains []*gateChain, verbose bool) {
	type group struct {
		rpc, path            string
		steps, knock, chains map[string]bool
		example              gateItem
		in                   string
		rank                 int
	}
	groups, order, keyOf, label := map[string]*group{}, []*group{}, groupKeys(chains), unclearLabel(chains)
	for _, g := range chains {
		for _, it := range g.items {
			if it.Passes {
				continue
			}
			key, rpc := keyOf(g.name, it), it.rpc()
			if l := label(it); l != rpc && strings.HasPrefix(key, rpc+" ") {
				key, rpc = l+strings.TrimPrefix(key, rpc), l
			}
			gr := groups[key]
			if gr == nil {
				gr = &group{rpc: rpc, path: strings.TrimPrefix(key, rpc+" "), steps: map[string]bool{}, knock: map[string]bool{}, chains: map[string]bool{}, rank: -1}
				groups[key] = gr
				order = append(order, gr)
			}
			if it.Reason.Kind == reasonKnockOn {
				gr.knock[g.name+" "+it.Step] = true
			} else {
				gr.steps[g.name+" "+it.Step] = true
			}
			gr.chains[g.name] = true
			rank := 0
			switch {
			case it.Reason.Kind == "" || it.Reason.Kind == reasonKnockOn:
			case len(it.Reason.Or) > 0:
				rank += 4
			case it.Reason.Kind == reasonWrite:
				rank += 8
			default:
				rank += 16
			}
			if it.Failed {
				rank += 2
			}
			if it.Pinned == "" {
				rank++
			}
			if rank > gr.rank {
				gr.example, gr.in, gr.rank = it, g.name, rank
			}
		}
	}
	if len(order) == 0 {
		return
	}
	sort.SliceStable(order, func(i, j int) bool {
		if len(order[i].chains) != len(order[j].chains) {
			return len(order[i].chains) > len(order[j].chains)
		}
		return len(order[i].steps) > len(order[j].steps)
	})
	perRPC := map[string]int{}
	for _, gr := range order {
		perRPC[gr.rpc]++
	}
	fmt.Println("failures by suspect rpc:")
	for _, gr := range order {
		n, it := len(gr.steps), gr.example
		if n == 0 {
			n = len(gr.knock)
		}
		head := gr.rpc
		if perRPC[gr.rpc] > 1 && gr.path != "" {
			head += " " + gr.path
		}
		line := fmt.Sprintf("  %s: %d step(s) in %d chain(s); e.g. %s %s", head, n, len(gr.chains), gr.in, it.Step)
		if r := it.Reason.in(said{step: it.Step, rpc: gr.rpc}); r != "" {
			line += "; " + r
		} else if it.Reason.Kind == "" {
			path, eg := it.shown()
			line += " " + path + " " + eg
		}
		if verbose && len(gr.steps) > 0 && len(gr.knock) > 0 {
			line += fmt.Sprintf(" (+%d knock-on step(s))", len(gr.knock))
		}
		fmt.Println(line)
	}
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

func methodName(call string) string {
	return call[strings.LastIndex(call, "/")+1:]
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
			return "hollow ratchet: " + gateError(out)
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
	return "hollow ratchet: " + gateError(out)
}

func gateError(out gateOutcome) string {
	for _, line := range strings.Split(out.stderr, "\n") {
		if rest, ok := strings.CutPrefix(line, "shrt chain: hollow: "); ok {
			return rest
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out.stderr+"\n"+out.stdout), "\n")
	return line
}
