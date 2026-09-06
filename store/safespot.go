package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/N4darae/shrt/runner"
)

var (
	ErrNotConfirmed = errors.New("safe spot requires an explicit human confirmation")
	ErrExists       = errors.New("safe spot already exists")
	ErrRunNotPassed = errors.New("run did not pass, it cannot become a safe spot")
)

type SafeSpot struct {
	Chain       string               `json:"chain"`
	RunID       string               `json:"run_id"`
	Target      string               `json:"target"`
	Build       string               `json:"build,omitempty"`
	ConfirmedBy string               `json:"confirmed_by"`
	ConfirmedAt time.Time            `json:"confirmed_at"`
	Note        string               `json:"note,omitempty"`
	Supersedes  string               `json:"supersedes,omitempty"`
	Volatile    []string             `json:"volatile,omitempty"`
	Digest      string               `json:"digest"`
	Steps       []*runner.StepRecord `json:"steps"`
}

type Confirmation struct {
	By           string
	Note         string
	Acknowledged bool
	Supersede    bool
	Now          time.Time
}

func (s *Store) Promote(rec *runner.Record, c Confirmation) (*SafeSpot, string, error) {
	c.By = strings.TrimSpace(c.By)
	if c.By == "" || !c.Acknowledged {
		return nil, "", ErrNotConfirmed
	}
	if !rec.Passed() {
		return nil, "", fmt.Errorf("%w: status=%s", ErrRunNotPassed, rec.Status)
	}
	path := s.SafeSpotPath(rec.Chain)
	prev, err := s.LoadSafeSpot(rec.Chain)
	switch {
	case err == nil:
		if !c.Supersede {
			return nil, "", fmt.Errorf("%w at %s (confirmed by %s at %s)", ErrExists, path, prev.ConfirmedBy, prev.ConfirmedAt.Format(time.RFC3339))
		}
		if err := s.archive(rec.Chain, prev); err != nil {
			return nil, "", err
		}
	case errors.Is(err, os.ErrNotExist):
		prev = nil
	default:
		return nil, "", err
	}

	now := c.Now
	if now.IsZero() {
		now = time.Now()
	}
	spot := &SafeSpot{
		Chain:       rec.Chain,
		RunID:       rec.RunID,
		Target:      rec.Target,
		Build:       rec.Build,
		ConfirmedBy: c.By,
		ConfirmedAt: now.UTC(),
		Note:        c.Note,
		Volatile:    rec.Volatile,
		Steps:       rec.Steps,
	}
	if prev != nil {
		spot.Supersedes = prev.RunID
	}
	spot.Digest = digest(spot.Steps)
	if err := writeJSON(path, spot); err != nil {
		return nil, "", err
	}
	return spot, path, nil
}

func (s *Store) LoadSafeSpot(chainName string) (*SafeSpot, error) {
	path := s.SafeSpotPath(chainName)
	if err := mustExist(path, "safe spot"); err != nil {
		return nil, fmt.Errorf("%w: %w", os.ErrNotExist, err)
	}
	spot := &SafeSpot{}
	if err := readJSON(path, spot); err != nil {
		return nil, err
	}
	return spot, nil
}

func (s *Store) HasSafeSpot(chainName string) bool {
	_, err := os.Stat(s.SafeSpotPath(chainName))
	return err == nil
}

func (s *Store) SafeSpotPath(chainName string) string {
	return filepath.Join(s.SafeSpotsDir, slug(chainName)+".json")
}

func (s *Store) archive(chainName string, prev *SafeSpot) error {
	stamp := prev.ConfirmedAt.UTC().Format("20060102T150405Z")
	path := filepath.Join(s.SafeSpotsDir, "archive", slug(chainName), stamp+".json")
	return writeJSON(path, prev)
}

func digest(steps []*runner.StepRecord) string {
	h := sha256.New()
	for _, st := range steps {
		fmt.Fprintf(h, "%s|%s|", st.ID, st.Call)
		raw, _ := json.Marshal(st.Response)
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
