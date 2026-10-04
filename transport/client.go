package transport

import (
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	neturl "net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ProtocolVersionHeader = "Connect-Protocol-Version"
	ProtocolVersion       = "1"
	ContentTypeJSON       = "application/json"
	ContentTypeStreamJSON = "application/connect+json"
	maxBodyBytes          = 32 << 20
)

var StreamWait = 5 * time.Second

type Call struct {
	Procedure string
	Body      []byte
	Header    http.Header
	Meta      map[string]any
	Stream    int
}

type Result struct {
	Status  int
	Body    []byte
	Header  http.Header
	Error   *Error
	Latency time.Duration
}

type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details []json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

type Handler func(ctx context.Context, call *Call) (*Result, error)

type Middleware func(next Handler) Handler

type Client struct {
	baseURL  string
	http     *http.Client
	headers  map[string]string
	hostOver string
	chain    Handler
}

type Options struct {
	BaseURL      string
	HostOverride string
	Timeout      time.Duration
	Headers      map[string]string
	HTTPClient   *http.Client
	Middlewares  []Middleware
}

func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cmp.Or(opts.Timeout, 30*time.Second)}
	}
	if opts.HostOverride != "" {
		switch t := hc.Transport.(type) {
		case nil:
			hc.Transport = &http.Transport{
				TLSClientConfig:     &tls.Config{ServerName: opts.HostOverride, MinVersion: tls.VersionTLS12},
				MaxIdleConnsPerHost: 8,
				ForceAttemptHTTP2:   true,
			}
		case *http.Transport:
			if t.TLSClientConfig == nil || t.TLSClientConfig.ServerName == "" {
				clone := t.Clone()
				if clone.TLSClientConfig == nil {
					clone.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
				}
				clone.TLSClientConfig.ServerName = opts.HostOverride
				shallow := *hc
				shallow.Transport = clone
				hc = &shallow
			}
		}
	}
	shallow := *hc
	shallow.CheckRedirect = refuseRedirect
	hc = &shallow
	c := &Client{
		baseURL:  strings.TrimRight(opts.BaseURL, "/"),
		http:     hc,
		headers:  opts.Headers,
		hostOver: opts.HostOverride,
	}
	c.chain = Wrap(c.send, opts.Middlewares...)
	return c
}

func Wrap(base Handler, mws ...Middleware) Handler {
	h := base
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func (c *Client) Do(ctx context.Context, call *Call) (*Result, error) {
	if call.Header == nil {
		call.Header = http.Header{}
	}
	return c.chain(ctx, call)
}

func (c *Client) Raw(ctx context.Context, call *Call) (*Result, error) {
	if call.Header == nil {
		call.Header = http.Header{}
	}
	return c.send(ctx, call)
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) send(ctx context.Context, call *Call) (*Result, error) {
	url := c.baseURL + normalizeProcedure(call.Procedure)
	body := call.Body
	if len(body) == 0 {
		body = []byte("{}")
	}
	contentType := ContentTypeJSON
	if call.Stream > 0 {
		contentType, body = ContentTypeStreamJSON, frame(0, body)
	}
	parent := ctx
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.hostOver != "" {
		req.Host = c.hostOver
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(ProtocolVersionHeader, ProtocolVersion)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, values := range call.Header {
		req.Header.Del(k)
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	var wrote atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wrote.Store(true)
			}
		},
	}))
	start := time.Now()
	hc := *c.http
	hc.Transport = noResend{next: c.http.Transport}
	resp, err := hc.Do(req)
	if err != nil {
		var urlErr *neturl.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		if errors.Is(err, errResendRefused) {
			return nil, fmt.Errorf("POST %s: %w", url, closedError(err, wrote.Load()))
		}
		if Unreachable(err) {
			return nil, fmt.Errorf("POST %s: the target could not be reached (%w): the backend is down or not started, "+
				"so nothing reached it and this is not a backend defect", url, err)
		}
		if ConnectionClosed(err) {
			return nil, fmt.Errorf("POST %s: %w", url, closedError(err, wrote.Load()))
		}
		if TimedOut(err) && ctx.Err() == nil {
			return nil, fmt.Errorf("POST %s: %s (%s): %w", url, NoAnswerBeforeTimeout, c.http.Timeout, err)
		}
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if call.Stream > 0 && resp.StatusCode == http.StatusOK {
		timer := time.AfterFunc(StreamWait, stop)
		defer timer.Stop()
		res, err := readStream(resp.Body, call.Stream)
		if err != nil && (ctx.Err() == nil || parent.Err() != nil) {
			return nil, fmt.Errorf("read %s: %w", url, err)
		}
		res.Header, res.Latency = resp.Header.Clone(), time.Since(start)
		return res, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		if ConnectionClosed(err) {
			return nil, fmt.Errorf("read %s: %w", url, closedError(err, true))
		}
		if TimedOut(err) && ctx.Err() == nil {
			return nil, fmt.Errorf("read %s: %s (%s): %w", url, NoAnswerBeforeTimeout, c.http.Timeout, err)
		}
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("POST %s: the target answered %d with Location %q; shrt never follows redirects, "+
			"because following one would re-send the request body and the Authorization header to wherever it points "+
			"and record another service's answer as the target's: point target.base_url at the final address", url, resp.StatusCode, resp.Header.Get("Location"))
	}
	res := &Result{
		Status:  resp.StatusCode,
		Body:    raw,
		Header:  resp.Header.Clone(),
		Latency: time.Since(start),
	}
	if resp.StatusCode != http.StatusOK {
		res.Error = decodeError(resp.StatusCode, raw)
	}
	return res, nil
}

