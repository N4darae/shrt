package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/runner"
)

func (s *Store) SaveRun(rec *runner.Record) (string, error) {
	path := s.runPath(rec.Chain, rec.RunID)
	if err := writeJSON(path, rec); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) LoadRun(chainName, runID string) (*runner.Record, error) {
	if runID == "" || runID == "latest" {
		return s.LatestRun(chainName)
	}
	path := s.runPath(chainName, runID)
	rec := &runner.Record{}
	if err := readJSON(path, rec); err != nil {
		return nil, fmt.Errorf("load run %s/%s: %w", chainName, runID, err)
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
		rec := &runner.Record{}
		if err := readJSON(s.runPath(chainName, id), rec); err == nil {
			started[id] = rec.StartedAt
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

func (s *Store) FindRun(runID string) ([]*runner.Record, error) {
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
