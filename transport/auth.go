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
	Profile      string
	EnvRefs      []string
	Target       string
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
	issuedAt     time.Time
	sentAt       time.Time
	fromCache    bool
	accepted     bool
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
	if e, ok := s.readCache(); ok {
		s.set(e, true)
		if !s.stale() {
			return s.token, nil
		}
		s.set(cachedToken{}, false)
	}
	return s.login(ctx)
}

func (s *LoginTokenSource) set(e cachedToken, fromCache bool) {
	s.token, s.expiresAt, s.issuedAt, s.sentAt = e.Token, e.ExpiresAt, e.IssuedAt, e.SentAt
	s.fromCache, s.accepted = fromCache, false
}

func (s *LoginTokenSource) Seed(token string, expiresAt time.Time) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set(cachedToken{Token: token, ExpiresAt: expiresAt, IssuedAt: time.Now()}, false)
}

func (s *LoginTokenSource) Accepted(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || token != s.token {
		return
	}
	s.accepted = true
}

func (s *LoginTokenSource) UntriedCached(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return token != "" && token == s.token && s.fromCache && !s.accepted
}

func (s *LoginTokenSource) Minted(token string) (minted, accepted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || token != s.token {
		return false, false
	}
	return !s.fromCache, s.accepted
}

func (s *LoginTokenSource) Refusal(token string, at time.Time) (TokenRefusal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || token != s.token {
		return TokenRefusal{}, false
	}
	return TokenRefusal{
		Token: fingerprint(token), SentAt: s.sentAt, IssuedAt: s.issuedAt, ExpiresAt: s.expiresAt, RefusedAt: at,
		Cached: s.fromCache, FirstUse: !s.accepted,
	}, true
}

func (s *LoginTokenSource) CurrentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

