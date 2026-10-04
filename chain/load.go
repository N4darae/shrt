package chain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/N4darae/shrt/namecase"
	"github.com/N4darae/shrt/yamlkey"
	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Chain, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadBytes(raw, path)
}

func loadBytes(raw []byte, path string) (*Chain, error) {
	c := &Chain{}
	if err := yamlkey.DecodeStrict(raw, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	markVacuousRules(raw, c)
	if c.Name == "" {
		c.Name = FileStem(path)
	}
	if err := c.Normalize(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.SourcePath = path
	return c, nil
}

func LoadDirPartial(dir string) ([]*Chain, []error, error) {
	files, err := chainFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	out := make([]*Chain, 0, len(files))
	broken := []error{}
	for _, f := range files {
		c, err := LoadFile(f)
		if err != nil {
			broken = append(broken, err)
			continue
		}
		out = append(out, c)
	}
	return out, broken, nil
}

func Resolve(dir, ref string) (*Chain, error) {
	if strings.ContainsAny(ref, "/\\") || strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		return LoadFile(ref)
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(dir, ref+ext)
		if _, err := os.Stat(p); err == nil {
			return LoadFile(p)
		}
	}
	for _, n := range Names(dir) {
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(dir, n+ext)
			if declaredName(p) != ref {
				continue
			}
			if c, err := LoadFile(p); err == nil && c.Name == ref {
				return c, nil
			}
		}
	}
	return nil, fmt.Errorf("chain %q not found in %s%s", ref, dir, DidYouMean(ref, Names(dir)))
}

func FileStem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

type NameMismatchError struct {
	Path, Name, Stem string
	Clash            []string
	NameFile         string
	NameFileChain    string
}

func (e *NameMismatchError) Error() string {
	if len(e.Clash) > 0 {
		return fmt.Sprintf("%s declares name: %s, and so does %s: chain files that share a name would share one run history and "+
			"one safe spot, so shrt run, verify and confirm refuse %s and %s until only one file claims %s",
			e.Path, e.Name, strings.Join(e.Clash, ", "), e.Stem, e.Name, e.Name)
	}
	if e.NameFile != "" {
		return fmt.Sprintf("%s declares name: %s, while %s is another chain (name: %s): these are two different chains, and %s "+
			"means that file by its file name and this chain by its name at once, so shrt run, verify and confirm refuse %s",
			e.Path, e.Name, e.NameFile, e.NameFileChain, e.Name, e.Name)
	}
	return fmt.Sprintf("%s declares name: %s, so its runs and safe spot are %s's, and shrt verify %s and shrt verify %s verify the same chain against that safe spot; "+
		"a reader looking for %s finds no file of that name, and one looking at %s.yaml expects chain %s", e.Path, e.Name, e.Name, e.Stem, e.Name, e.Name, e.Stem, e.Stem)
}

func (e *NameMismatchError) Remedy() string {
	if len(e.Clash) > 0 || e.NameFile != "" {
		return fmt.Sprintf("set name: %s in %s (or drop name:), or delete the file if it is a scratch copy", e.Stem, filepath.Base(e.Path))
	}
	return fmt.Sprintf("rename the file to %s.yaml, or set name: %s (or drop name:)", e.Name, e.Stem)
}

func NameMismatch(c *Chain) error {
	if c == nil || c.SourcePath == "" {
		return nil
	}
	stem := FileStem(c.SourcePath)
	if c.Name == stem {
		return nil
	}
	mm := &NameMismatchError{Path: c.SourcePath, Name: c.Name, Stem: stem}
	dir := filepath.Dir(c.SourcePath)
	for _, p := range Claimants(dir, c.Name) {
		if !sameFile(p, c.SourcePath) {
			mm.Clash = append(mm.Clash, p)
		}
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(dir, c.Name+ext)
		if other, err := LoadFile(p); err == nil && !sameFile(p, c.SourcePath) && other.Name != c.Name {
			mm.NameFile, mm.NameFileChain = p, other.Name
			break
		}
	}
	return mm
}

func Names(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			out = append(out, strings.TrimSuffix(e.Name(), ext))
		}
	}
	return out
}

func DidYouMean(ref string, names []string) string {
	return namecase.Suggest(namecase.Closest(ref, names, 1))
}

func (c *Chain) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

func markVacuousRules(raw []byte, c *Chain) {
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil || len(doc.Content) == 0 {
		return
	}
	steps := mappingValue(doc.Content[0], "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return
	}
	for i, sn := range steps.Content {
		if i >= len(c.Steps) || c.Steps[i] == nil {
			break
		}
		expect := mappingValue(sn, "expect")
		if expect == nil || expect.Kind != yaml.SequenceNode {
			continue
		}
		for j, en := range expect.Content {
			if j >= len(c.Steps[i].Expect) {
				break
			}
			if v := mappingValue(en, "contains"); v != nil && v.Kind == yaml.ScalarNode && v.Value == "" && v.Tag != "!!null" {
				c.Steps[i].Expect[j].vacuous = `contains: ""`
			}
			if v := mappingValue(en, "not_empty"); v != nil && v.Kind == yaml.ScalarNode && v.Tag == "!!bool" && strings.EqualFold(v.Value, "false") {
				c.Steps[i].Expect[j].vacuous = "not_empty: false"
			}
		}
	}
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
