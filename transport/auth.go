package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/N4darae/shrt/namecase"
)

type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

type TokenSink interface {
	Seed(token string, expiresAt time.Time)
}

type AuthSpec struct {
	Procedure    string
	Canonicalize func([]byte) ([]byte, error)
	Body         func() ([]byte, error)
	TokenPath    string
	ExpiresPath  string
	Header       string
	Scheme       string
	Leeway       time.Duration
}

type LoginTokenSource struct {
	spec   AuthSpec
	invoke Handler

	RetryBackoff time.Duration

	mu           sync.Mutex
	token        string
	expiresAt    time.Time
	logins       int
	cachePath    string
	cacheProfile string
}

func NewLoginTokenSource(spec AuthSpec, invoke Handler) *LoginTokenSource {
	if spec.Leeway == 0 {
		spec.Leeway = 60 * time.Second
	}
	return &LoginTokenSource{spec: spec, invoke: invoke}
}

func (s *LoginTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && !s.stale() {
		return s.token, nil
	}
	if token, expiresAt, ok := s.readCache(); ok {
		s.token, s.expiresAt = token, expiresAt
		if !s.stale() {
			return s.token, nil
		}
		s.token, s.expiresAt = "", time.Time{}
	}
	return s.login(ctx)
}

func (s *LoginTokenSource) Seed(token string, expiresAt time.Time) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	s.expiresAt = expiresAt
}

func (s *LoginTokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = ""
	s.expiresAt = time.Time{}
	s.dropCache()
}

func (s *LoginTokenSource) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func (s *LoginTokenSource) stale() bool {
	if s.expiresAt.IsZero() {
		return false
	}
	return time.Now().Add(s.spec.Leeway).After(s.expiresAt)
}

const loginRetryAttempts = 5

func (s *LoginTokenSource) loginBackoff() time.Duration {
	if s.RetryBackoff > 0 {
		return s.RetryBackoff
	}
	return 7 * time.Second
}

func (s *LoginTokenSource) login(ctx context.Context) (string, error) {
	body, err := s.spec.Body()
	if err != nil {
		return "", fmt.Errorf("auth body: %w", err)
	}
	var res *Result
	for attempt := 1; ; attempt++ {
		res, err = s.invoke(ctx, &Call{Procedure: s.spec.Procedure, Body: body})
		if err != nil {
			return "", fmt.Errorf("auth login: %w", err)
		}
		if res.Error == nil || res.Error.Code != "resource_exhausted" || attempt >= loginRetryAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(s.loginBackoff()):
		}
	}
	if res.Error != nil {
		return "", fmt.Errorf("auth login rejected: %w", res.Error)
	}
	raw := res.Body
	if s.spec.Canonicalize != nil {
		if canonical, cerr := s.spec.Canonicalize(raw); cerr == nil {
			raw = canonical
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("auth login response is not JSON: %w", err)
	}
	token, ok := lookupString(payload, s.spec.TokenPath)
	if !ok || token == "" {
		return "", fmt.Errorf("auth login response has no token at %q", s.spec.TokenPath)
	}
	s.token = token
	s.expiresAt = time.Time{}
	if s.spec.ExpiresPath != "" {
		if unix, ok := lookupInt(payload, s.spec.ExpiresPath); ok && unix > 0 {
			s.expiresAt = time.Unix(unix, 0)
		}
	}
	s.logins++
	s.writeCache(s.token, s.expiresAt)
	return token, nil
}

type AuthProfile struct {
	Name   string
	Spec   AuthSpec
	Source TokenSource
	Owns   func(procedure string) bool
}

func (p *AuthProfile) HeaderScheme() (string, string) { return p.headerScheme() }

func (p *AuthProfile) headerScheme() (string, string) {
	header, scheme := p.Spec.Header, p.Spec.Scheme
	if header == "" {
		header = "Authorization"
	}
	if scheme == "" {
		scheme = "Bearer"
	}
	return header, scheme
}

type AuthRouter struct {
	Profiles []*AuthProfile
	Default  string
	Skip     func(procedure string) bool
	Envelope func(body []byte) string
}

func (r AuthRouter) byName(name string) *AuthProfile {
	for _, p := range r.Profiles {
		if p != nil && p.Name == name {
			return p
		}
	}
	return nil
}

func (r AuthRouter) Resolve(call *Call) (*AuthProfile, error) {
	if callSkipsAuth(call) {
		return nil, nil
	}
	if name := callAuthProfile(call); name != "" && name != InvalidTokenProfile {
		p := r.byName(name)
		if p == nil {
			return nil, fmt.Errorf("step asks for auth profile %q, which the config does not define (have: %s)",
				name, strings.Join(r.names(), ", "))
		}
		return p, nil
	}
	if r.Skip != nil && r.Skip(call.Procedure) {
		return nil, nil
	}
	for _, p := range r.Profiles {
		if p != nil && p.Owns != nil && p.Owns(call.Procedure) {
			return p, nil
		}
	}
	return r.byName(r.Default), nil
}

