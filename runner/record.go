package runner

import (
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
	Index       int                  `json:"index"`
	ID          string               `json:"id"`
	Call        string               `json:"call"`
	Procedure   string               `json:"procedure"`
	AuthProfile string               `json:"auth_profile,omitempty"`
	Status      string               `json:"status"`
	HTTPStatus  int                  `json:"http_status,omitempty"`
	LatencyMS   int64                `json:"latency_ms"`
	Request     json.RawMessage      `json:"request,omitempty"`
	Response    json.RawMessage      `json:"response,omitempty"`
	Transport   *TransportError      `json:"transport_error,omitempty"`
	Expect      []chain.ExpectResult `json:"expect,omitempty"`
	Exported    map[string]any       `json:"exported,omitempty"`
	Error       string               `json:"error,omitempty"`
	Warning     string               `json:"warning,omitempty"`
	Note        string               `json:"note,omitempty"`
	Volatile    []string             `json:"volatile,omitempty"`
	Drift       bool                 `json:"drift,omitempty"`

	serverBuild string
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