func (s *LoginTokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set(cachedToken{}, false)
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

func (s *LoginTokenSource) whose() string {
	if s.spec.Profile == "" {
		return ""
	}
	out := fmt.Sprintf("\n       this was the login of auth profile %q", s.spec.Profile)
	if len(s.spec.EnvRefs) > 0 {
		out += ", whose body reads " + strings.Join(s.spec.EnvRefs, ", ") + ": check those are set to credentials this backend accepts"
	}
	return out
}

func (s *LoginTokenSource) login(ctx context.Context) (string, error) {
	body, err := s.spec.Body()
	if err != nil {
		return "", fmt.Errorf("auth body: %w", err)
	}
	var res *Result
	var sentAt time.Time
	for attempt := 1; ; attempt++ {
		sentAt = time.Now()
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
		return "", fmt.Errorf("auth login response has no token at %q; the backend answered %s%s", s.spec.TokenPath, excerpt(raw, 300), s.whose())
	}
	entry := cachedToken{Token: token, IssuedAt: time.Now(), SentAt: sentAt}
	if s.spec.ExpiresPath != "" {
		if unix, ok := lookupInt(payload, s.spec.ExpiresPath); ok && unix > 0 {
			entry.ExpiresAt = time.Unix(unix, 0)
		}
	}
	s.set(entry, false)
	s.logins++
	s.writeCache(entry)
	return token, nil
}

type AuthProfile struct {
	Name   string
	Spec   AuthSpec
	Source TokenSource
	Owns   func(procedure string) bool
}

func (p *AuthProfile) HeaderScheme() (string, string) {
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
	Resend   func(procedure string) bool
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
			header, scheme := profile.HeaderScheme()
			token, err := apply(ctx, profile.Source, call, header, scheme)
			if err != nil {
				return nil, err
			}
			res, err := next(ctx, call)
			if err != nil {
				return res, err
			}
			tracked, _ := profile.Source.(tokenProvenance)
			if !router.unauthenticated(res) {
				if tracked != nil {
					tracked.Accepted(token)
				}
				return res, nil
			}
			untried := tracked != nil && res.Status == http.StatusUnauthorized && tracked.UntriedCached(token)
			minted, accepted := false, false
			if tracked != nil {
				minted, accepted = tracked.Minted(token)
			}
			noteRefusal(call, profile.Source, token)
			profile.Source.Invalidate()
			if !untried && (router.Resend == nil || !router.Resend(call.Procedure)) {
				call.Meta[MetaAuthRetry] = AuthRetryNotResent
				call.Meta[MetaAuthRefused] = true
				switch {
				case accepted:
					call.Meta[MetaAuthRefusedFresh] = FreshTokenAccepted
				case minted:
					call.Meta[MetaAuthRefusedFresh] = FreshTokenMinted
				}
				return res, nil
			}
			token, err = apply(ctx, profile.Source, call, header, scheme)
			if err != nil {
				return nil, err
			}
			call.Meta[MetaAuthRetry] = AuthRetryResent
			if untried {
				call.Meta[MetaAuthRetryCached] = true
			}
			res, err = next(ctx, call)
			if err != nil {
				return res, err
			}
			if router.unauthenticated(res) {
				call.Meta[MetaAuthRefused] = true
				noteRefusal(call, profile.Source, token)
				if tracked != nil {
					if again, _ := tracked.Minted(token); again {
						call.Meta[MetaAuthRefusedFresh] = FreshTokenRelogin
					}
				}
			} else if tracked != nil {
				tracked.Accepted(token)
			}
			return res, nil
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

const (
	MetaAuthRetry        = "auth_retry"
	MetaAuthRetryCached  = "auth_retry_cached"
	MetaAuthRefused      = "auth_refused"
	MetaAuthRefusedFresh = "auth_refused_fresh"
	MetaAuthTokenRefused = "auth_token_refused"
	FreshTokenMinted     = "minted"
	FreshTokenAccepted   = "accepted"
	FreshTokenRelogin    = "relogin"
	AuthRetryResent      = "resent"
	AuthRetryNotResent   = "not_resent"
)

type tokenProvenance interface {
	Accepted(token string)
	UntriedCached(token string) bool
	Minted(token string) (minted, accepted bool)
}

type tokenTiming interface {
	Refusal(token string, at time.Time) (TokenRefusal, bool)
}

func noteRefusal(call *Call, src TokenSource, token string) {
	timed, ok := src.(tokenTiming)
	if !ok {
		return
	}
	r, ok := timed.Refusal(token, time.Now())
	if !ok {
		return
	}
	if call.Meta == nil {
		call.Meta = map[string]any{}
	}
	prior, _ := call.Meta[MetaAuthTokenRefused].([]TokenRefusal)
	call.Meta[MetaAuthTokenRefused] = append(prior, r)
}

func noteProfile(call *Call, name string) {
	if call.Meta == nil {
		call.Meta = map[string]any{}
	}
	call.Meta[MetaAuthProfile] = name
}

func CallAuthProfile(call *Call) (string, bool) {
	v, ok := call.Meta[MetaAuthProfile]
	s, _ := v.(string)
	return s, ok
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
	header, scheme := profile.HeaderScheme()
	setAuthHeader(call, header, scheme+" "+InvalidToken)
	return next(ctx, call)
}

func callSkipsAuth(call *Call) bool {
	b, _ := call.Meta["skip_auth"].(bool)
	return b
}

func callAuthProfile(call *Call) string {
	s, _ := call.Meta["auth"].(string)
	return strings.TrimSpace(s)
}

func apply(ctx context.Context, src TokenSource, call *Call, header, scheme string) (string, error) {
	token, err := src.Token(ctx)
	if err != nil {
		return "", err
	}
	setAuthHeader(call, header, scheme+" "+token)
	return token, nil
}

func setAuthHeader(call *Call, header, value string) {
	if call.Header == nil {
		call.Header = map[string][]string{}
	}
	call.Header.Set(header, value)
}

func (r AuthRouter) unauthenticated(res *Result) bool {
	if res == nil {
		return false
	}
	if res.Status == http.StatusUnauthorized || res.Error != nil && strings.EqualFold(res.Error.Code, "unauthenticated") {
		return true
	}
	return r.Envelope != nil && res.Status == http.StatusOK && strings.EqualFold(r.Envelope(res.Body), "unauthenticated")
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
	v, _ := lookup(root, path)
	s, ok := v.(string)
	return s, ok
}

func lookupInt(root map[string]any, path string) (int64, bool) {
	v, _ := lookup(root, path)
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

func excerpt(raw []byte, max int) string {
	text := strings.Join(strings.Fields(string(raw)), " ")
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}
