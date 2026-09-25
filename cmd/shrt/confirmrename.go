package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/diff"
	"github.com/N4darae/shrt/pathmask"
	"github.com/N4darae/shrt/runner"
	"github.com/N4darae/shrt/store"
)

func renameSafeSpot(e *env, to, from, by string) error {
	by, err := approverEmail(by)
	if err != nil {
		return fmt.Errorf("%w\n-rename-from records who carried the approval across the rename, under the same rule as -approve", err)
	}
	if from == to {
		return fmt.Errorf("-rename-from %s names the chain itself; nothing to rename", from)
	}
	c, err := chain.Resolve(e.chainsDir(), to)
	if err != nil {
		return err
	}
	if _, err := chain.Resolve(e.chainsDir(), from); err == nil {
		return fmt.Errorf("chain %s still exists, so %s is a copy, not a rename: a safe spot follows a rename only once the old "+
			"chain file is gone. Delete or rename %s.yaml first, or run %s, propose and approve it normally", from, to, from, to)
	}
	spot, err := e.store.LoadSafeSpot(from)
	if err != nil {
		return fmt.Errorf("no safe spot for %s to carry across the rename: %w", from, err)
	}
	if e.store.HasSafeSpot(to) {
		return fmt.Errorf("chain %s already has a safe spot (%s): a rename cannot replace it; supersede it normally",
			to, rel(e.cfg.Root, e.store.SafeSpotPath(to)))
	}
	for _, name := range []string{from, to} {
		if e.store.HasProposal(name) {
			return fmt.Errorf("a proposal for %s is pending: approve or reject it first (shrt confirm %s -reject)", name, name)
		}
	}
	if err := keptRedNeverConfirmed(e, to); err != nil {
		return err
	}
	if spot.ChainDigest == "" {
		return fmt.Errorf("chain %s cannot be shown to be %s renamed: the safe spot %s does not record the chain it was confirmed with "+
			"(it was approved by an older shrt), so nothing proves that vars defaults, templates, allow_fail, exports, redact or kept_red "+
			"are unchanged.\nRun %s, propose that run (shrt confirm %s -note \"...\") and have a person approve it",
			to, from, rel(e.cfg.Root, e.store.SafeSpotPath(from)), to, to)
	}
	compared, diffErr := renameDifference(e, spot, c, from)
	if diffErr != "" {
		return fmt.Errorf("chain %s is not %s renamed: %s.\nA safe spot carries its approval across a pure rename only, so run %s, "+
			"propose that run (shrt confirm %s -note \"...\") and have a person approve it", to, from, diffErr, to, to)
	}
	moved, path, err := e.store.RenameSafeSpot(from, to, by, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("safe spot of %q carried to %q unchanged: %s\n", from, to, compared)
	fmt.Printf("  no new approval is needed: the content is the one %s approved at %s\n", moved.ConfirmedBy, moved.ConfirmedAt.Format(time.RFC3339))
	fmt.Printf("  rename recorded: from %s, by %s at %s\n", from, by, moved.Renamed[len(moved.Renamed)-1].At.Format(time.RFC3339))
	fmt.Printf("  digest:      %s\n  file:        %s (was %s)\n", moved.Digest, rel(e.cfg.Root, path), rel(e.cfg.Root, e.store.SafeSpotPath(from)))
	runs := filepath.Join(e.cfg.Abs(e.cfg.Paths.Runs), from)
	if _, err := os.Stat(runs); err == nil {
		fmt.Printf("  run records of %s stay under %s: they are machine-local evidence of the old name, not read by verify %s;\n"+
			"  remove them when done: rm -rf %s\n", from, rel(e.cfg.Root, runs), to, rel(e.cfg.Root, runs))
	}
	fmt.Printf("  commit both paths: git add %s %s\n",
		rel(e.cfg.Root, path), rel(e.cfg.Root, e.store.SafeSpotPath(from)))
	return nil
}

func renameDifference(e *env, spot *store.SafeSpot, c *chain.Chain, from string) (string, string) {
	if changes := diff.ChainChanges(spot, c); len(changes) > 0 {
		ch := changes[0]
		return "", fmt.Sprintf("the chain differs from what the safe spot recorded at %s %s (%s)", ch.Step, ch.Path, ch.Transition())
	}
	if len(spot.Steps) != len(c.Steps) {
		return "", fmt.Sprintf("the safe spot has %d step(s) and the chain %d", len(spot.Steps), len(c.Steps))
	}
	if why := volatileDiffers(spot.Volatile, append(append([]string{}, e.cfg.Volatile...), c.Volatile...)); why != "" {
		return "", "chain-level volatile " + why
	}
	for i, st := range spot.Steps {
		s := c.Steps[i]
		if why := stepDiffers(st, s, c, protoDefault(e, true)); why != "" {
			return "", "step " + st.ID + ": " + why
		}
	}
	now := c.Digest()
	if now != spot.ChainDigest {
		why := fmt.Sprintf("the chain file is not the one the safe spot was confirmed with (chain digest %s, now %s)", spot.ChainDigest, now)
		if old, where, ok := chainLastCommitted(e, from); ok && old.Digest() == spot.ChainDigest {
			if at := chainFileDiffers(old, c); at != "" {
				why += "; against " + from + ".yaml at " + where + " it differs in " + at
			}
		} else {
			why += "; it differs somewhere a run record does not show, such as vars defaults, a template that resolves to the same value, " +
				"allow_fail, export, redact, kept_red, unordered or a description"
		}
		return "", why
	}
	return "identical, apart from its name, to the chain the safe spot was confirmed with (chain digest " + now + ")", ""
}

func volatileDiffers(recorded, now []string) string {
	a, b := sortedSet(recorded), sortedSet(now)
	if reflect.DeepEqual(a, b) {
		return ""
	}
	return fmt.Sprintf("paths differ: recorded [%s], now [%s]", strings.Join(a, ", "), strings.Join(b, ", "))
}

func sortedSet(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}

func stepDiffers(st *runner.StepRecord, s *chain.Step, c *chain.Chain, isDefault func(procedure, path string, v any) bool) string {
	if why := volatileDiffers(st.Volatile, s.Volatile); why != "" {
		return "volatile " + why
	}
	if why := volatileDiffers(st.Unordered, append(append([]string{}, c.Unordered...), s.Unordered...)); why != "" {
		return "unordered " + why
	}
	profile := s.Auth
	if profile == "" && !s.SkipAuth {
		profile = "default"
	}
	if st.AuthProfile != "" && st.AuthProfile != profile {
		return fmt.Sprintf("auth profile %s -> %s", st.AuthProfile, orNone(profile))
	}
	for k, v := range s.Headers {
		got, ok := st.Headers[k]
		if !ok {
			return "header " + k + " was not sent by the confirmed run"
		}
		if !templateMatches(v, got) {
			return fmt.Sprintf("header %s %q does not produce the recorded %q", k, v, got)
		}
	}
	for k := range st.Headers {
		if _, ok := s.Headers[k]; !ok {
			return "header " + k + " was sent by the confirmed run and the chain no longer sets it"
		}
	}
	var sent any
	if len(st.Request) > 0 {
		if err := json.Unmarshal(st.Request, &sent); err != nil {
			return "the recorded request does not decode"
		}
	}
	wanted := map[string]bool{}
	var why string
	var walk func(v any, path string)
	walk = func(v any, path string) {
		if why != "" {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				walk(x, pathmask.Join(path, k))
			}
			return
		case []any:
			for i, x := range t {
				walk(x, pathmask.Join(path, pathmask.IndexKey(i)))
			}
			return
		}
		wanted[path] = true
		got, ok := chain.Get(sent, path)
		text := fmt.Sprint(v)
		switch {
		case !ok && strings.Contains(text, "${"):
			why = "body " + path + " was not sent by the confirmed run"
		case !ok && (isDefault == nil || !isDefault(st.Procedure, path, v)):
			why = fmt.Sprintf("body %s %q was not sent by the confirmed run", path, text)
		case !ok:
		case fmt.Sprint(got) == pathmask.MaskRedacted:
		case !templateMatches(text, fmt.Sprint(got)):
			why = fmt.Sprintf("body %s %q does not produce the recorded %q", path, text, fmt.Sprint(got))
		}
	}
	walk(s.Body, "")
	if why != "" {
		return why
	}
	var extra func(v any, path string)
	extra = func(v any, path string) {
		if why != "" {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				extra(x, pathmask.Join(path, k))
			}
			return
		case []any:
			for i, x := range t {
				extra(x, pathmask.Join(path, pathmask.IndexKey(i)))
			}
			return
		}
		if path != "" && !wanted[path] && !chainSetsBelow(wanted, path) {
			why = "body " + path + " was sent by the confirmed run and the chain no longer sets it"
		}
	}
	extra(sent, "")
	return why
}

