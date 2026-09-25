package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/store"
)

const CheckSafeSpotDigests = "safespot-digests"

func checkSafeSpotDigests(_ context.Context, cfg *config.Config, _ Options, r *Report) {
	dir := cfg.Abs(cfg.Paths.SafeSpots)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return
	}
	unreadable, edited := []string{}, []string{}
	for _, n := range names {
		shown := filepath.ToSlash(filepath.Join(cfg.Paths.SafeSpots, n))
		raw, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			unreadable = append(unreadable, shown+" ("+err.Error()+")")
			continue
		}
		spot := &store.SafeSpot{}
		if err := json.Unmarshal(raw, spot); err != nil {
			unreadable = append(unreadable, shown+" ("+err.Error()+")")
			continue
		}
		if !spot.DigestMatches() {
			edited = append(edited, shown)
		}
	}
	if len(unreadable) > 0 {
		r.add(CheckSafeSpotDigests, LevelError, fmt.Sprintf("%d safe spot(s) do not load: %s", len(unreadable), strings.Join(unreadable, "; ")),
			"restore each from version control or from its archive, or re-approve it: run the chain, propose it with -supersede and have a person approve it")
	}
	if len(edited) > 0 {
		r.add(CheckSafeSpotDigests, LevelError, fmt.Sprintf("%d safe spot(s) were changed after they were approved, their digest does not "+
			"match their content, so shrt verify refuses them: %s", len(edited), strings.Join(edited, ", ")),
			"restore each from version control or from .shrt/safespots/archive/, or re-approve it: run the chain, propose it with "+
				"'shrt confirm <chain> -supersede -note \"...\"' and have a person approve it")
	}
	if len(unreadable)+len(edited) == 0 {
		r.add(CheckSafeSpotDigests, LevelOK, fmt.Sprintf("%d safe spot(s) load and match the digest sealed at approval", len(names)), "")
	}
}
