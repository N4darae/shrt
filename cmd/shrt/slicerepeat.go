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
	SliceRun   string `json:"slice_run,omitempty"`
	Outcome    string `json:"outcome"`
	Record     string `json:"slice_record,omitempty"`
	NotCounted string `json:"not_counted,omitempty"`
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
		verdicts = append(verdicts, v)
		if v.Outcome == sliceDidNotRun && i == 1 {
			break
		}
	}
	out, err := verdicts[0], verdicts[0].err()
	if n > 1 && len(verdicts) > 1 {
		out, err = combineSliceVerdicts(verdicts)
	}
	for i, v := range verdicts {
		if n > 1 && !a.quiet && (a.verbose || v.Outcome != sliceReproduced) {
			fmt.Println(v.repeatLine(i+1, n, out))
		}
	}
	return out, err
}

func (v *sliceVerdict) notCountedWhy() string {
	switch {
	case v.brokeWhy != "":
		return v.brokeWhy
	case v.Outcome == sliceDidNotRun && v.Reason != "":
		why, _, _ := strings.Cut(v.Reason, "\n")
		return capText(why, 120)
	case v.Outcome == sliceDidNotRun:
		return "did not run"
	}
	return ""
}

func combineSliceVerdicts(verdicts []*sliceVerdict) (*sliceVerdict, error) {
	runs := make([]sliceRunOutcome, 0, len(verdicts))
	counted := []*sliceVerdict{}
	for _, v := range verdicts {
		why := v.notCountedWhy()
		runs = append(runs, sliceRunOutcome{SliceRun: v.SliceRun, Outcome: v.Outcome, Record: v.SliceRecord, NotCounted: why})
		if why == "" {
			counted = append(counted, v)
		}
	}
	if len(counted) == 0 {
		counted = verdicts
		for i := range runs {
			runs[i].NotCounted = ""
		}
	}
	reproduced := 0
	var rep *sliceVerdict
	rank := map[string]int{sliceNotReproduced: 3, sliceInconclusive: 2, sliceDidNotRun: 1}
	for _, v := range counted {
		if v.Outcome == sliceReproduced {
			reproduced++
			continue
		}
		if rep == nil || rank[v.Outcome] > rank[rep.Outcome] {
			rep = v
		}
	}
	if rep == nil {
		rep = counted[len(counted)-1]
	}
	out := *rep
	out.Repeat = len(verdicts)
	out.ReproducedRuns = reproduced
	out.Runs = runs
	switch {
	case reproduced == len(counted):
		out.Outcome = sliceReproduced
		out.Reproduced = true
	case reproduced > 0 && rep.Outcome == sliceNotReproduced:
		out.Outcome = sliceIntermittent
		out.Reproduced = false
		out.Next = ""
		out.Reason = fmt.Sprintf("step %s got the verdict of source run %s in %d of %d runs of the same slice: the backend answers it "+
			"differently to the same input, the differences above are from a run that did not reproduce it", out.Step, out.SourceRun, reproduced, len(counted))
	}
	return &out, out.err()
}

func (v *sliceVerdict) counted() int {
	n := 0
	for _, r := range v.Runs {
		if r.NotCounted == "" {
			n++
		}
	}
	return n
}

func (v *sliceVerdict) runsLabel() string {
	if v.Repeat <= 1 {
		return v.SliceRun
	}
	ids := []string{}
	for _, r := range v.Runs {
		if r.SliceRun != "" && r.NotCounted == "" {
			ids = append(ids, r.SliceRun)
		}
	}
	return fmt.Sprintf("%s (%d of %d reproduced)", strings.Join(ids, ", "), v.ReproducedRuns, v.counted())
}

func (v *sliceVerdict) countLabel() string {
	if v.Repeat <= 1 {
		return ""
	}
	out := fmt.Sprintf(" %d/%d", v.ReproducedRuns, v.counted())
	skipped := []string{}
	for i, r := range v.Runs {
		if r.NotCounted != "" {
			skipped = append(skipped, fmt.Sprintf("repeat %d not counted: %s", i+1, r.NotCounted))
		}
	}
	if len(skipped) > 0 {
		out += " (" + strings.Join(skipped, "; ") + ")"
	}
	return out
}

func (v *sliceVerdict) repeatLine(i, n int, combined *sliceVerdict) string {
	word := outcomeWord(v.Outcome)
	if i <= len(combined.Runs) && combined.Runs[i-1].NotCounted != "" {
		word = "not counted"
	}
	line := fmt.Sprintf("repeat %d of %d: %s", i, n, word)
	if v.SliceRun != "" {
		line += fmt.Sprintf(", slice run %s %s", v.SliceRun, v.Status)
	}
	if v.Outcome == sliceDidNotRun && word != "not counted" {
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

func keptFailureWhy(replay *runner.Record, broke []string) string {
	sr, _ := replay.Step(broke[0])
	kind := sr.Status
	switch code := verdictOf(sr).ErrorCode; {
	case sr.Transport != nil:
		kind = sr.Transport.Code
	case code != "" && code != chain.EnvelopeOK():
		kind = code
	default:
		for _, x := range sr.Expect {
			if !x.Passed {
				kind = x.Path
				break
			}
		}
	}
	out := fmt.Sprintf("kept step %s failed, %s", broke[0], kind)
	if len(broke) > 1 {
		out += ", and after it " + capList(broke[1:], 2)
	}
	return out
}
