package doctor

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

const CheckUpgrade = "upgrade"

type chainCounts map[string]int

func (c chainCounts) total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

func (c chainCounts) names() []string {
	return slices.Sorted(maps.Keys(c))
}

func (c chainCounts) list() string {
	parts := []string{}
	for _, k := range c.names() {
		parts = append(parts, fmt.Sprintf("%s (%d)", k, c[k]))
	}
	return strings.Join(parts, ", ")
}

func sameTarget(recorded, now string) bool {
	return recorded == "" || now == "" || config.SameTarget(recorded, now)
}

type tokenSplit struct {
	mine, other int
	foreign     chainCounts
	expired     int
}

func splitTokens(cfg *config.Config, opts Options, entries map[string]time.Time) (tokenSplit, bool) {
	split := tokenSplit{foreign: chainCounts{}}
	if opts.TokenKeys == nil {
		return split, false
	}
	now := strings.TrimRight(strings.TrimSpace(cfg.Target.BaseURL), "/")
	mine := map[string]bool{}
	for _, k := range opts.TokenKeys(cfg, now) {
		mine[k] = true
	}
	if len(mine) == 0 {
		return split, false
	}
	elsewhere := map[string]string{}
	for _, t := range recordedTargets(cfg) {
		for _, k := range opts.TokenKeys(cfg, t) {
			elsewhere[k] = t
		}
	}
	clock := opts.Now()
	for k, exp := range entries {
		switch t, ok := elsewhere[k]; {
		case mine[k]:
			split.mine++
			if !exp.IsZero() && exp.Before(clock) {
				split.expired++
			}
		case ok:
			split.foreign[t]++
		default:
			split.other++
		}
	}
	return split, true
}

