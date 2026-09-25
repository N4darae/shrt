package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
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

const RecordFormat = 2

var SealsIntroduced = time.Date(2026, 9, 24, 18, 11, 7, 0, time.UTC)

type Record struct {
	Format      int            `json:"format,omitempty"`
	RunID       string         `json:"run_id"`
	Chain       string         `json:"chain"`
	ChainSource string         `json:"chain_source,omitempty"`
	ChainDigest string         `json:"chain_digest,omitempty"`
	Target      string         `json:"target"`
	Build       string         `json:"build,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	DurationMS  int64          `json:"duration_ms"`
	Status      string         `json:"status"`
	DryRun      bool           `json:"dry_run,omitempty"`
	KeepGoing   bool           `json:"keep_going,omitempty"`
	ReplayOf    string         `json:"replay_of,omitempty"`
	Vars        map[string]any `json:"vars,omitempty"`
	Exports     map[string]any `json:"exports,omitempty"`
	Volatile    []string       `json:"volatile,omitempty"`
	Redacted    []string       `json:"redacted,omitempty"`
	Steps       []*StepRecord  `json:"steps"`
	Failure     string         `json:"failure,omitempty"`
	FailedSteps []string       `json:"failed_steps,omitempty"`
	Warning     string         `json:"warning,omitempty"`
	KeptRed     string         `json:"kept_red,omitempty"`
	KeptRedNote string         `json:"kept_red_note,omitempty"`
	KeptRedNew  string         `json:"kept_red_new,omitempty"`
	Seal        string         `json:"seal,omitempty"`

	sealClaim string
}

func (r *Record) SealingBuildEvidence() string {
	if r.sealClaim != "" {
		return r.sealClaim
	}
	if r.Format != 0 {
		return fmt.Sprintf("it carries record format %d", r.Format)
	}
	for _, st := range r.Steps {
		if st == nil {
			continue
		}
		switch {
		case st.BodyRefs != nil:
			return "it carries body_refs, which only a build that seals run records writes"
		case st.AuthPrincipal != "":
			return "it carries auth_principal, which only a build that seals run records writes"
		case st.Unordered != nil:
			return "it carries unordered, which only a build that seals run records writes"
		case st.Headers != nil:
			return "it carries headers, which only a build that seals run records writes"
		}
	}
	started, from := r.StartedAt, "it started at"
	if at, ok := runIDTime(r.RunID); ok && at.After(started) {
		started, from = at, fmt.Sprintf("its run id %s is dated", r.RunID)
	}
	if started.After(SealsIntroduced) {
		return fmt.Sprintf("%s %s, after %s, when shrt began sealing every run record it writes",
			from, started.UTC().Format(time.RFC3339), SealsIntroduced.Format(time.RFC3339))
	}
	return ""
}

func runIDTime(id string) (time.Time, bool) {
	stamp, _, _ := strings.Cut(id, "-")
	at, err := time.Parse("20060102T150405Z", stamp)
	return at, err == nil
}

func (r *Record) MalformedSeal() bool { return r.sealClaim != "" }

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
	LatencyResent []int64              `json:"latency_resent_ms,omitempty"`
	Request       json.RawMessage      `json:"request,omitempty"`
	BodyRefs      map[string]string    `json:"body_refs,omitempty"`
	Headers       map[string]string    `json:"headers,omitzero"`
	Response      json.RawMessage      `json:"response,omitempty"`
	Undeclared    json.RawMessage      `json:"undeclared,omitempty"`
	Transport     *TransportError      `json:"transport_error,omitempty"`
	Expect        []chain.ExpectResult `json:"expect,omitempty"`
	Exported      map[string]any       `json:"exported,omitempty"`
	Error         string               `json:"error,omitempty"`
	Warning       string               `json:"warning,omitempty"`
	Note          string               `json:"note,omitempty"`
	Volatile      []string             `json:"volatile,omitempty"`
	Unordered     []string             `json:"unordered,omitempty"`
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
		Vars   json.RawMessage `json:"vars,omitempty"`
		Format json.RawMessage `json:"format"`
		Seal   *string         `json:"seal"`
	}
	decoded.plain = (*plain)(r)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	r.Format, r.Seal, r.sealClaim = 0, "", ""
	if decoded.Seal != nil {
		r.Seal = *decoded.Seal
		if r.Seal == "" {
			r.sealClaim = "it carries an empty seal, which no build writes"
		}
	}
	if len(decoded.Format) > 0 {
		var n int
		if err := json.Unmarshal(decoded.Format, &n); err != nil || n <= 0 || string(decoded.Format) == "null" {
			r.sealClaim = fmt.Sprintf("its format is %s, which no build writes (a build that seals run records writes a positive whole number)", decoded.Format)
		} else {
			r.Format = n
		}
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
