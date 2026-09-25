package chain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type NameClashError struct {
	Ref    string
	Name   string
	Files  []string
	Stem   string
	StemOf string
}

func (e *NameClashError) Error() string {
	if e.Stem != "" {
		return fmt.Sprintf("%q names two different chains: the file %s (chain %s) and chain %s, declared by %s. "+
			"Nothing was sent: rename a file or change a name: so each chain's file is <name>.yaml, then retry",
			e.Ref, e.Stem, e.StemOf, e.Ref, strings.Join(e.Files, ", "))
	}
	return fmt.Sprintf("%d chain files declare name %q: %s. The name keys the run directory and the safe spot, so their runs "+
		"would share one history and one safe spot, and a proposal could not say which file it came from. Nothing was sent: "+
		"change the name: of all but one of them (name: %s-scratch, say), or delete the copies, then retry",
		len(e.Files), e.Name, strings.Join(e.Files, ", "), e.Name)
}

func (e *NameClashError) Relative(to func(string) string) *NameClashError {
	out := *e
	out.Files = make([]string, len(e.Files))
	for i, p := range e.Files {
		out.Files[i] = to(p)
	}
	if e.Stem != "" {
		out.Stem = to(e.Stem)
	}
	return &out
}

func (e *NameMismatchError) Relative(to func(string) string) *NameMismatchError {
	out := *e
	out.Path = to(e.Path)
	out.Clash = make([]string, len(e.Clash))
	for i, p := range e.Clash {
		out.Clash[i] = to(p)
	}
	if e.NameFile != "" {
		out.NameFile = to(e.NameFile)
	}
	return &out
}

func Claimants(dir, name string) []string {
	out := []string{}
	for _, p := range chainFiles(dir) {
		if c, err := LoadFile(p); err == nil && c.Name == name {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func chainFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func sameFile(a, b string) bool {
	x, err1 := os.Stat(a)
	y, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(x, y)
}

func ResolveUnique(dir, ref string) (*Chain, error) {
	c, err := Resolve(dir, ref)
	if err != nil {
		return nil, err
	}
	isPath := strings.ContainsAny(ref, "/\\") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml")
	if !isPath && c.Name != ref {
		named := Claimants(dir, ref)
		if len(named) > 0 {
			return nil, &NameClashError{Ref: ref, Name: ref, Files: named, Stem: c.SourcePath, StemOf: c.Name}
		}
	}
	claim := Claimants(dir, c.Name)
	inDir := false
	for _, p := range claim {
		if sameFile(p, c.SourcePath) {
			inDir = true
		}
	}
	if inDir && len(claim) > 1 {
		return nil, &NameClashError{Ref: ref, Name: c.Name, Files: claim}
	}
	return c, nil
}
