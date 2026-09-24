package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
)

func init() {
	register(&command{
		name:    "verify",
		summary: "replay a chain and diff it against its safe spot",
		run:     runVerify,
	})
}

const verifyExitCodes = "\nexit codes:\n" +
	"  0  no drift against the safe spot, and the replay passed\n" +
	"  1  drift against the safe spot, the replay did not pass, or the chain has no safe spot\n" +
	"  3  could not verify: a step never got an answer (target unreachable, login failed) and\n" +
	"     nothing else drifted, so this is not a verdict about the backend\n"

func runVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	vars := varFlags{}
	fs.Var(vars, "var", "override a chain var, repeatable: -var key=value")
	useRun := fs.String("run", "", "diff a recorded run id instead of replaying")
	asJSON := fs.Bool("json", false, "emit the diff report as JSON")
	quiet := fs.Bool("quiet", false, "suppress per-step progress")
	save := fs.Bool("save", true, "persist the replay record")
	build := fs.String("build", "", buildFlagUsage)
	listMasked := fs.Bool("masked", false, "list every response value kept out of the comparison: under a volatile pattern, or id- or timestamp-shaped on both sides, with both values")
	setUsage(fs, "usage: shrt verify <chain> [flags]", verifyExitCodes)
	rest, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: shrt verify <chain> [flags]")
	}
	e, err := loadEnv(true)
	if err != nil {
		return err
	}
	name := rest[0]
	if err := e.knownChain(name); err != nil {
		return err
	}
	spot, err := e.store.LoadSafeSpot(name)
	if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("chain %s has no safe spot: nothing is confirmed at %s", name, e.store.SafeSpotPath(name))
	}
	if err != nil {
		if e.store.HasProposal(name) {
			return fmt.Errorf("%w\na proposal for %s awaits a person's decision: shrt confirm %s -approve -by <their email> once the user says yes, or -reject", err, name, name)
		}
		return fmt.Errorf("%w\nno safe spot yet — run the chain, check the responses, propose it with 'shrt confirm %s -note \"...\"', and a person approves it", err, name)
	}
	if !spot.DigestMatches() {
		return fmt.Errorf("safe spot %s was changed after it was approved: its digest %s does not match its content, so it is not what a person approved.\n"+
			"Restore the file (from version control or %s), or re-approve: run the chain, check the responses, "+
			"propose it with 'shrt confirm %s -supersede -note \"...\"', and a person approves it",
			rel(e.cfg.Root, e.store.SafeSpotPath(name)), spot.Digest, rel(e.cfg.Root, filepath.Join(e.store.SafeSpotsDir, "archive")), name)
	}

	var rec *runner.Record
	var c *chain.Chain
	if *useRun != "" {
		if *useRun == spot.RunID {
			fmt.Fprintf(os.Stderr,
				"verify: run %s IS the run this safe spot was made from, so this compares a recording with "+
					"itself. It is a clean control for the differ, and it is NOT evidence about the backend: "+
					"it reports no drift whatever the backend now does. Pass a LATER run id, or drop -run to "+
					"replay live.\n", *useRun)
		}
		rec, err = e.store.LoadRun(name, *useRun)
		if resolved, resolveErr := chain.Resolve(e.chainsDir(), name); resolveErr == nil {
			c = resolved
		}
	} else {
		c, err = chain.Resolve(e.chainsDir(), name)
		if err != nil {
			return err
		}
		rec, err = executeChain(ctx, e, c, runner.Options{Vars: c.CoerceVars(vars), Volatile: e.cfg.Volatile, Redact: e.cfg.Redact, Build: *build, KeepGoing: true}, *quiet || *asJSON)
		if err == nil && *save {
			if _, serr := e.store.SaveRun(rec); serr != nil {
				return serr
			}
		}
	}
	if err != nil {
		return err
	}

	report := diff.CompareMasking(spot, rec, currentVolatile(e, name))
	if c != nil {
		report.RequestChanges = append(diff.ChainChanges(spot, c), diff.CompareRequests(spot, rec, derivedRequestPath(c))...)
	}
	varDrift := ""
	if len(report.RequestChanges) > 0 {
		only := map[string]any(vars)
		if *useRun != "" {
			only = nil
		}
		varDrift = varsDifferFromConfirmed(e, spot.RunID, c, rec, only)
	}
	if varDrift != "" {
		how := "set by -var; the chain file is not what differs"
		if *useRun != "" {
			how = "as run " + rec.RunID + " was recorded"
		}
		report.InputCause = fmt.Sprintf("this run's vars differ from the confirmed run's (%s), %s", varDrift, how)
	}
	if *asJSON {
		if err := emitJSON(map[string]any{"run": rec, "diff": report}); err != nil {
			return err
		}
	} else {
		fmt.Println()
		if spot.Build != "" || rec.Build != "" {
			fmt.Printf("safe spot build %s, this run build %s\n", orUnknown(spot.Build), orUnknown(rec.Build))
		}
		fmt.Println(report.Text())
		if list := report.MaskedList(); *listMasked && list != "" {
			fmt.Println(list)
		}
		if report.Clean() && !report.Widened() && !report.PrincipalChanged() {
			fmt.Printf("covers the %d step(s) of this chain only; a regression in a path no safe spot exercises is not seen\n", len(spot.Steps))
		}
	}
	if step, why, ok := unansweredOnly(rec, report); ok {
		return exitWith(3, "could not verify %s: step %q never got an answer (%s), and nothing past it was compared. "+
			"This is not a verdict about the backend: start or reach the target and run verify again", name, step, why)
	}
	if !report.Clean() && varDrift != "" {
		fix := "Verify without that -var to compare like with like"
		if *useRun != "" {
			fix = "Verify a run made with the confirmed vars, or drop -run to replay the chain as it is"
		}
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %d request value(s) changed because this run's vars differ from the confirmed run's (%s).\n"+
			"%s; if the new value is intended, run the chain with it until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), len(report.RequestChanges), varDrift, fix, name)
	}
	if report.PrincipalChanged() {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, and a step ran under another auth profile than the confirmed run (%s).\n"+
			"Restore the step's auth; if the new principal is intended, run the chain until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), principalChanges(report), name)
	}
	if !report.Clean() && len(report.RequestChanges) > 0 {
		return fmt.Errorf("drift with different input: %d change(s) vs safe spot, after %d request value(s) changed since it was confirmed.\n"+
			"Restore the chain's input; if the new input is intended, bring its expectations in line, run it until it passes,\n"+
			"and propose that run in place of the safe spot: shrt confirm %s -supersede -note \"...\"",
			len(report.Changes), len(report.RequestChanges), name)
	}
	if !report.Clean() {
		return fmt.Errorf("regression: %d change(s) vs safe spot", len(report.Changes))
	}
	if report.Widened() {
		return fmt.Errorf("the replay was masked with volatile pattern(s) the safe spot did not approve: %s.\n"+
			"Remove them from the chain and config, or, if they are intended, run the chain and propose that run\n"+
			"in place of the safe spot so a person approves the wider mask: shrt confirm %s -supersede -note \"...\"",
			strings.Join(report.UnapprovedVolatile, ", "), name)
	}
	if !rec.Passed() {
		return fmt.Errorf("chain %s: %s", rec.Chain, rec.Status)
	}
	return nil
}

