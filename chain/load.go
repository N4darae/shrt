package chain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	c := &Chain{}
	if err := decodeStrict(raw, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	markVacuousRules(raw, c)
	if c.Name == "" {
		c.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if err := c.Normalize(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.SourcePath = path
	return c, nil
}

func LoadDirPartial(dir string) ([]*Chain, []error, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".yaml" || ext == ".yml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]*Chain, 0, len(names))
	broken := []error{}
	for _, n := range names {
		c, err := LoadFile(filepath.Join(dir, n))
		if err == nil {
			err = NameMismatch(c)
		}
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
			c, err := LoadFile(p)
			if err != nil {
				return nil, err
			}
			if err := NameMismatch(c); err != nil {
				return nil, err
			}
			return c, nil
		}
	}
	for _, n := range Names(dir) {
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(dir, n+ext)
			if c, err := LoadFile(p); err == nil && c.Name == ref {
				return nil, NameMismatch(c)
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
}

func (e *NameMismatchError) Error() string {
	return fmt.Sprintf("%s declares name: %s, but a chain under paths.chains is found by its file name %s: its runs would be "+
		"stored as %s's while shrt verify %s compared them with %s's safe spot, so nothing is run or verified until the two agree: "+
		"rename the file to %s.yaml, or set name: %s (or drop name:)", e.Path, e.Name, e.Stem, e.Name, e.Stem, e.Stem, e.Name, e.Stem)
}

func NameMismatch(c *Chain) error {
	if c == nil || c.SourcePath == "" {
		return nil
	}
	stem := FileStem(c.SourcePath)
	if c.Name == stem {
		return nil
	}
	return &NameMismatchError{Path: c.SourcePath, Name: c.Name, Stem: stem}
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
	near := namecase.Closest(ref, names, 1)
	if len(near) == 0 {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", near[0])
}

func (c *Chain) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

func decodeStrict(raw []byte, into any) error {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return yamlkey.Explain(err, into)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err == nil && carriesContent(&extra) {
		return fmt.Errorf("this file holds more than one YAML document, and only the first is read — " +
			"everything after the '---' would be silently ignored. Split it into separate files")
	}
	return nil
}

func carriesContent(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	for _, c := range n.Content {
		if c.Tag != "!!null" {
			return true
		}
	}
	return false
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
