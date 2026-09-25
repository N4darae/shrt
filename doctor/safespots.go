package doctor

import (
	"context"
	"encoding/json"
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
	Name      string
	Path      string
	RenamedTo string
	Near      string
}

func (o Orphan) Line() string {
	switch {
	case o.RenamedTo != "":
		return fmt.Sprintf("safe spot %s has no chain %q: chain %q has the same step ids and calls, so it was likely renamed", o.Path, o.Name, o.RenamedTo)
	case o.Near != "":
		return fmt.Sprintf("safe spot %s has no chain %q (did you mean %q?): the chain was deleted or renamed", o.Path, o.Name, o.Near)
	}
	return fmt.Sprintf("safe spot %s has no chain %q: the chain was deleted or renamed", o.Path, o.Name)
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
	names := chain.Names(chainsDir)
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
		o.RenamedTo = sameStepsAs(filepath.Join(spots, e.Name()), chainsDir, names)
		if o.RenamedTo == "" {
			o.Near = strings.TrimSuffix(strings.TrimPrefix(chain.DidYouMean(name, names), " (did you mean \""), "\"?)")
		}
		out = append(out, o)
	}
	return out
}

func sameStepsAs(spotPath, chainsDir string, names []string) string {
	raw, err := os.ReadFile(spotPath)
	if err != nil {
		return ""
	}
	var spot struct {
		Steps []struct {
			ID   string `json:"id"`
			Call string `json:"call"`
		} `json:"steps"`
	}
	if json.Unmarshal(raw, &spot) != nil || len(spot.Steps) == 0 {
		return ""
	}
	found := ""
	for _, n := range names {
		c, err := chain.Resolve(chainsDir, n)
		if err != nil || len(c.Steps) != len(spot.Steps) {
			continue
		}
		same := true
		for i, s := range c.Steps {
			want := spot.Steps[i]
			if s == nil || s.ID != want.ID || !strings.EqualFold(methodName(s.Call), methodName(want.Call)) {
				same = false
				break
			}
		}
		if same {
			if found != "" {
				return ""
			}
			found = n
		}
	}
	return found
}

func methodName(call string) string {
	return call[strings.LastIndex(call, "/")+1:]
}

func checkSafeSpots(_ context.Context, cfg *config.Config, _ Options, r *Report) {
	orphans := OrphanSafeSpots(cfg)
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
		r.add(CheckSafeSpots, LevelWarn, o.Line()+fmt.Sprintf("; shrt verify %s fails with chain not found, and so does a gate that verifies every safe spot", o.Name), o.Remedy())
	}
}
