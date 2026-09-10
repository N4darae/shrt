package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type cachedToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *LoginTokenSource) UseCache(path, profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachePath = path
	s.cacheProfile = profile
}

func (s *LoginTokenSource) cacheKey() string {
	sum := sha256.New()
	sum.Write([]byte(s.spec.Procedure))
	sum.Write([]byte{0})
	if s.spec.Body != nil {
		if body, err := s.spec.Body(); err == nil {
			sum.Write(body)
		} else {
			return ""
		}
	}
	return s.cacheProfile + "#" + hex.EncodeToString(sum.Sum(nil))[:16]
}

func (s *LoginTokenSource) readCache() (string, time.Time, bool) {
	if s.cachePath == "" {
		return "", time.Time{}, false
	}
	key := s.cacheKey()
	if key == "" {
		return "", time.Time{}, false
	}
	raw, err := os.ReadFile(s.cachePath)
	if err != nil {
		return "", time.Time{}, false
	}
	entries := map[string]cachedToken{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return "", time.Time{}, false
	}
	e, ok := entries[key]
	if !ok || e.Token == "" || e.ExpiresAt.IsZero() {
		return "", time.Time{}, false
	}
	return e.Token, e.ExpiresAt, true
}

func (s *LoginTokenSource) dropCache() {
	if s.cachePath == "" {
		return
	}
	key := s.cacheKey()
	if key == "" {
		return
	}
	raw, err := os.ReadFile(s.cachePath)
	if err != nil {
		return
	}
	entries := map[string]cachedToken{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return
	}
	if _, ok := entries[key]; !ok {
		return
	}
	delete(entries, key)
	body, err := json.Marshal(entries)
	if err != nil {
		return
	}
	tmp := s.cachePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.cachePath)
}

func (s *LoginTokenSource) writeCache(token string, expiresAt time.Time) {
	if s.cachePath == "" || token == "" {
		return
	}
	key := s.cacheKey()
	if key == "" {
		return
	}
	entries := map[string]cachedToken{}
	if raw, err := os.ReadFile(s.cachePath); err == nil {
		_ = json.Unmarshal(raw, &entries)
	}
	cutoff := time.Now()
	for k, e := range entries {
		if k != key && !e.ExpiresAt.IsZero() && e.ExpiresAt.Before(cutoff) {
			delete(entries, k)
		}
	}
	entries[key] = cachedToken{Token: token, ExpiresAt: expiresAt}
	body, err := json.Marshal(entries)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o700); err != nil {
		return
	}
	tmp := s.cachePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.cachePath)
}
