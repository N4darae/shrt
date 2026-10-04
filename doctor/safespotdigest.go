package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/store"
)

const CheckSafeSpotDigests = "safespot-digests"

func checkSafeSpotDigests(_ context.Context, cfg *config.Config, _ Options, r *Report) {
	dir := cfg.Abs(cfg.Paths.SafeSpots)
	names, err := spotFiles(dir)
	if err != nil || len(names) == 0 {
		return
	}
	conflicted, unreadable, edited := []string{}, []string{}, []string{}
	for _, n := range names {
		shown := filepath.ToSlash(filepath.Join(cfg.Paths.SafeSpots, n))
		raw, err := os.ReadFile(filepath.Join(dir, n))
		if err == nil && store.HasConflictMarkers(raw) {
			conflicted = append(conflicted, shown)
			continue
		}
		spot := &store.SafeSpot{}
		if err == nil {
			err = json.Unmarshal(raw, spot)
		}
		switch {
		case err != nil:
			unreadable = append(unreadable, shown+" ("+err.Error()+")")
		case !spot.DigestMatches():
			edited = append(edited, shown)
		}
	}
	for _, path := range conflicted {
		r.add(CheckSafeSpotDigests, LevelError, fmt.Sprintf("safe spot %s holds git merge conflict markers: two branches superseded it "+
			"and the merge left both sides in the file, so no verify can read it", path),
			"take one side whole (git checkout --ours "+path+" or --theirs "+path+"), never a hand merge of the two; run the gate on "+
				"the merged backend, and if it fails or the chain changed, run the chain, propose it (shrt confirm <chain> -supersede "+
				"-note \"...\") and have a person approve it. The losing side's approval stays in git history")
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
	if len(conflicted)+len(unreadable)+len(edited) == 0 {
		r.add(CheckSafeSpotDigests, LevelOK, fmt.Sprintf("%d safe spot(s) load and match the digest sealed at approval", len(names)), "")
	}
}
