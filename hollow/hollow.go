package hollow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/runner"
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
	RunsDir       string    `json:"runs_dir"`
	Records       int       `json:"records"`
	ReadSteps     int       `json:"read_steps"`
	EnvelopeOnly  int       `json:"envelope_only"`
	HollowRecords int       `json:"hollow_step_records"`
	DistinctSteps int       `json:"distinct_steps"`
	ChainFixed    int       `json:"chain_asserts_data"`
	Allowed       int       `json:"allowlisted"`
	Unallowed     int       `json:"reported"`
	Findings      []Finding `json:"findings"`
	Orphans       []string  `json:"orphan_run_dirs,omitempty"`
	OrphanRecords int       `json:"orphan_records,omitempty"`
}

func IsReadProcedure(procedure string) bool {
	return chain.IsReadOnlyCall(procedure)
}

func MethodName(procedure string) string {
	if i := strings.LastIndex(procedure, "/"); i >= 0 {
		return procedure[i+1:]
	}
	return procedure
}

func IsEnvelopePath(path string) bool { return chain.IsEnvelopePath(path) }

func IsMetadataAssertion(path, rule string) bool {
	if IsEnvelopePath(path) || chain.IsTransportPath(path) {
		return true
	}
	if rule == "unevaluated" {
		return true
	}
	if !chain.IsPagingFieldName(headSegment(path)) {
		return false
	}
	return rule == "" || rule == "not_empty" || rule == "exists"
}

func headSegment(path string) string {
	if i := strings.Index(path, "."); i >= 0 {
		return path[:i]
	}
	return path
}

func assertsOnlyEnvelope(rec *runner.StepRecord) bool {
	if len(rec.Expect) == 0 {
		return true
	}
	for _, e := range rec.Expect {
		if AssertsAbsence(e) || IsVacuousResult(e) {
			continue
		}
		if !IsMetadataAssertion(e.Path, e.Rule) {
			return false
		}
	}
	return true
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
	if !want {
		return false
	}
	for _, seg := range strings.Split(path, ".") {
		if isIndexSegment(seg) {
			return true
		}
	}
	return false
}

func isIndexSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func DataAsserted(chains []*chain.Chain) map[string]bool {
	out := map[string]bool{}
	for _, c := range chains {
		for _, s := range c.Steps {
			for _, e := range s.Expect {
				if chain.TautologyReason(e) != "" || AssertsAbsenceExpectation(e) || isVacuousExpectation(e) {
					continue
				}
				if !IsMetadataAssertion(e.Path, expectationRule(e)) {
					out[stepKey(c.Name, s.ID)] = true
					break
				}
			}
		}
	}
	return out
}

func BodyIsEmpty(response json.RawMessage) bool {
	if len(response) == 0 {
		return true
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response, &fields); err != nil {
		return false
	}
	for name, raw := range fields {
		if chain.IsMetadataField(name) {
			continue
		}
		if !valueIsEmpty(raw) {
			return false
		}
	}
	return true
}

func valueIsEmpty(raw json.RawMessage) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
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
		return len(t) == 0
	case map[string]any:
		return mapIsEmpty(t)
	}
	return false
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
		return len(t) == 0
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
	rep := &Report{RunsDir: runsDir, Findings: []Finding{}}
	files, err := recordFiles(runsDir)
	if err != nil {
		return nil, err
	}
	if known != nil {
		kept := files[:0]
		orphaned := map[string]int{}
		for _, path := range files {
			dir := filepath.Base(filepath.Dir(path))
			if known[strings.ToLower(dir)] {
				kept = append(kept, path)
				continue
			}
			orphaned[dir]++
			rep.OrphanRecords++
		}
		files = kept
		for dir := range orphaned {
			rep.Orphans = append(rep.Orphans, dir)
		}
		sort.Strings(rep.Orphans)
	}
	seen := map[string]*Finding{}
	order := []string{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rec := &runner.Record{}
		if err := json.Unmarshal(raw, rec); err != nil {
			continue
		}
		rep.Records++
		for _, step := range rec.Steps {
			if step == nil || step.Status != "passed" || step.Transport != nil {
				continue
			}
			procedure := step.Procedure
			if procedure == "" {
				procedure = step.Call
			}
			if !IsReadProcedure(procedure) {
				continue
			}
			rep.ReadSteps++
			if !assertsOnlyEnvelope(step) {
				continue
			}
			rep.EnvelopeOnly++
			if !BodyIsEmpty(step.Response) {
				continue
			}
			rep.HollowRecords++
			k := stepKey(rec.Chain, step.ID)
			f, ok := seen[k]
			if !ok {
				f = &Finding{
					Chain:     rec.Chain,
					Step:      step.ID,
					RPC:       MethodName(procedure),
					Procedure: procedure,
				}
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
		switch {
		case dataAsserted[k]:
			f.Status = StatusChainFixed
			rep.ChainFixed++
		default:
			if reason, allowed := allow.Reason(f.Chain, f.Step); allowed {
				f.Status = StatusAllowlisted
				f.Reason = reason
				rep.Allowed++
			} else {
				f.Status = StatusReported
				rep.Unallowed++
			}
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
	}
	return ""
}

func IsVacuousResult(e chain.ExpectResult) bool {
	return e.Rule == "not_equal" && chain.VacuousNotEqualResult(e.Path, e.Want, e.Got)
}

func isVacuousExpectation(e chain.Expectation) bool {
	return e.NotEqual != nil && chain.VacuousNotEqual(e.Path, e.NotEqual)
}
