package hollow

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

var ErrNoRecords = errors.New("no run records were read")

const (
	StatusReported    = "reported"
	StatusAllowlisted = "allowlisted"
	StatusChainFixed  = "chain-asserts-data"
)

type Finding struct {
	Chain       string `json:"chain"`
	Step        string `json:"step"`
	RPC         string `json:"rpc"`
	Procedure   string `json:"procedure"`
	RunID       string `json:"run_id"`
	Occurrences int    `json:"occurrences"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

type Report struct {
	RunsDir        string `json:"runs_dir"`
	Records        int    `json:"records"`
	ReplayRecords  int    `json:"replay_records"`
	KeptRedRecords int    `json:"kept_red_records"`
	FailedRecords  int    `json:"failed_records"`

	FailedKeptRedRecords int       `json:"failed_kept_red_records"`
	ReadSteps            int       `json:"read_steps"`
	EnvelopeOnly         int       `json:"envelope_only"`
	AssertsNothing       int       `json:"asserts_nothing"`
	HollowRecords        int       `json:"hollow_step_records"`
	DistinctSteps        int       `json:"distinct_steps"`
	ChainFixed           int       `json:"chain_asserts_data"`
	Allowed              int       `json:"allowlisted"`
	Unallowed            int       `json:"reported"`
	Findings             []Finding `json:"findings"`
	Orphans              []string  `json:"orphan_run_dirs,omitempty"`
	OrphanRecords        int       `json:"orphan_records,omitempty"`
	Scratch              []string  `json:"scratch_run_dirs,omitempty"`
	ScratchRecords       int       `json:"scratch_records,omitempty"`
	Edited               []string  `json:"edited_records,omitempty"`
	Unsealed             int       `json:"unsealed_records,omitempty"`
}

func IsReadProcedure(procedure string) bool {
	return chain.IsReadOnlyCall(procedure)
}

func IsEnvelopePath(path string) bool { return chain.IsEnvelopePath(path) }

func IsMetadataAssertion(path, rule string) bool {
	if IsEnvelopePath(path) || chain.IsTransportPath(path) || rule == "unevaluated" {
		return true
	}
	if head, _, _ := strings.Cut(path, "."); !chain.IsPagingFieldName(head) {
		return false
	}
	return rule == "" || rule == "not_empty" || rule == "exists"
}

func assertsOnlyEnvelope(rec *runner.StepRecord) bool {
	for _, e := range rec.Expect {
		if AssertsAbsence(e) || IsVacuousResult(e) {
			continue
		}
		if !IsMetadataAssertion(e.Path, e.Rule) || pinsEnvelopeDetail(e) {
			return false
		}
	}
	return true
}

func pinsEnvelopeDetail(e chain.ExpectResult) bool {
	if e.Rule != "equals" || fmt.Sprint(e.Want) == "" || e.Want == nil || !IsEnvelopePath(e.Path) {
		return false
	}
	segs := chain.SplitPath(e.Path)
	if strings.Join(segs, ".") == chain.EnvelopePath() {
		return false
	}
	return slices.Contains(chain.CodeFields(), segs[len(segs)-1])
}

func AssertsAbsenceExpectation(e chain.Expectation) bool {
	return e.Exists != nil && !discriminatesEmptiness(e.Path, *e.Exists)
}

func AssertsAbsence(e chain.ExpectResult) bool {
	if e.Rule != "exists" {
		return false
	}
	want, ok := e.Want.(bool)
	return !ok || !discriminatesEmptiness(e.Path, want)
}

func discriminatesEmptiness(path string, want bool) bool {
	segs := strings.Split(path, ".")
	if !want {
		return chain.IsDigits(segs[len(segs)-1])
	}
	return slices.ContainsFunc(segs, chain.IsDigits)
}

func DeclaresRefusal(expect []chain.ExpectResult) bool {
	for _, e := range expect {
		if e.Passed && pinsRefusal(e.Path, e.Rule, e.Want) {
			return true
		}
	}
	return false
}

func declaresRefusalExpectation(e chain.Expectation) bool {
	switch {
	case e.Equals != nil:
		return pinsRefusal(e.Path, "equals", e.Equals)
	case e.NotEqual != nil:
		return pinsRefusal(e.Path, "not_equal", e.NotEqual)
	}
	return false
}

func pinsRefusal(path, rule string, want any) bool {
	if strings.Join(chain.SplitPath(path), ".") != chain.EnvelopePath() || want == nil {
		return false
	}
	isOK := fmt.Sprint(want) == chain.EnvelopeOK()
	return rule == "equals" && !isOK || rule == "not_equal" && isOK
}

func DataAsserted(chains []*chain.Chain) map[string]bool {
	out := map[string]bool{}
	for _, c := range chains {
		for _, s := range c.Steps {
			for _, e := range s.Expect {
				e, envelopeRef := bindVars(e, c.Vars)
				if envelopeRef {
					continue
				}
				if declaresRefusalExpectation(e) || chain.TautologyReason(e) == "" && !AssertsAbsenceExpectation(e) &&
					!isVacuousExpectation(e) && !IsMetadataAssertion(e.Path, expectationRule(e)) {
					out[stepKey(c.Name, s.ID)] = true
					break
				}
			}
		}
	}
	return out
}

var varRef = regexp.MustCompile(`^\$\{\s*vars\.([A-Za-z0-9_-]+)\s*\}$`)

func bindVars(e chain.Expectation, vars map[string]any) (chain.Expectation, bool) {
	bind := func(v any) (any, bool) {
		text, ok := v.(string)
		if !ok || !strings.Contains(text, "${") {
			return v, false
		}
		if m := varRef.FindStringSubmatch(text); m != nil {
			if bound, ok := vars[m[1]]; ok {
				return bound, false
			}
		}
		return v, true
	}
	var equals, notEqual bool
	e.Equals, equals = bind(e.Equals)
	e.NotEqual, notEqual = bind(e.NotEqual)
	return e, (equals || notEqual) && strings.Join(chain.SplitPath(e.Path), ".") == chain.EnvelopePath()
}

func BodyIsEmpty(response json.RawMessage) bool {
	var fields map[string]any
	return len(response) == 0 || json.Unmarshal(response, &fields) == nil && mapIsEmpty(fields)
}

func mapIsEmpty(m map[string]any) bool {
	for key, v := range m {
		if chain.IsMetadataField(key) {
			continue
		}
		if !anyIsEmpty(v) {
			return false
		}
	}
	return true
}

func anyIsEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case float64:
		return t == 0
	case string:
		if t == "" {
			return true
		}
		n, err := strconv.ParseFloat(t, 64)
		return err == nil && n == 0
	case []any:
		for _, item := range t {
			m, isMessage := item.(map[string]any)
			if item != nil && (!isMessage || !mapIsEmpty(m)) {
				return false
			}
		}
		return true
	case map[string]any:
		return mapIsEmpty(t)
	}
	return false
}

func stepKey(chainName, step string) string { return chainName + "\x00" + step }

func Scan(runsDir string, allow *Allowlist, dataAsserted map[string]bool) (*Report, error) {
	return ScanKnown(runsDir, allow, dataAsserted, nil)
}

func ScanKnown(runsDir string, allow *Allowlist, dataAsserted map[string]bool, known map[string]bool) (*Report, error) {
	return ScanKnownScratch(runsDir, allow, dataAsserted, known, nil)
}

func ScanKnownScratch(runsDir string, allow *Allowlist, dataAsserted map[string]bool, known map[string]bool, scratch func(chainSource string) bool) (*Report, error) {
	rep := &Report{RunsDir: runsDir, Findings: []Finding{}}
	files, err := recordFiles(runsDir)
	if err != nil {
		return nil, err
	}
	if known != nil {
		kept := files[:0]
		orphaned := map[string]int{}
		scratchDirs := map[string]bool{}
		for _, path := range files {
			dir := filepath.Base(filepath.Dir(path))
			if known[strings.ToLower(dir)] {
				kept = append(kept, path)
				continue
			}
			orphaned[dir]++
			if scratch != nil && !scratchDirs[dir] && scratch(recordSource(path)) {
				scratchDirs[dir] = true
			}
		}
		files = kept
		for dir, n := range orphaned {
			if scratchDirs[dir] {
				rep.Scratch = append(rep.Scratch, dir)
				rep.ScratchRecords += n
				continue
			}
			rep.Orphans = append(rep.Orphans, dir)
			rep.OrphanRecords += n
		}
		sort.Strings(rep.Orphans)
		sort.Strings(rep.Scratch)
	}
	seen := map[string]*Finding{}
	order := []string{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rec := &runner.Record{}
		if err := rec.UnmarshalJSON(raw); err != nil {
			continue
		}
		switch err := store.SealState(rec); {
		case errors.Is(err, store.ErrRunEdited):
			rep.Edited = append(rep.Edited, path)
			continue
		case errors.Is(err, store.ErrRunUnsealed):
			rep.Unsealed++
		}
		rep.Records++
		if rec.ReplayOf != "" {
			rep.ReplayRecords++
		}
		if rec.KeptRed != "" {
			rep.KeptRedRecords++
		}
		if rec.Status != runner.StatusPassed {
			rep.FailedRecords++
			if rec.KeptRed != "" {
				rep.FailedKeptRedRecords++
			}
		}
		for _, step := range rec.Steps {
			if step == nil || step.Status != "passed" || step.Transport != nil {
				continue
			}
			procedure := cmp.Or(step.Procedure, step.Call)
			if !IsReadProcedure(procedure) {
				continue
			}
			rep.ReadSteps++
			if !assertsOnlyEnvelope(step) {
				continue
			}
			if len(step.Expect) == 0 {
				rep.AssertsNothing++
			} else {
				rep.EnvelopeOnly++
			}
			if DeclaresRefusal(step.Expect) || !BodyIsEmpty(step.Response) {
				continue
			}
			rep.HollowRecords++
			k := stepKey(rec.Chain, step.ID)
			f, ok := seen[k]
			if !ok {
				f = &Finding{Chain: rec.Chain, Step: step.ID, RPC: procedure[strings.LastIndex(procedure, "/")+1:], Procedure: procedure}
				seen[k] = f
				order = append(order, k)
			}
			f.Occurrences++
			if rec.RunID > f.RunID {
				f.RunID = rec.RunID
			}
		}
	}
	if rep.Records == 0 {
		return rep, ErrNoRecords
	}
	sort.Strings(order)
	rep.DistinctSteps = len(order)
	for _, k := range order {
		f := seen[k]
		reason, allowed := allow.Reason(f.Chain, f.Step)
		switch {
		case dataAsserted[k]:
			f.Status = StatusChainFixed
			rep.ChainFixed++
		case allowed:
			f.Status, f.Reason = StatusAllowlisted, reason
			rep.Allowed++
		default:
			f.Status = StatusReported
			rep.Unallowed++
		}
		rep.Findings = append(rep.Findings, *f)
	}
	return rep, nil
}

func recordFiles(runsDir string) ([]string, error) {
	entries, err := os.ReadDir(runsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		inner, err := os.ReadDir(filepath.Join(runsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range inner {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			files = append(files, filepath.Join(runsDir, e.Name(), f.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func expectationRule(e chain.Expectation) string {
	switch {
	case e.Exists != nil:
		return "exists"
	case e.NotEmpty:
		return "not_empty"
	case e.Contains != "":
		return "contains"
	case e.NotEqual != nil:
		return "not_equal"
	case e.Equals != nil:
		return "equals"
	case e.Includes != nil:
		return "includes"
	}
	return ""
}

func IsVacuousResult(e chain.ExpectResult) bool {
	return e.Rule == "not_equal" && chain.VacuousNotEqualResult(e.Path, e.Want, e.Got)
}

func isVacuousExpectation(e chain.Expectation) bool {
	return e.NotEqual != nil && chain.VacuousNotEqual(e.Path, e.NotEqual)
}

func recordSource(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var head struct {
		ChainSource string `json:"chain_source"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return ""
	}
	return head.ChainSource
}
