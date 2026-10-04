package store

import (
	"cmp"
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
	ProposedBy  string               `json:"proposed_by,omitempty"`
	ProposedAt  *time.Time           `json:"proposed_at,omitempty"`
	Supersedes  string               `json:"supersedes,omitempty"`
	Volatile    []string             `json:"volatile,omitempty"`
	Renamed     []Rename             `json:"renamed,omitempty"`
	ChainDigest string               `json:"chain_digest,omitempty"`
	Digest      string               `json:"digest"`
	Steps       []*runner.StepRecord `json:"steps"`
}

type Rename struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Digest string    `json:"digest_before"`
}

type Confirmation struct {
	By           string
	Note         string
	Acknowledged bool
	Supersede    bool
	Now          time.Time
	Proposal     *Proposal
}

func (s *Store) Promote(rec *runner.Record, c Confirmation) (*SafeSpot, string, error) {
	c.By = strings.TrimSpace(c.By)
	if c.By == "" || !c.Acknowledged {
		return nil, "", ErrNotConfirmed
	}
	if !rec.Passed() {
		return nil, "", notPassed(rec)
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
		ChainDigest: rec.ChainDigest,
		Steps:       rec.Steps,
	}
	if prev != nil {
		spot.Supersedes = prev.RunID
	}
	if c.Proposal != nil {
		at := c.Proposal.ProposedAt
		spot.ProposedBy, spot.ProposedAt = c.Proposal.ProposedBy, &at
	}
	spot.Digest = spot.ComputeDigest()
	if err := writeJSON(path, spot); err != nil {
		return nil, "", err
	}
	return spot, path, nil
}

func (s *Store) LoadSafeSpot(chainName string) (*SafeSpot, error) {
	path := s.SafeSpotPath(chainName)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: safe spot not found at %s", os.ErrNotExist, path)
	}
	spot := &SafeSpot{}
	if err := readJSON(path, spot); err != nil {
		if raw, rerr := os.ReadFile(path); rerr == nil && HasConflictMarkers(raw) {
			return nil, MergeConflictError(path)
		}
		return nil, err
	}
	return spot, nil
}

func (s *Store) RenameSafeSpot(from, to, by string, now time.Time) (*SafeSpot, string, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		return nil, "", ErrNotConfirmed
	}
	spot, err := s.LoadSafeSpot(from)
	if err != nil {
		return nil, "", err
	}
	if kind := spot.DigestKind(); kind != DigestCurrent {
		return nil, "", fmt.Errorf("safe spot %s does not match its digest (%s), so its approval cannot be carried to %s: "+
			"run %s, propose and approve it normally", s.SafeSpotPath(from), orDigestMismatch(kind), to, to)
	}
	if s.HasSafeSpot(to) {
		return nil, "", fmt.Errorf("%w at %s: a rename cannot replace a safe spot; supersede it normally", ErrExists, s.SafeSpotPath(to))
	}
	if now.IsZero() {
		now = time.Now()
	}
	spot.Renamed = append(spot.Renamed, Rename{From: from, To: to, At: now.UTC(), By: by, Digest: spot.Digest})
	spot.Chain = to
	spot.Digest = spot.ComputeDigest()
	path := s.SafeSpotPath(to)
	if err := writeJSON(path, spot); err != nil {
		return nil, "", err
	}
	if err := os.Remove(s.SafeSpotPath(from)); err != nil {
		return nil, "", err
	}
	return spot, path, nil
}

func orDigestMismatch(kind string) string {
	if kind == "" {
		return "hand-edited, or written by another tool"
	}
	return "an older digest format, " + kind
}

func (s *Store) HasSafeSpot(chainName string) bool {
	_, err := os.Stat(s.SafeSpotPath(chainName))
	return err == nil
}

func (s *Store) SafeSpotPath(chainName string) string {
	return filepath.Join(s.SafeSpotsDir, slug(chainName)+".json")
}