func chainSetsBelow(wanted map[string]bool, path string) bool {
	for p := range wanted {
		if p == path || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

var templatePart = regexp.MustCompile(`\$\{[^}]*\}`)

func templateMatches(template, got string) bool {
	if !strings.Contains(template, "${") {
		return template == got
	}
	parts := templatePart.Split(template, -1)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile("^" + strings.Join(quoted, ".*") + "$")
	return err == nil && re.MatchString(got)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func chainLastCommitted(e *env, name string) (*chain.Chain, string, bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, "", false
	}
	relPath, err := filepath.Rel(e.cfg.Root, filepath.Join(e.chainsDir(), name+".yaml"))
	if err != nil {
		return nil, "", false
	}
	relPath = filepath.ToSlash(relPath)
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = e.cfg.Root
		return cmd.Output()
	}
	where := "git HEAD"
	raw, err := git("show", "HEAD:"+relPath)
	if err != nil {
		last, lerr := git("rev-list", "-1", "HEAD", "--", relPath)
		commit := strings.TrimSpace(string(last))
		if lerr != nil || commit == "" {
			return nil, "", false
		}
		if raw, err = git("show", commit+"^:"+relPath); err != nil {
			return nil, "", false
		}
		where = "commit " + commit[:min(len(commit), 12)] + "^"
	}
	dir, err := os.MkdirTemp("", "shrt-rename-")
	if err != nil {
		return nil, "", false
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return nil, "", false
	}
	old, err := chain.LoadFile(path)
	if err != nil {
		return nil, "", false
	}
	return old, where, true
}

func chainFileDiffers(old, now *chain.Chain) string {
	a, b := *old, *now
	a.Name, b.Name = "", ""
	a.SourcePath, b.SourcePath = "", ""
	var x, y any
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	_ = json.Unmarshal(ra, &x)
	_ = json.Unmarshal(rb, &y)
	return firstJSONDifference(x, y, "")
}

func firstJSONDifference(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return orRoot(path)
		}
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		for k := range y {
			if _, seen := x[k]; !seen {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if d := firstJSONDifference(x[k], y[k], pathmask.Join(path, k)); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return orRoot(path)
		}
		for i := range x {
			if d := firstJSONDifference(x[i], y[i], pathmask.Join(path, pathmask.IndexKey(i))); d != "" {
				return d
			}
		}
		return ""
	}
	if !reflect.DeepEqual(a, b) {
		return orRoot(path)
	}
	return ""
}

func orRoot(path string) string {
	if path == "" {
		return "its content"
	}
	return path
}