func varsDifferFromConfirmed(e *env, spotRun string, c *chain.Chain, rec *runner.Record, only map[string]any) string {
	var confirmed map[string]any
	if prev, err := e.store.LoadRun(rec.Chain, spotRun); err == nil {
		confirmed = prev.Vars
	} else if c != nil {
		redact := append(append([]string{}, e.cfg.Redact...), c.Redact...)
		confirmed, _ = pathmask.NewRedactor(redact).Apply(c.Vars).(map[string]any)
	} else {
		return ""
	}
	names := []string{}
	for k := range rec.Vars {
		names = append(names, k)
	}
	for k := range confirmed {
		if _, ok := rec.Vars[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	out := []string{}
	for _, k := range names {
		if _, supplied := only[k]; only != nil && !supplied {
			continue
		}
		now, had := rec.Vars[k], confirmed[k]
		_, inNow := rec.Vars[k]
		_, inBefore := confirmed[k]
		switch {
		case inNow && !inBefore:
			out = append(out, fmt.Sprintf("%s=%v, confirmed without it", k, now))
		case !inNow && inBefore:
			out = append(out, fmt.Sprintf("%s unset, confirmed with %v", k, had))
		case fmt.Sprint(now) != fmt.Sprint(had):
			out = append(out, fmt.Sprintf("%s=%v, confirmed with %v", k, now, had))
		}
	}
	return strings.Join(out, "; ")
}

func principalChanges(report *diff.Report) string {
	out := []string{}
	for _, c := range report.RequestChanges {
		if c.Path == diff.AuthProfilePath {
			out = append(out, fmt.Sprintf("%s: %v -> %v", c.Step, c.Want, c.Got))
		}
	}
	return strings.Join(out, ", ")
}

func orUnknown(s string) string {
	if s == "" {
		return "(not recorded)"
	}
	return s
}

var requestRef = regexp.MustCompile(`\$\{\s*([^}]*)\}`)

func derivedRequestPath(c *chain.Chain) func(step, path string) bool {
	return func(step, path string) bool {
		s, ok := c.Step(step)
		if !ok {
			return false
		}
		v, ok := chain.Get(s.Body, path)
		if !ok {
			return false
		}
		text, ok := v.(string)
		if !ok {
			return false
		}
		for _, m := range requestRef.FindAllStringSubmatch(text, -1) {
			head, _, _ := strings.Cut(strings.TrimSpace(m[1]), ".")
			if head != "vars" && head != "env" {
				return true
			}
		}
		return false
	}
}

func unansweredOnly(rec *runner.Record, report *diff.Report) (string, string, bool) {
	unanswered := map[string]string{}
	first := ""
	for _, st := range rec.Steps {
		if st.Status == runner.StatusError && st.HTTPStatus == 0 && len(st.Response) == 0 {
			unanswered[st.ID] = st.Error
			if first == "" {
				first = st.ID
			}
		}
	}
	if first == "" {
		return "", "", false
	}
	for _, c := range report.Changes {
		if c.Kind == diff.KindNotReached {
			continue
		}
		if _, skip := unanswered[c.Step]; skip {
			continue
		}
		return "", "", false
	}
	return first, unanswered[first], true
}
