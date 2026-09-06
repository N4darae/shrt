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
	return nil, fmt.Errorf("chain %q not found in %s", ref, dir)
}

func (c *Chain) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

func decodeStrict(raw []byte, into any) error {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return err
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
