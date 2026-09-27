package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type TokenRefusal struct {
	Token     string    `json:"token"`
	SentAt    time.Time `json:"sent_at,omitzero"`
	IssuedAt  time.Time `json:"issued_at,omitzero"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	RefusedAt time.Time `json:"refused_at"`
	Cached    bool      `json:"cached,omitempty"`
	FirstUse  bool      `json:"first_use,omitempty"`
}

func fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}

func (r TokenRefusal) Age() (time.Duration, bool) {
	if r.IssuedAt.IsZero() || r.RefusedAt.IsZero() {
		return 0, false
	}
	return r.RefusedAt.Sub(r.IssuedAt), true
}

func (r TokenRefusal) Stated() (time.Duration, bool) {
	if r.IssuedAt.IsZero() || r.ExpiresAt.IsZero() {
		return 0, false
	}
	lo := r.ExpiresAt.Sub(r.IssuedAt.Truncate(time.Second))
	hi := lo
	if !r.SentAt.IsZero() && r.SentAt.Before(r.IssuedAt) {
		hi = r.ExpiresAt.Sub(r.SentAt.Truncate(time.Second))
	}
	for _, unit := range []time.Duration{time.Hour, time.Minute, 10 * time.Second, time.Second} {
		if round := hi.Truncate(unit); round >= lo && round > 0 {
			return round, true
		}
	}
	return lo, true
}

func (r TokenRefusal) Left() (time.Duration, bool) {
	if r.ExpiresAt.IsZero() || r.RefusedAt.IsZero() {
		return 0, false
	}
	return r.ExpiresAt.Sub(r.RefusedAt), true
}

const EarlyRefusalMargin = time.Minute

func (r TokenRefusal) Early() bool {
	left, ok := r.Left()
	if !ok {
		return false
	}
	margin := EarlyRefusalMargin
	if stated, ok := r.Stated(); ok && stated/10 > margin {
		margin = stated / 10
	}
	return left > margin
}