func recordedTargets(cfg *config.Config) []string {
	now := strings.TrimRight(strings.TrimSpace(cfg.Target.BaseURL), "/")
	seen := map[string]bool{}
	note := func(t string) {
		if t != "" && !sameTarget(t, now) {
			seen[strings.TrimRight(t, "/")] = true
		}
	}
	files, _ := filepath.Glob(filepath.Join(cfg.Abs(cfg.Paths.Runs), "*", "*.json"))
	spots, _ := filepath.Glob(filepath.Join(cfg.Abs(cfg.Paths.SafeSpots), "*.json"))
	for _, path := range append(files, spots...) {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var head struct {
			Target string `json:"target"`
		}
		if json.Unmarshal(raw, &head) == nil {
			note(head.Target)
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func readTokenCache(cfg *config.Config) map[string]time.Time {
	raw, err := os.ReadFile(cfg.Abs(filepath.Join(config.DirName, config.TokensFile)))
	if err != nil {
		return nil
	}
	out, _ := parseTokenCache(raw)
	return out
}

func parseTokenCache(raw []byte) (map[string]time.Time, error) {
	entries := map[string]struct {
		ExpiresAt time.Time `json:"expires_at"`
	}{}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(entries))
	for k, e := range entries {
		out[k] = e.ExpiresAt
	}
	return out, nil
}

func checkUpgrade(_ context.Context, cfg *config.Config, opts Options, r *Report) {
	now := strings.TrimRight(strings.TrimSpace(cfg.Target.BaseURL), "/")
	runsDir := cfg.Abs(cfg.Paths.Runs)
	unsealed, edited, foreign := chainCounts{}, chainCounts{}, chainCounts{}
	foreignTargets := map[string]bool{}
	dirs, _ := os.ReadDir(runsDir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(runsDir, d.Name(), "*.json"))
		for _, path := range files {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			rec := &runner.Record{}
			if rec.UnmarshalJSON(raw) != nil {
				continue
			}
			name := cmp.Or(rec.Chain, d.Name())
			switch err := store.SealState(rec); {
			case errors.Is(err, store.ErrRunUnsealed):
				unsealed[name]++
			case errors.Is(err, store.ErrRunEdited):
				edited[name]++
			}
			if !sameTarget(rec.Target, now) {
				foreign[name]++
				foreignTargets[rec.Target] = true
			}
		}
	}
	noPrincipal, foreignSpots := []string{}, []string{}
	s := store.New(runsDir, cfg.Abs(cfg.Paths.SafeSpots))
	spots, _ := filepath.Glob(filepath.Join(s.SafeSpotsDir, "*.json"))
	for _, path := range spots {
		spot := &store.SafeSpot{}
		raw, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(raw, spot) != nil || spot.Chain == "" {
			continue
		}
		if spotLacksPrincipal(spot) {
			noPrincipal = append(noPrincipal, spot.Chain)
		}
		if !sameTarget(spot.Target, now) {
			foreignSpots = append(foreignSpots, fmt.Sprintf("%s (%s)", spot.Chain, spot.Target))
		}
	}
	before := len(r.Findings)
	if n := unsealed.total(); n > 0 {
		cmds := []string{}
		for _, c := range unsealed.names() {
			cmds = append(cmds, "shrt run "+c)
		}
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d run record(s) predate sealed run records, so an edit to them cannot be ruled out and none can be proposed: %s", n, unsealed.list()),
			"re-run each chain for a sealed record, and propose that run instead:\n"+strings.Join(cmds, "\n"))
	}
	if n := edited.total(); n > 0 {
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d run record(s) were changed after shrt wrote them (their content no longer matches their seal), so every command refuses them: %s", n, edited.list()),
			"restore them from where they were copied, or delete them and re-run the chains")
	}
	if len(noPrincipal) > 0 {
		sort.Strings(noPrincipal)
		cmds := []string{}
		for _, c := range noPrincipal {
			cmds = append(cmds, fmt.Sprintf("shrt confirm %s -supersede -note \"...\"", c))
		}
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d safe spot(s) record no auth_principal, so verify cannot tell a run that logged in as another account: %s", len(noPrincipal), strings.Join(noPrincipal, ", ")),
			"run each chain until it passes, propose it in place of the safe spot, and have a person approve it:\n"+strings.Join(cmds, "\n"))
	}
	if len(foreignSpots) > 0 {
		sort.Strings(foreignSpots)
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d safe spot(s) were confirmed against another base_url than %s: %s", len(foreignSpots), orUnset(now), strings.Join(foreignSpots, ", ")),
			"verify compares across targets and says so on every run; to compare like with like, run the chain here,\n"+
				"propose it with 'shrt confirm <chain> -supersede -note \"...\"', and have a person approve it")
	}
	if n := foreign.total(); n > 0 {
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d run record(s) were recorded against another base_url (%s) than %s: %s", n, strings.Join(slices.Sorted(maps.Keys(foreignTargets)), ", "), orUnset(now), foreign.list()),
			"chain slice does not pin from them and chain which does not cite them; re-run the chains here, and delete the old\n"+
				"records once their evidence has served its purpose")
	}
	if split, ok := splitTokens(cfg, opts, readTokenCache(cfg)); ok && split.foreign.total() > 0 {
		r.add(CheckUpgrade, LevelWarn,
			fmt.Sprintf("%d cached token(s) minted against another base_url (%s) sit in %s; no login against %s uses them",
				split.foreign.total(), strings.Join(split.foreign.names(), ", "), config.DirName+"/"+config.TokensFile, orUnset(now)),
			fmt.Sprintf("rm %s drops them (every login here then logs in afresh once)", config.DirName+"/"+config.TokensFile))
	}
	if len(r.Findings) == before {
		r.add(CheckUpgrade, LevelOK, "no record from an older build or another target: every run record is sealed, every safe spot records its principal, and all were recorded against "+orUnset(now), "")
	}
}

func spotLacksPrincipal(spot *store.SafeSpot) bool {
	for _, st := range spot.Steps {
		if st == nil || st.AuthPrincipal != "" {
			continue
		}
		switch st.AuthProfile {
		case "", "none", "invalid":
			continue
		}
		return true
	}
	return false
}

func orUnset(s string) string {
	return cmp.Or(s, "(no base_url)")
}
