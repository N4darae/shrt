package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/runner"
)

var ErrRunEdited = errors.New("the run record is not the one shrt wrote")

var ErrRunUnsealed = errors.New("the run record predates sealed run records")

func SealState(rec *runner.Record) error {
	if rec.MalformedSeal() {
		return ErrRunEdited
	}
	if rec.Seal == "" {
		if rec.SealingBuildEvidence() != "" {
			return ErrRunEdited
		}
		return ErrRunUnsealed
	}
	seal, err := runSeal(rec)
	if err != nil {
		return err
	}
	if seal != rec.Seal {
		return ErrRunEdited
	}
	return nil
}

func (s *Store) vouch(path string, rec *runner.Record) error {
	switch err := SealState(rec); {
	case errors.Is(err, ErrRunEdited) && (rec.Seal == "" || rec.MalformedSeal()):
		return fmt.Errorf("%w: %s was written by a build that seals run records (%s) but its seal was removed or its format altered, so "+
			"it is not what ran and is not evidence of anything. Restore it, or run the chain again and use the new run", ErrRunEdited, path, rec.SealingBuildEvidence())
	case errors.Is(err, ErrRunEdited):
		return fmt.Errorf("%w: %s was changed after shrt wrote it (its content no longer matches its seal %s), so it is not "+
			"what ran and is not evidence of anything. Restore it, or run the chain again and use the new run", ErrRunEdited, path, rec.Seal)
	case errors.Is(err, ErrRunUnsealed):
		if s.Notes != nil && !s.notedUnsealed {
			s.notedUnsealed = true
			fmt.Fprintf(s.Notes, "note: run %s of %s predates sealed run records (as may others this command reads), so an edit to it "+
				"cannot be ruled out; it is used as recorded. Run the chain again for a record shrt can check\n", rec.RunID, rec.Chain)
		}
		return nil
	default:
		return err
	}
}

func (s *Store) SaveRun(rec *runner.Record) (string, error) {
	path := s.runPath(rec.Chain, rec.RunID)
	rec.Format = runner.RecordFormat
	seal, err := runSeal(rec)
	if err != nil {
		return "", err
	}
	rec.Seal = seal
	if err := writeJSON(path, rec); err != nil {
		return "", err
	}
	return path, nil
}

func runSeal(rec *runner.Record) (string, error) {
	unsealed := *rec
	unsealed.Seal = ""
	raw, err := json.Marshal(&unsealed)
	if err != nil {
		return "", err
	}
	var back runner.Record
	if err := json.Unmarshal(raw, &back); err != nil {
		return "", err
	}
	back.Seal = ""
	canonical, err := json.Marshal(&back)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:32], nil
}

func (s *Store) checkSealed(rec *runner.Record) error {
	path := s.runPath(rec.Chain, rec.RunID)
	disk := &runner.Record{}
	if err := readJSON(path, disk); err != nil {
		return fmt.Errorf("%w: run %s is not saved under %s (%v), so there is no record of it to vouch for", ErrRunEdited, rec.RunID, path, err)
	}
	if why := disk.SealingBuildEvidence(); (disk.Seal == "" || disk.MalformedSeal()) && why != "" {
		return fmt.Errorf("%w: %s was written by a build that seals run records (%s) but its seal was removed or its "+
			"format altered, so it is not what ran. Run the chain again and propose the new run", ErrRunEdited, path, why)
	}
	if disk.Seal == "" {
		return fmt.Errorf("%w: run %s has no seal, because it was written before shrt sealed run records (or its seal was "+
			"removed), so it cannot be checked for edits and cannot be proposed. Run the chain again and propose the new run: "+
			"shrt run %s, then shrt confirm %s -note \"...\"", ErrRunUnsealed, rec.RunID, rec.Chain, rec.Chain)
	}
	seal, err := runSeal(disk)
	if err != nil {
		return err
	}
	if seal != disk.Seal || recordDigest(disk) != recordDigest(rec) {
		return fmt.Errorf("%w: %s was changed after shrt wrote it (its content no longer matches its seal %s), so it "+
			"is not what ran. Run the chain again and propose the new run", ErrRunEdited, path, disk.Seal)
	}
	return nil
}

func RunID(id string) string {
	return strings.TrimSuffix(strings.TrimSpace(id), ".json")
}

func (s *Store) LoadRun(chainName, runID string) (*runner.Record, error) {
	runID = RunID(runID)
	if runID == "" || runID == "latest" {
		return s.LatestRun(chainName)
	}
	path := s.runPath(chainName, runID)
	rec := &runner.Record{}
	if err := readJSON(path, rec); err != nil {
		return nil, fmt.Errorf("load run %s/%s: %w", chainName, runID, err)
	}
	if slug(rec.Chain) != slug(chainName) {
		return nil, fmt.Errorf("load run %s/%s: the record is a run of chain %q, not of %q, and was copied or moved into %s; it is not evidence about %s",
			chainName, runID, rec.Chain, chainName, s.chainDir(chainName), chainName)
	}
	if err := s.vouch(path, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Store) ListRuns(chainName string) ([]string, error) {
	names, err := listJSONFiles(s.chainDir(chainName))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, strings.TrimSuffix(n, ".json"))
	}
	s.orderSameSecond(chainName, out)
	return out, nil
}

func (s *Store) orderSameSecond(chainName string, ids []string) {
	for i := 0; i < len(ids); {
		j := i + 1
		for j < len(ids) && runSecond(ids[j]) == runSecond(ids[i]) {
			j++
		}
		if j-i > 1 {
			s.sortByStart(chainName, ids[i:j])
		}
		i = j
	}
}

func (s *Store) sortByStart(chainName string, ids []string) {
	started := make(map[string]time.Time, len(ids))
	modified := make(map[string]time.Time, len(ids))
	for _, id := range ids {
		if at, ok := recordStartedAt(s.runPath(chainName, id)); ok {
			started[id] = at
		}
		if info, err := os.Stat(s.runPath(chainName, id)); err == nil {
			modified[id] = info.ModTime()
		}
	}
	sort.SliceStable(ids, func(a, b int) bool {
		x, y := ids[a], ids[b]
		if !started[x].Equal(started[y]) {
			return started[x].Before(started[y])
		}
		if !modified[x].Equal(modified[y]) {
			return modified[x].Before(modified[y])
		}
		return x < y
	})
}

func recordStartedAt(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return time.Time{}, false
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return time.Time{}, false
		}
		if key == "started_at" {
			var at time.Time
			if err := dec.Decode(&at); err != nil {
				return time.Time{}, false
			}
			return at, true
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

func (s *Store) FindRun(runID string) ([]*runner.Record, error) {
	runID = RunID(runID)
	entries, err := os.ReadDir(s.RunsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []*runner.Record{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.RunsDir, e.Name(), runID+".json")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		rec := &runner.Record{}
		if err := readJSON(path, rec); err != nil {
			return nil, fmt.Errorf("load run %s: %w", path, err)
		}
		if err := s.vouch(path, rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func runSecond(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}

func (s *Store) LatestRun(chainName string) (*runner.Record, error) {
	ids, err := s.ListRuns(chainName)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no runs recorded for chain %q", chainName)
	}
	return s.LoadRun(chainName, ids[len(ids)-1])
}

func (s *Store) chainDir(chainName string) string {
	return filepath.Join(s.RunsDir, slug(chainName))
}

func (s *Store) runPath(chainName, runID string) string {
	return filepath.Join(s.chainDir(chainName), runID+".json")
}
