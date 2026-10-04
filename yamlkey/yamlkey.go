package yamlkey

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/namecase"
	"gopkg.in/yaml.v3"
)

var unknownField = regexp.MustCompile(`^line (\d+): field (.+) not found in type (\S+)$`)

var flowError = regexp.MustCompile(`^yaml: line (\d+): did not find expected ',' or '[}\]]'$`)

func DecodeStrict(raw []byte, into any) error {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return Explain(err, into, raw)
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err == nil && slices.ContainsFunc(extra.Content, func(c *yaml.Node) bool { return c.Tag != "!!null" }) {
		return fmt.Errorf("this file holds more than one YAML document, and only the first is read — " +
			"everything after the '---' would be silently ignored. Split it into separate files")
	}
	return nil
}

func MappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	if i := Index(n, key); i >= 0 {
		return n.Content[i+1]
	}
	return nil
}

func Index(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func Set(mapping *yaml.Node, key string, value *yaml.Node) {
	if i := Index(mapping, key); i >= 0 {
		mapping.Content[i+1] = value
		return
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func Explain(err error, into any, raw []byte) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return quoteHint(err, raw)
	}
	keys := map[string][]string{}
	collect(reflect.TypeOf(into), keys, map[reflect.Type]bool{})
	said := make([]string, 0, len(typeErr.Errors))
	for _, e := range typeErr.Errors {
		m := unknownField.FindStringSubmatch(e)
		if m == nil {
			said = append(said, e)
			continue
		}
		said = append(said, fmt.Sprintf("unknown key %q at line %s", m[2], m[1])+namecase.Suggest(namecase.Closest(m[2], keys[m[3]], 1)))
	}
	return errors.New(strings.Join(said, "; "))
}

func quoteHint(err error, raw []byte) error {
	m := flowError.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	n, _ := strconv.Atoi(m[1])
	for i := n; i < len(lines); i++ {
		if strings.Contains(lines[i], "${") {
			return fmt.Errorf("%w (line %d: quote a ${...} inside {} or []: \"${...}\")", err, i+1)
		}
		if i >= n && strings.ContainsAny(lines[i], "}]") {
			break
		}
	}
	return err
}

func collect(t reflect.Type, keys map[string][]string, seen map[reflect.Type]bool) {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map) {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	keys[t.String()] = fieldKeys(t, keys, seen)
}

func fieldKeys(t reflect.Type, keys map[string][]string, seen map[reflect.Type]bool) []string {
	out := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		collect(f.Type, keys, seen)
		if strings.Contains(opts, "inline") {
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				out = append(out, fieldKeys(inner, keys, seen)...)
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out = append(out, name)
	}
	return out
}
