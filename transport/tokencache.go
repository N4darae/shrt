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
	IssuedAt  time.Time `json:"issued_at,omitzero"`
	SentAt    time.Time `json:"sent_at,omitzero"`
}

func (s *LoginTokenSource) UseCache(path, profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachePath = path
	s.cacheProfile = profile
}

func (s *LoginTokenSource) CacheKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cacheKey()
}

func (s *LoginTokenSource) cacheKey() string {
	sum := sha256.New()
	sum.Write([]byte(s.spec.Target))
	sum.Write([]byte{0})
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

func (s *LoginTokenSource) loadCache() (string, map[string]cachedToken, bool) {
	if s.cachePath == "" {
		return "", nil, false
	}
	key := s.cacheKey()
	if key == "" {
		return "", nil, false
	}
	entries := map[string]cachedToken{}
	raw, err := os.ReadFile(s.cachePath)
	if err != nil {
		return key, entries, false
	}
	err = json.Unmarshal(raw, &entries)
	return key, entries, err == nil
}

func (s *LoginTokenSource) readCache() (cachedToken, bool) {
	key, entries, ok := s.loadCache()
	e := entries[key]
	if !ok || e.Token == "" || e.ExpiresAt.IsZero() {
		return cachedToken{}, false
	}
	return e, true
}

func (s *LoginTokenSource) dropCache() {
	key, entries, ok := s.loadCache()
	if _, found := entries[key]; !ok || !found {
		return
	}
	delete(entries, key)
	s.saveCache(entries)
}

func (s *LoginTokenSource) writeCache(entry cachedToken) {
	key, entries, _ := s.loadCache()
	if key == "" {
		return
	}
	cutoff := time.Now()
	for k, e := range entries {
		if k != key && !e.ExpiresAt.IsZero() && e.ExpiresAt.Before(cutoff) {
			delete(entries, k)
		}
	}
	entries[key] = entry
	s.saveCache(entries)
}

func (s *LoginTokenSource) saveCache(entries map[string]cachedToken) {
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