var errResendRefused = errors.New("the request had already been sent and shrt never re-sends one")

func refuseResend() (io.ReadCloser, error) {
	return nil, errResendRefused
}

type noResend struct {
	next http.RoundTripper
}

func (n noResend) RoundTrip(req *http.Request) (*http.Response, error) {
	next := n.next
	if next == nil {
		next = http.DefaultTransport
	}
	if req.Body != nil && req.Body != http.NoBody {
		clone := *req
		clone.GetBody = refuseResend
		req = &clone
	}
	return next.RoundTrip(req)
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func decodeError(status int, raw []byte) *Error {
	e := &Error{}
	if err := json.Unmarshal(raw, e); err != nil || e.Code == "" {
		return &Error{Code: fmt.Sprintf("http_%d", status), Message: strings.TrimSpace(string(raw))}
	}
	return e
}

func normalizeProcedure(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

func frame(flags byte, payload []byte) []byte {
	out := make([]byte, 5, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:], uint32(len(payload)))
	return append(out, payload...)
}

func readStream(r io.Reader, want int) (*Result, error) {
	messages := []json.RawMessage{}
	res := &Result{Status: http.StatusOK}
	done := func() *Result {
		if res.Error == nil {
			res.Body, _ = json.Marshal(map[string]any{"messages": messages})
		}
		return res
	}
	for len(messages) < want {
		head := make([]byte, 5)
		if _, err := io.ReadFull(r, head); err != nil {
			if errors.Is(err, io.EOF) {
				return done(), nil
			}
			return done(), err
		}
		size := binary.BigEndian.Uint32(head[1:])
		if size > maxBodyBytes {
			return done(), fmt.Errorf("a stream frame of %d bytes is over the %d-byte limit", size, maxBodyBytes)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r, payload); err != nil {
			return done(), err
		}
		if head[0]&0x01 != 0 {
			return done(), errors.New("the stream sent a compressed frame, and shrt asks for none")
		}
		if head[0]&0x02 == 0 {
			messages = append(messages, json.RawMessage(payload))
			continue
		}
		var end struct {
			Error *Error `json:"error"`
		}
		if err := json.Unmarshal(payload, &end); err == nil && end.Error != nil && end.Error.Code != "" {
			res.Error, res.Status = end.Error, connectStatus(end.Error.Code)
			res.Body, _ = json.Marshal(end.Error)
		}
		return done(), nil
	}
	return done(), nil
}

func connectStatus(code string) int {
	switch code {
	case "canceled":
		return 499
	case "invalid_argument", "failed_precondition", "out_of_range":
		return http.StatusBadRequest
	case "deadline_exceeded":
		return http.StatusGatewayTimeout
	case "not_found":
		return http.StatusNotFound
	case "already_exists", "aborted":
		return http.StatusConflict
	case "permission_denied":
		return http.StatusForbidden
	case "resource_exhausted":
		return http.StatusTooManyRequests
	case "unimplemented":
		return http.StatusNotImplemented
	case "unavailable":
		return http.StatusServiceUnavailable
	case "unauthenticated":
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}
