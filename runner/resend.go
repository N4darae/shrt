package runner

import (
	"context"
	"fmt"
	"net/http"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/transport"
)

type Attempt struct {
	HTTPStatus int    `json:"http_status,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
}

func (a *Attempt) Text() string {
	if a.Message == "" {
		return a.Code
	}
	return a.Code + ": " + a.Message
}

var resendCodes = map[string]bool{
	"internal": true, "unknown": true, "unavailable": true, "resource_exhausted": true,
	"data_loss": true, "aborted": true, "deadline_exceeded": true,
}

func resendable(step *chain.Step, res *transport.Result, err error) bool {
	if err != nil || res == nil || res.Error == nil || !chain.IsReadOnlyCall(step.Call) || chain.HasTransportExpectation(step.Expect) {
		return false
	}
	switch res.Error.Code {
	case "http_502", "http_503", "http_504":
		return false
	}
	return resendCodes[res.Error.Code] || res.Status >= http.StatusInternalServerError
}

func authMeta(step *chain.Step) map[string]any {
	if !step.SkipAuth && step.Auth == "" {
		return nil
	}
	return map[string]any{"skip_auth": step.SkipAuth, "auth": step.Auth}
}

func (r *Runner) resendRead(ctx context.Context, step *chain.Step, call *transport.Call, header http.Header, sr *StepRecord, res *transport.Result, err error) *transport.Result {
	if !resendable(step, res, err) {
		return res
	}
	again := &transport.Call{Procedure: call.Procedure, Body: call.Body, Header: header.Clone(), Stream: call.Stream, Meta: authMeta(step)}
	next, err := r.Client.Do(ctx, again)
	if err != nil {
		return res
	}
	sr.FirstAttempt = &Attempt{HTTPStatus: res.Status, Code: res.Error.Code, Message: res.Error.Message, LatencyMS: res.Latency.Milliseconds()}
	how := "judged on the re-send"
	if next.Error != nil {
		how = "the re-send failed too"
	}
	sr.Warning = joinLines(sr.Warning, fmt.Sprintf("a read re-sent once after %s (HTTP %d); %s", sr.FirstAttempt.Text(), res.Status, how))
	return next
}
