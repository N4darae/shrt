package runner

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/N4darae/shrt/chain"
)

const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

const NoAuthProfile = "none"

type Record struct {
	RunID       string         `json:"run_id"`
	Chain       string         `json:"chain"`
	ChainSource string         `json:"chain_source,omitempty"`
	Target      string         `json:"target"`
	Build       string         `json:"build,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	DurationMS  int64          `json:"duration_ms"`
	Status      string         `json:"status"`
	DryRun      bool           `json:"dry_run,omitempty"`
	KeepGoing   bool           `json:"keep_going,omitempty"`
	Vars        map[string]any `json:"vars,omitempty"`
	Exports     map[string]any `json:"exports,omitempty"`
	Volatile    []string       `json:"volatile,omitempty"`
	Redacted    []string       `json:"redacted,omitempty"`
	Steps       []*StepRecord  `json:"steps"`
	Failure     string         `json:"failure,omitempty"`
	FailedSteps []string       `json:"failed_steps,omitempty"`
	Warning     string         `json:"warning,omitempty"`
}

func (s *StepRecord) AssertionFailed() bool {
	for _, e := range s.Expect {
		if !e.Passed {
			return true
		}
	}
	return false
}

type StepRecord struct {
	Index         int                  `json:"index"`
	ID            string               `json:"id"`
	Call          string               `json:"call"`
	Procedure     string               `json:"procedure"`
	AuthProfile   string               `json:"auth_profile,omitempty"`
	AuthPrincipal string               `json:"auth_principal,omitempty"`
	AuthRetry     string               `json:"auth_retry,omitempty"`
	Status        string               `json:"status"`
	HTTPStatus    int                  `json:"http_status,omitempty"`
	LatencyMS     int64                `json:"latency_ms"`
	Request       json.RawMessage      `json:"request,omitempty"`
	Response      json.RawMessage      `json:"response,omitempty"`
	Transport     *TransportError      `json:"transport_error,omitempty"`
	Expect        []chain.ExpectResult `json:"expect,omitempty"`
	Exported      map[string]any       `json:"exported,omitempty"`
	Error         string               `json:"error,omitempty"`
	Warning       string               `json:"warning,omitempty"`
	Note          string               `json:"note,omitempty"`
	Volatile      []string             `json:"volatile,omitempty"`
	Drift         bool                 `json:"drift,omitempty"`

	serverBuild string
	unreachable string
}

func (s *StepRecord) NotSentUnreachable() bool {
	return s.unreachable != "" && s.Status == StatusSkipped
}

type TransportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r *Record) Step(id string) (*StepRecord, bool) {
	for _, s := range r.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

func (r *Record) Passed() bool { return r.Status == StatusPassed }

func (r *Record) UnmarshalJSON(data []byte) error {
	type plain Record
	var decoded struct {
		*plain
		Vars json.RawMessage `json:"vars,omitempty"`
	}
	decoded.plain = (*plain)(r)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	r.Vars = nil
	if len(decoded.Vars) == 0 || string(decoded.Vars) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(decoded.Vars))
	dec.UseNumber()
	var vars map[string]any
	if err := dec.Decode(&vars); err != nil {
		return err
	}
	for k, v := range vars {
		vars[k] = exactNumber(v)
	}
	r.Vars = vars
	return nil
}

func exactNumber(v any) any {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	case map[string]any:
		for k, inner := range t {
			t[k] = exactNumber(inner)
		}
	case []any:
		for i, inner := range t {
			t[i] = exactNumber(inner)
		}
	}
	return v
}
