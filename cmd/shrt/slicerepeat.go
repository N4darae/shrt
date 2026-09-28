package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
)

const sliceIntermittent = "intermittent"

type sliceRunOutcome struct {
	SliceRun string `json:"slice_run,omitempty"`
	Outcome  string `json:"outcome"`
	Record   string `json:"slice_record,omitempty"`
}

func repeatVars(vars varFlags, res *chain.SliceResult, n int) varFlags {
	if n <= 1 {
		return vars
	}
	fresh := freshSet(res)
	out := varFlags{}
	for k, v := range vars {
		out[k] = v
		if fresh[k] {
			out[k] = fmt.Sprintf("%v-r%d", v, n)
		}
	}
	return out
}

func runSliceVerifyRepeated(ctx context.Context, e *env, res *chain.SliceResult, rec *runner.Record, a sliceVerifyArgs, n int) (*sliceVerdict, error) {
	if n < 1 {
		n = 1
	}
	verdicts := []*sliceVerdict{}
	for i := 1; i <= n; i++ {
		ai := a
		ai.vars = repeatVars(a.vars, res, i)
		ai.quiet = a.quiet || n > 1
		v, err := runSliceVerify(ctx, e, res, rec, ai)
		if v == nil {
			if len(verdicts) == 0 {
				return nil, err
			}
			break
		}
		if n > 1 && !a.quiet && (a.verbose || v.Outcome != sliceReproduced) {
			fmt.Println(v.repeatLine(i, n))
		}
		verdicts = append(verdicts, v)
		if v.Outcome == sliceDidNotRun {
			break
		}
	}
	if n == 1 || len(verdicts) == 1 {
		v := verdicts[0]
		return v, v.err()
	}
	return combineSliceVerdicts(verdicts)
}

func combineSliceVerdicts(verdicts []*sliceVerdict) (*sliceVerdict, error) {
	reproduced := 0
	var rep *sliceVerdict
	rank := map[string]int{sliceNotReproduced: 3, sliceInconclusive: 2, sliceDidNotRun: 1}
	for _, v := range verdicts {
		if v.Outcome == sliceReproduced {
			reproduced++
			continue
		}
		if rep == nil || rank[v.Outcome] > rank[rep.Outcome] {
			rep = v
		}
	}
	if rep == nil {
		rep = verdicts[len(verdicts)-1]
	}
	out := *rep
	out.Repeat = len(verdicts)
	out.ReproducedRuns = reproduced
	out.Runs = make([]sliceRunOutcome, 0, len(verdicts))
	for _, v := range verdicts {
		out.Runs = append(out.Runs, sliceRunOutcome{SliceRun: v.SliceRun, Outcome: v.Outcome, Record: v.SliceRecord})
	}
	switch {
	case reproduced == len(verdicts):
		out.Outcome = sliceReproduced
		out.Reproduced = true
	case reproduced > 0:
		out.Outcome = sliceIntermittent
		out.Reproduced = false
		out.Next = ""
		out.Reason = fmt.Sprintf("step %s got the verdict of source run %s in %d of %d runs of the same slice: the backend answers it "+
			"differently to the same input, the differences above are from a run that did not reproduce it", out.Step, out.SourceRun, reproduced, len(verdicts))
	}
	return &out, out.err()
}

func (v *sliceVerdict) runsLabel() string {
	if v.Repeat <= 1 {
		return v.SliceRun
	}
	ids := []string{}
	for _, r := range v.Runs {
		if r.SliceRun != "" {
			ids = append(ids, r.SliceRun)
		}
	}
	return fmt.Sprintf("%s (%d of %d reproduced)", strings.Join(ids, ", "), v.ReproducedRuns, v.Repeat)
}

func (v *sliceVerdict) countLabel() string {
	if v.Repeat <= 1 {
		return ""
	}
	return fmt.Sprintf(" %d/%d", v.ReproducedRuns, v.Repeat)
}

func (v *sliceVerdict) repeatLine(i, n int) string {
	line := fmt.Sprintf("repeat %d of %d: %s", i, n, outcomeWord(v.Outcome))
	if v.SliceRun != "" {
		line += fmt.Sprintf(", slice run %s %s", v.SliceRun, v.Status)
	}
	if v.Outcome == sliceDidNotRun {
		why, _, _ := strings.Cut(v.Reason, "\n")
		line += ": " + capText(why, 200)
	}
	return line
}

func outcomeWord(outcome string) string {
	return map[string]string{
		sliceReproduced:    "reproduced",
		sliceNotReproduced: "NOT REPRODUCED",
		sliceInconclusive:  "INCONCLUSIVE",
		sliceDidNotRun:     "DID NOT RUN",
		sliceIntermittent:  "intermittent",
	}[outcome]
}