func (s *Store) archive(chainName string, prev *SafeSpot) error {
	base := cmp.Or(prev.RunID, prev.ConfirmedAt.UTC().Format("20060102T150405Z"))
	if strings.ContainsAny(base, `/\:*?"<>| `) || strings.HasPrefix(base, ".") {
		base = slug(base)
	}
	dir := filepath.Join(s.SafeSpotsDir, "archive", slug(chainName))
	path := filepath.Join(dir, base+".json")
	for i := 2; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", base, i))
	}
	return writeJSON(path, prev)
}

const (
	DigestCurrent         = "current"
	DigestWithoutApproval = "without-approval"
	DigestLegacy          = "legacy"
)

func (spot *SafeSpot) DigestMatches() bool {
	return spot.DigestKind() != ""
}

func (spot *SafeSpot) DigestKind() string {
	switch spot.Digest {
	case spot.ComputeDigest():
		return DigestCurrent
	case spot.ContentDigest():
		return DigestWithoutApproval
	case legacyDigest(spot.Steps):
		return DigestLegacy
	}
	return ""
}

func (spot *SafeSpot) ComputeDigest() string {
	if len(spot.Renamed) > 0 {
		return hashJSON(struct {
			Content     string
			ConfirmedBy string
			ConfirmedAt time.Time
			Note        string
			ProposedBy  string
			ProposedAt  *time.Time
			Supersedes  string
			Renamed     []Rename
		}{spot.ContentDigest(), spot.ConfirmedBy, spot.ConfirmedAt, spot.Note, spot.ProposedBy, spot.ProposedAt, spot.Supersedes, spot.Renamed})
	}
	return hashJSON(struct {
		Content     string
		ConfirmedBy string
		ConfirmedAt time.Time
		Note        string
		ProposedBy  string
		ProposedAt  *time.Time
		Supersedes  string
	}{spot.ContentDigest(), spot.ConfirmedBy, spot.ConfirmedAt, spot.Note, spot.ProposedBy, spot.ProposedAt, spot.Supersedes})
}

func (spot *SafeSpot) ContentDigest() string {
	if spot.ChainDigest != "" {
		return hashJSON(struct {
			Chain, RunID, Target, Build string
			Volatile                    []string
			ChainDigest                 string
			Steps                       []*runner.StepRecord
		}{spot.Chain, spot.RunID, spot.Target, spot.Build, spot.Volatile, spot.ChainDigest, spot.Steps})
	}
	return hashJSON(struct {
		Chain, RunID, Target, Build string
		Volatile                    []string
		Steps                       []*runner.StepRecord
	}{spot.Chain, spot.RunID, spot.Target, spot.Build, spot.Volatile, spot.Steps})
}

func recordDigest(rec *runner.Record) string {
	return hashJSON(struct {
		Chain, RunID, Target, Build string
		Volatile                    []string
		Vars                        map[string]any
		Steps                       []*runner.StepRecord
	}{rec.Chain, rec.RunID, rec.Target, rec.Build, rec.Volatile, rec.Vars, rec.Steps})
}

func legacyDigest(steps []*runner.StepRecord) string {
	h := sha256.New()
	for _, st := range steps {
		fmt.Fprintf(h, "%s|%s|", st.ID, st.Call)
		raw, _ := json.Marshal(st.Response)
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func hashJSON(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

func notPassed(rec *runner.Record) error {
	failing := []string{}
	for _, st := range rec.Steps {
		if st.Status == runner.StatusFailed || st.Status == runner.StatusError {
			failing = append(failing, fmt.Sprintf("%s (%s)", st.ID, st.Status))
		}
	}
	if len(failing) == 0 {
		failing = append(failing, rec.FailedSteps...)
	}
	where := ""
	if len(failing) > 0 {
		where = ", failing step(s): " + strings.Join(failing, ", ")
	}
	return fmt.Errorf("%w: run %s status=%s%s", ErrRunNotPassed, rec.RunID, rec.Status, where)
}
