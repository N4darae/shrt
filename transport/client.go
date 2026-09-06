package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ProtocolVersionHeader = "Connect-Protocol-Version"
	ProtocolVersion       = "1"
	ContentTypeJSON       = "application/json"
	maxBodyBytes          = 32 << 20
)

type Call struct {
	Procedure string
	Body      []byte
	Header    http.Header
	Meta      map[string]any
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
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.hostOver != "" {
		req.Host = c.hostOver
	}
	req.Header.Set("Content-Type", ContentTypeJSON)
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

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
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
