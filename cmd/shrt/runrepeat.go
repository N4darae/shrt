package main

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const repeatPassed = "passed"

type repeatVerdict struct {
	Chain       string   `json:"chain"`
	Outcome     string   `json:"outcome"`
	Repeat      int      `json:"repeat"`
	Same        int      `json:"same_runs"`
	Failed      []string `json:"failed_steps,omitempty"`
	Runs        []string `json:"runs"`
	Records     string   `json:"records,omitempty"`
	Differences []string `json:"differences,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

func runRepeated(ctx context.Context, e *env, c *chain.Chain, n int, vars map[string]any, opts runner.Options, save, quiet, asJSON bool) error {
	v := &repeatVerdict{Chain: c.Name, Repeat: n, Outcome: sliceReproduced}
	var first *runner.Record
	for i := 1; i <= n; i++ {
		o := opts
		o.Vars = repeatRunVars(c, vars, i)
		rec, err := executeChain(ctx, e, c, o, true)
		if err != nil {
			return err
		}
		v.Runs = append(v.Runs, rec.RunID)
		if save {
			path, err := e.store.SaveRun(rec)
			if err != nil {
				return err
			}
			v.Records = shownPath(filepath.Dir(path))
		}
		if first == nil {
			first, v.Failed = rec, failedSteps(rec)
		}
		diffs := repeatDifferences(c, first, rec, fmt.Sprintf("run %d", i))
		if !quiet && !asJSON {
			fmt.Println(repeatLine(i, n, rec, diffs))
		}
		if why := noVerdictIn(rec); why != "" {
			v.Outcome, v.Reason = sliceDidNotRun, fmt.Sprintf("run %d (%s) %s", i, rec.RunID, why)
			break
		}
		if len(diffs) == 0 {
			v.Same++
		}
		for j, d := range diffs {
			if j == 5 {
				v.Differences = append(v.Differences, fmt.Sprintf("run %d: and %d more", i, len(diffs)-j))
				break
			}
			v.Differences = append(v.Differences, fmt.Sprintf("run %d (%s): %s", i, rec.RunID, d))
		}
	}
	switch {
	case v.Outcome == sliceDidNotRun:
	case len(v.Differences) > 0:
		v.Outcome = sliceNotReproduced
	case len(v.Failed) == 0:
		v.Outcome = repeatPassed
	}
	if asJSON {
		if err := emitJSON(v); err != nil {
			return err
		}
		return v.err()
	}
	fmt.Print(v.text(e, c, first))
	if err := v.err(); err != nil {
		return shownError{err}
	}
	return nil
}

func repeatRunVars(c *chain.Chain, supplied map[string]any, i int) map[string]any {
	vars := map[string]any{}
	for k, val := range supplied {
		vars[k] = val
	}
	if _, given := vars[chain.RunTagVar]; !given && len(c.UnusedVarNames(map[string]any{chain.RunTagVar: ""})) == 0 {
		vars[chain.RunTagVar] = chain.NewRunTag()
	}
	if i > 1 {
		for _, name := range chain.FreshVars(c.Steps, nil, nil) {
			if v, given := supplied[name]; given {
				vars[name] = fmt.Sprintf("%v-%s", v, chain.NewRunTag())
			}
		}
	}
	return vars
}

func repeatDifferences(c *chain.Chain, first, rec *runner.Record, label string) []string {
	if first == rec {
		return nil
	}
	was, now := failedSteps(first), failedSteps(rec)
	if !slices.Equal(was, now) {
		return []string{fmt.Sprintf("failed steps: source %s, %s %s", cmp.Or(chain.ListSome(was, 5), "none"), label, cmp.Or(chain.ListSome(now, 5), "none"))}
	}
	same := sameUpToFixtures(first.Vars, rec.Vars)
	out := []string{}
	for _, id := range was {
		a, _ := first.Step(id)
		b, _ := rec.Step(id)
		st, _ := c.Step(id)
		for _, d := range compareVerdictsAt(st, verdictOf(a), verdictOf(b), same, label) {
			out = append(out, id+": "+d)
		}
	}
	return out
}

func noVerdictIn(rec *runner.Record) string {
	for _, st := range rec.Steps {
		if st != nil && (unansweredCall(st) || rec.Status == runner.StatusError && st.Status == runner.StatusError) {
			why, _, _ := strings.Cut(st.Error, "\n")
			return fmt.Sprintf("got no answer at step %s (%s)", st.ID, why)
		}
	}
	return ""
}

func repeatLine(i, n int, rec *runner.Record, diffs []string) string {
	line := fmt.Sprintf("repeat %d of %d: run %s %s", i, n, rec.RunID, rec.Status)
	if failed := failedSteps(rec); len(failed) > 0 {
		line += " at " + chain.ListSome(failed, 3)
	}
	switch {
	case i == 1:
	case len(diffs) == 0:
		line += ", as run 1"
	default:
		line += ", not as run 1"
	}
	return line
}

func (v *repeatVerdict) text(e *env, c *chain.Chain, first *runner.Record) string {
	var b strings.Builder
	switch v.Outcome {
	case sliceReproduced:
		fmt.Fprintf(&b, "reproduced %d/%d: %s failed the same way in every run\n", v.Same, v.Repeat, chain.ListSome(v.Failed, 3))
		for _, id := range v.Failed {
			st, _ := first.Step(id)
			fmt.Fprintf(&b, "  %s (%s): %s\n", id, chain.RPCName(st.Call), answeredText(st))
			for _, x := range st.Expect {
				if !x.Passed && x.Rule != "unevaluated" {
					fmt.Fprintf(&b, "    failed: %s %s\n", x.Path, chain.WantGot(x.Rule, quoted(x.Want), quoted(x.Got)))
				}
			}
		}
		for _, line := range failureRequests(e, first, false) {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	case repeatPassed:
		fmt.Fprintf(&b, "passed %d/%d: no step failed in any run\n", v.Same, v.Repeat)
	case sliceNotReproduced:
		fmt.Fprintf(&b, "NOT REPRODUCED: %d of %d runs failed as run 1 did\n", v.Same, len(v.Runs))
		for _, d := range v.Differences {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	case sliceDidNotRun:
		fmt.Fprintf(&b, "DID NOT RUN: %s\n", v.Reason)
	}
	if line := neverRanLine(c, first); line != "" && v.Outcome != sliceDidNotRun {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	where := ""
	if v.Records != "" {
		where = " in " + v.Records
	}
	fmt.Fprintf(&b, "  runs %s%s\n", strings.Join(v.Runs, ", "), where)
	fmt.Fprintf(&b, "exit %d: %s\n", v.exitCode(), v.outcomeText())
	return b.String()
}

func answeredText(st *runner.StepRecord) string {
	verdict := verdictOf(st)
	answer := strings.TrimPrefix(refusalText(verdict), ", transport")
	if verdict.ErrorCode != "" {
		answer = fmt.Sprintf(" %s %q%s", verdictPath(st), verdict.ErrorCode, refusalText(verdict))
	}
	if answer == "" {
		why, _, _ := strings.Cut(st.Error, "\n")
		return "got no answer: " + why
	}
	return "answered" + answer
}

func (v *repeatVerdict) exitCode() int {
	switch v.Outcome {
	case repeatPassed, sliceNotReproduced:
		return 1
	case sliceDidNotRun:
		return 3
	}
	return 0
}

func (v *repeatVerdict) outcomeText() string {
	switch v.Outcome {
	case repeatPassed:
		return fmt.Sprintf("passed %d/%d, nothing reproduced", v.Same, v.Repeat)
	case sliceNotReproduced:
		return fmt.Sprintf("NOT REPRODUCED, %d of %d runs failed as run 1 did", v.Same, len(v.Runs))
	case sliceDidNotRun:
		return "DID NOT RUN"
	}
	return fmt.Sprintf("reproduced %d/%d", v.Same, v.Repeat)
}

func (v *repeatVerdict) err() error {
	what := v.outcomeText()
	if v.Reason != "" {
		what += ", " + v.Reason
	}
	if v.exitCode() == 0 {
		return nil
	}
	return exitWith(v.exitCode(), "chain %s: %s", v.Chain, what)
}
