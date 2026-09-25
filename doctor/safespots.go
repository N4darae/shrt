package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/config"
)

const CheckSafeSpots = "safespots"

type Orphan struct {
	Name       string
	Path       string
	RenamedTo  string
	Candidates []string
	Near       string
}

func (o Orphan) Line() string {
	switch {
	case o.RenamedTo != "":
		return fmt.Sprintf("safe spot %s has no chain %q: chain %q has the same step ids and calls, so it was likely renamed", o.Path, o.Name, o.RenamedTo)
	case len(o.Candidates) > 0:
		return fmt.Sprintf("safe spot %s has no chain %q: chains %s have the same step ids and calls, so it was likely renamed to one of them", o.Path, o.Name, quoteAll(o.Candidates))
	case o.Near != "":
		return fmt.Sprintf("safe spot %s has no chain %q (did you mean %q?): the chain was deleted or renamed", o.Path, o.Name, o.Near)
	}
	return fmt.Sprintf("safe spot %s has no chain %q: the chain was deleted or renamed", o.Path, o.Name)
}

func quoteAll(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(out, ", ")
}

func (o Orphan) Remedy() string {
	next := "<new-name>"
	if o.RenamedTo != "" {
		next = o.RenamedTo
	} else if o.Near != "" {
		next = o.Near
	}
	return fmt.Sprintf("a safe spot belongs to its chain's name, so it does not follow a rename by itself. If %s is %s renamed and "+
		"nothing else changed, carry the approved safe spot across, which refuses on any other difference: shrt confirm %s "+
		"-rename-from %s -by <email of the user who approved the rename>. Otherwise run it "+
		"(shrt run %s), propose that run (shrt confirm %s -note \"...\") for a person to approve, then remove the orphan: git rm %s. "+
		"If the chain was deleted on purpose, remove the orphan: git rm %s", next, o.Name, next, o.Name, next, next, o.Path, o.Path)
}

func OrphanSafeSpots(cfg *config.Config) []Orphan {
	spots := cfg.Abs(cfg.Paths.SafeSpots)
	entries, err := os.ReadDir(spots)
	if err != nil {
		return nil
	}
	chainsDir := cfg.Abs(cfg.Paths.Chains)
	names := declaredNames(chainsDir)
	out := []Orphan{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if slices.Contains(names, name) {
			continue
		}
		o := Orphan{Name: name, Path: filepath.ToSlash(filepath.Join(cfg.Paths.SafeSpots, e.Name()))}
		o.RenamedTo, o.Candidates = sameStepsAs(filepath.Join(spots, e.Name()), spots, chainsDir, names)
		if o.RenamedTo == "" && len(o.Candidates) == 0 {
			o.Near = strings.TrimSuffix(strings.TrimPrefix(chain.DidYouMean(name, names), " (did you mean \""), "\"?)")
		}
		out = append(out, o)
	}
	return out
}

func sameStepsAs(spotPath, spotsDir, chainsDir string, names []string) (string, []string) {
	raw, err := os.ReadFile(spotPath)
	if err != nil {
		return "", nil
	}
	var spot struct {
		Steps []struct {
			ID     string               `json:"id"`
			Call   string               `json:"call"`
			Expect []chain.ExpectResult `json:"expect"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &spot) != nil || len(spot.Steps) == 0 {
		return "", nil
	}
	same, expected := []string{}, []string{}
	for _, n := range names {
		c, err := chain.Resolve(chainsDir, n)
		if err != nil || len(c.Steps) != len(spot.Steps) {
			continue
		}
		steps, rules := true, true
		for i, s := range c.Steps {
			want := spot.Steps[i]
			if s == nil || s.ID != want.ID || !strings.EqualFold(methodName(s.Call), methodName(want.Call)) {
				steps = false
				break
			}
			if len(s.Expect) != len(want.Expect) {
				rules = false
				continue
			}
			for j, e := range s.Expect {
				r := e.Evaluate(nil)
				if r.Path != want.Expect[j].Path || r.Rule != want.Expect[j].Rule {
					rules = false
				}
			}
		}
		if steps {
			same = append(same, n)
			if rules {
				expected = append(expected, n)
			}
		}
	}
	pool := same
	if len(expected) > 0 {
		pool = expected
	}
	if len(pool) == 1 {
		return pool[0], nil
	}
	unspotted := []string{}
	for _, n := range pool {
		if _, err := os.Stat(filepath.Join(spotsDir, n+".json")); err != nil {
			unspotted = append(unspotted, n)
		}
	}
	if len(unspotted) == 1 {
		return unspotted[0], nil
	}
	if len(unspotted) > 1 {
		pool = unspotted
	}
	if len(pool) > 1 {
		return "", pool
	}
	return "", nil
}

func declaredNames(chainsDir string) []string {
	out := []string{}
	for _, n := range chain.Names(chainsDir) {
		name := n
		for _, ext := range []string{".yaml", ".yml"} {
			if c, err := chain.LoadFile(filepath.Join(chainsDir, n+ext)); err == nil {
				name = c.Name
			}
		}
		out = append(out, name)
	}
	return out
}

func methodName(call string) string {
	return call[strings.LastIndex(call, "/")+1:]
}

func checkSafeSpots(_ context.Context, cfg *config.Config, _ Options, r *Report) {
	chainsDir := cfg.Abs(cfg.Paths.Chains)
	mismatched := 0
	for _, n := range chain.Names(chainsDir) {
		for _, ext := range []string{".yaml", ".yml"} {
			c, err := chain.LoadFile(filepath.Join(chainsDir, n+ext))
			if err != nil {
				continue
			}
			var mm *chain.NameMismatchError
			if errors.As(chain.NameMismatch(c), &mm) {
				mismatched++
				r.add(CheckSafeSpots, LevelWarn, fmt.Sprintf("chain file %s declares name: %s: its runs and safe spot are %s's, and shrt verify %s "+
					"and shrt verify %s both verify it against that safe spot, but the file name does not say so",
					filepath.ToSlash(filepath.Join(cfg.Paths.Chains, n+ext)), c.Name, c.Name, n, c.Name), mm.Remedy())
			}
		}
	}
	orphans := OrphanSafeSpots(cfg)
	if len(orphans) == 0 && mismatched > 0 {
		return
	}
	if len(orphans) == 0 {
		if entries, err := os.ReadDir(cfg.Abs(cfg.Paths.SafeSpots)); err == nil {
			n := 0
			for _, e := range entries {
				if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
					n++
				}
			}
			r.add(CheckSafeSpots, LevelOK, fmt.Sprintf("%d safe spot(s), each with its chain under %s", n, cfg.Paths.Chains), "")
		}
		return
	}
	for _, o := range orphans {
		fate := fmt.Sprintf("; shrt verify %s fails with chain not found, and so does a gate that verifies every safe spot", o.Name)
		if c, err := chain.Resolve(chainsDir, o.Name); err == nil && c.Name != o.Name {
			fate = fmt.Sprintf("; shrt verify %s verifies chain %s against %s's safe spot, never this one", o.Name, c.Name, c.Name)
		}
		r.add(CheckSafeSpots, LevelWarn, o.Line()+fate, o.Remedy())
	}
}