func (r AuthRouter) names() []string {
	out := make([]string, 0, len(r.Profiles))
	for _, p := range r.Profiles {
		if p != nil {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}

func WithAuthRouter(router AuthRouter) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, call *Call) (*Result, error) {
			profile, err := router.Resolve(call)
			if err != nil {
				return nil, err
			}
			if callAuthProfile(call) == InvalidTokenProfile {
				noteProfile(call, InvalidTokenProfile)
				return sendInvalidToken(ctx, next, call, profile)
			}
			if profile == nil || profile.Source == nil {
				noteProfile(call, "")
				return next(ctx, call)
			}
			noteProfile(call, profile.Name)
			header, scheme := profile.headerScheme()
			if err := apply(ctx, profile.Source, call, header, scheme); err != nil {
				return nil, err
			}
			res, err := next(ctx, call)
			if err != nil || !router.unauthenticated(res) {
				return res, err
			}
			profile.Source.Invalidate()
			if err := apply(ctx, profile.Source, call, header, scheme); err != nil {
				return nil, err
			}
			return next(ctx, call)
		}
	}
}

func WithAuth(src TokenSource, spec AuthSpec, skip func(procedure string) bool) Middleware {
	return WithAuthRouter(AuthRouter{
		Profiles: []*AuthProfile{{Name: DefaultProfile, Spec: spec, Source: src}},
		Default:  DefaultProfile,
		Skip:     skip,
	})
}

const DefaultProfile = "default"

const MetaAuthProfile = "auth_profile"

func noteProfile(call *Call, name string) {
	if call.Meta == nil {
		call.Meta = map[string]any{}
	}
	call.Meta[MetaAuthProfile] = name
}

func CallAuthProfile(call *Call) (string, bool) {
	v, ok := call.Meta[MetaAuthProfile]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

const (
	InvalidTokenProfile = "invalid"
	InvalidToken        = "shrt-invalid-token"
)

func sendInvalidToken(ctx context.Context, next Handler, call *Call, profile *AuthProfile) (*Result, error) {
	if profile == nil {
		return nil, fmt.Errorf("auth: %s asks for a token the backend never issued, but no auth profile "+
			"covers %s (it is a login or listed in skip_calls), so there is no header to put it in",
			InvalidTokenProfile, call.Procedure)
	}
	header, scheme := profile.headerScheme()
	if call.Header == nil {
		call.Header = map[string][]string{}
	}
	value := InvalidToken
	if scheme != "" {
		value = scheme + " " + InvalidToken
	}
	call.Header.Set(header, value)
	return next(ctx, call)
}

func callSkipsAuth(call *Call) bool {
	v, ok := call.Meta["skip_auth"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func callAuthProfile(call *Call) string {
	v, ok := call.Meta["auth"]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func apply(ctx context.Context, src TokenSource, call *Call, header, scheme string) error {
	token, err := src.Token(ctx)
	if err != nil {
		return err
	}
	if call.Header == nil {
		call.Header = map[string][]string{}
	}
	value := token
	if scheme != "" {
		value = scheme + " " + token
	}
	call.Header.Set(header, value)
	return nil
}

func isUnauthenticated(res *Result) bool {
	if res == nil {
		return false
	}
	if res.Status == 401 {
		return true
	}
	return res.Error != nil && strings.EqualFold(res.Error.Code, "unauthenticated")
}

func (r AuthRouter) unauthenticated(res *Result) bool {
	if isUnauthenticated(res) {
		return true
	}
	if r.Envelope == nil || res == nil || res.Status != http.StatusOK {
		return false
	}
	return strings.EqualFold(r.Envelope(res.Body), "unauthenticated")
}

func EnvelopeCodeReader(path string) func([]byte) string {
	if path == "" {
		return nil
	}
	return func(body []byte) string {
		var root map[string]any
		if json.Unmarshal(body, &root) != nil {
			return ""
		}
		code, _ := lookupString(root, path)
		return code
	}
}

func lookupString(root map[string]any, path string) (string, bool) {
	v, ok := lookup(root, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func lookupInt(root map[string]any, path string) (int64, bool) {
	v, ok := lookup(root, path)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func lookup(root map[string]any, path string) (any, bool) {
	var cur any = root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		key, ok := namecase.LookupKey(m, seg)
		if !ok {
			return nil, false
		}
		cur = m[key]
	}
	return cur, true
}
