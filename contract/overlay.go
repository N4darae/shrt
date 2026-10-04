package contract

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/yamlkey"
	"gopkg.in/yaml.v3"
)

const OverlayAPIVersion = "shrt/contract/v1"

const (
	StatusDraft    = "draft"
	StatusVerified = "verified"
)

const (
	RefSeparator       = "->"
	LegacyRefSeparator = "#"
)

const (
	RoleNone     = "NONE"
	RequiredNone = "NONE"
)

const RequiredUnknown = "UNKNOWN"

const (
	CheckedByFK        = "fk"
	CheckedByAppLookup = "app_lookup"
	CheckedByNone      = "none"
)

type Overlay struct {
	APIVersion  string                  `yaml:"apiVersion" json:"apiVersion"`
	Domain      string                  `yaml:"domain" json:"domain"`
	Description string                  `yaml:"description,omitempty" json:"description,omitempty"`
	Failures    []Failure               `yaml:"failures,omitempty" json:"failures,omitempty"`
	RPCs        map[string]*RPCContract `yaml:"rpcs" json:"rpcs"`

	SourcePath   string   `yaml:"-" json:"-"`
	EmptyEntries []string `yaml:"-" json:"-"`
}

type RPCContract struct {
	Summary      string                    `yaml:"summary,omitempty" json:"summary,omitempty"`
	Note         string                    `yaml:"note,omitempty" json:"note,omitempty"`
	Auth         string                    `yaml:"auth,omitempty" json:"auth,omitempty"`
	RequiresRole []string                  `yaml:"requires_role,omitempty" json:"requires_role,omitempty"`
	Required     []string                  `yaml:"required" json:"required"`
	Needs        []string                  `yaml:"needs,omitempty" json:"needs,omitempty"`
	NoProducer   string                    `yaml:"no_producer,omitempty" json:"no_producer,omitempty"`
	Before       []string                  `yaml:"before,omitempty" json:"before,omitempty"`
	Fields       map[string]*FieldContract `yaml:"fields,omitempty" json:"fields,omitempty"`
	Aliases      map[string]*AliasContract `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Effects      Effects                   `yaml:"effects,omitempty" json:"effects,omitempty"`
	Exports      map[string]string         `yaml:"exports,omitempty" json:"exports,omitempty"`
	Terminal     map[string]string         `yaml:"terminal,omitempty" json:"terminal,omitempty"`
	SoftSignals  map[string]string         `yaml:"soft_signals,omitempty" json:"soft_signals,omitempty"`
	Failures     []Failure                 `yaml:"failures,omitempty" json:"failures,omitempty"`
	Source       []string                  `yaml:"source,omitempty" json:"source,omitempty"`
	Status       string                    `yaml:"status" json:"status"`
	VerifiedBy   string                    `yaml:"verified_by,omitempty" json:"verified_by,omitempty"`
	VerifiedRun  string                    `yaml:"verified_run,omitempty" json:"verified_run,omitempty"`

	Unfilled map[string]bool `yaml:"-" json:"-"`
}

func (c *RPCContract) IsUnfilled(key string) bool { return c.Unfilled[key] }

func (c *RPCContract) DeclaresNoRole() bool {
	return len(c.RequiresRole) == 1 && strings.TrimSpace(c.RequiresRole[0]) == RoleNone
}

func IsRequiredLiteral(name string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed == RequiredNone || trimmed == RequiredUnknown
}

type AliasContract struct {
	Note   string                    `yaml:"note,omitempty" json:"note,omitempty"`
	Fields map[string]*FieldContract `yaml:"fields,omitempty" json:"fields,omitempty"`
}

type FieldContract struct {
	From      string `yaml:"from,omitempty" json:"from,omitempty"`
	Value     string `yaml:"value,omitempty" json:"value,omitempty"`
	SameAs    string `yaml:"same_as,omitempty" json:"same_as,omitempty"`
	OneOf     string `yaml:"oneof,omitempty" json:"oneof,omitempty"`
	CheckedBy string `yaml:"checked_by,omitempty" json:"checked_by,omitempty"`
	Note      string `yaml:"note,omitempty" json:"note,omitempty"`
}

const FailureScopeAll = "all"

type Failure struct {
	Code        int    `yaml:"code,omitempty" json:"code,omitempty"`
	ConnectCode string `yaml:"connect_code,omitempty" json:"connect_code,omitempty"`
	Reason      string `yaml:"reason,omitempty" json:"reason,omitempty"`
	Message     string `yaml:"message,omitempty" json:"message,omitempty"`
	Field       string `yaml:"field,omitempty" json:"field,omitempty"`
	When        string `yaml:"when,omitempty" json:"when,omitempty"`
	Unreachable string `yaml:"unreachable,omitempty" json:"unreachable,omitempty"`
	Scope       string `yaml:"scope,omitempty" json:"scope,omitempty"`

	PendingDeploy string `yaml:"pending_deploy,omitempty" json:"pending_deploy,omitempty"`

	Unique *UniqueCompare `yaml:"unique,omitempty" json:"unique,omitempty"`
}

type UniqueCompare struct {
	Case string `yaml:"case,omitempty" json:"case,omitempty"`
	Trim *bool  `yaml:"trim,omitempty" json:"trim,omitempty"`
}

func (f Failure) Label() string {
	switch {
	case f.Code != 0 && f.Reason != "":
		return fmt.Sprintf("%d %s", f.Code, f.Reason)
	case f.Code != 0:
		return fmt.Sprintf("%d", f.Code)
	case f.Reason != "":
		return f.Reason
	case f.ConnectCode != "":
		return f.ConnectCode
	default:
		return "(unnamed)"
	}
}

type Ref struct {
	RPC   string
	Alias string
	Path  string
}

func ParseRef(raw string) (Ref, error) {
	trimmed := strings.TrimSpace(raw)
	head, path, ok := cutRef(trimmed)
	if !ok || strings.TrimSpace(head) == "" || strings.TrimSpace(path) == "" {
		return Ref{}, fmt.Errorf("expected <rpc>[@alias]%s<response_path>, got %q", RefSeparator, raw)
	}
	rpc, alias, _ := strings.Cut(strings.TrimSpace(head), "@")
	if strings.TrimSpace(rpc) == "" {
		return Ref{}, fmt.Errorf("expected <rpc>[@alias]%s<response_path>, got %q", RefSeparator, raw)
	}
	return Ref{
		RPC:   strings.TrimSpace(rpc),
		Alias: strings.TrimSpace(alias),
		Path:  strings.TrimSpace(path),
	}, nil
}

func cutRef(raw string) (head, path string, ok bool) {
	if head, path, ok = strings.Cut(raw, RefSeparator); ok {
		return head, path, true
	}
	return strings.Cut(raw, LegacyRefSeparator)
}

func UsesLegacySeparator(raw string) bool {
	return !strings.Contains(raw, RefSeparator) && strings.Contains(raw, LegacyRefSeparator)
}

func (r Ref) Node() string {
	if r.Alias == "" {
		return r.RPC
	}
	return r.RPC + "@" + r.Alias
}

func SplitNode(node string) (rpc, alias string) {
	rpc, alias, _ = strings.Cut(strings.TrimSpace(node), "@")
	return strings.TrimSpace(rpc), strings.TrimSpace(alias)
}

func (c *RPCContract) Dependencies() []string {
	sets := []map[string]*FieldContract{c.Fields}
	for _, alias := range sortedKeys(c.Aliases) {
		sets = append(sets, c.Aliases[alias].Fields)
	}
	return c.dependsOn(sets...)
}

func (c *RPCContract) DependenciesFor(alias string) []string { return c.dependsOn(c.FieldsFor(alias)) }

func (c *RPCContract) dependsOn(sets ...map[string]*FieldContract) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(node string) {
		node = strings.TrimSpace(node)
		if node == "" || seen[node] {
			return
		}
		seen[node] = true
		out = append(out, node)
	}
	for _, n := range c.Needs {
		add(n)
	}
	for _, fields := range sets {
		for _, name := range sortedKeys(fields) {
			for _, raw := range []string{fields[name].From, fields[name].SameAs} {
				if ref, err := ParseRef(raw); err == nil {
					add(ref.Node())
				}
			}
		}
	}
	return out
}

func (c *RPCContract) FieldsFor(alias string) map[string]*FieldContract {
	sets := []map[string]*FieldContract{c.Fields}
	if override, ok := c.Aliases[alias]; alias != "" && ok {
		sets = append(sets, override.Fields)
	}
	merged := map[string]*FieldContract{}
	for _, fields := range sets {
		for name, f := range fields {
			copied := *f
			merged[name] = &copied
		}
	}
	return merged
}

func LoadOverlay(path string) (*Overlay, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadOverlayBytes(path, raw)
}

func LoadOverlayBytes(path string, raw []byte) (*Overlay, error) {
	o := &Overlay{}
	if err := decodeStrict(raw, o); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if o.APIVersion == "" {
		o.APIVersion = OverlayAPIVersion
	}
	if o.APIVersion != OverlayAPIVersion {
		return nil, fmt.Errorf("%s: unsupported apiVersion %q, want %q", path, o.APIVersion, OverlayAPIVersion)
	}
	if o.Domain == "" {
		o.Domain = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	o.SourcePath = path
	o.EmptyEntries = normalizeEmptyEntries(o)
	markUnfilled(o, raw)
	dropTodoRequired(o)
	return o, nil
}

func dropTodoRequired(o *Overlay) {
	for _, c := range o.RPCs {
		if c == nil || len(c.Required) == 0 {
			continue
		}
		kept := make([]string, 0, len(c.Required))
		for _, name := range c.Required {
			if IsTodo(name) {
				continue
			}
			kept = append(kept, name)
		}
		if len(kept) == len(c.Required) {
			continue
		}
		c.Required = kept
		if c.Unfilled == nil {
			c.Unfilled = map[string]bool{}
		}
		c.Unfilled["required"] = true
	}
}

func normalizeEmptyEntries(o *Overlay) []string {
	empty := []string{}
	for rpc, c := range o.RPCs {
		if c == nil {
			o.RPCs[rpc] = &RPCContract{}
			empty = append(empty, rpc)
			continue
		}
		for name, f := range c.Fields {
			if f == nil {
				c.Fields[name] = &FieldContract{}
				empty = append(empty, rpc+" fields."+name)
			}
		}
		for alias, a := range c.Aliases {
			if a == nil {
				c.Aliases[alias] = &AliasContract{}
				empty = append(empty, rpc+" aliases."+alias)
				continue
			}
			for name, f := range a.Fields {
				if f == nil {
					a.Fields[name] = &FieldContract{}
					empty = append(empty, rpc+" aliases."+alias+".fields."+name)
				}
			}
		}
	}
	sort.Strings(empty)
	return empty
}

func markUnfilled(o *Overlay, raw []byte) {
	for _, issue := range ScanTodos(o.Domain, raw) {
		c, ok := o.RPCs[issue.RPC]
		if !ok || c == nil {
			continue
		}
		if c.Unfilled == nil {
			c.Unfilled = map[string]bool{}
		}
		c.Unfilled[issue.Field] = true
	}
}

type Library struct {
	Overlays []*Overlay

	byRPC     map[string]*RPCContract
	domainOf  map[string]string
	inherited map[string][]Failure
	before    map[string][]string
}

func LoadLibraryIn(dir string, cat *catalog.Catalog) (*Library, []error, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return NewLibrary(nil), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	names := []string{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	overlays := []*Overlay{}
	broken := []error{}
	definedIn := map[string]definedAt{}
	for _, n := range names {
		o, err := LoadOverlay(filepath.Join(dir, n))
		if err != nil {
			broken = append(broken, err)
			continue
		}
		clash := false
		for _, rpc := range sortedKeys(o.RPCs) {
			key := rpc
			if cat != nil {
				if m, err := cat.Lookup(rpc); err == nil {
					key = m.FullName
				}
			}
			if first, seen := definedIn[key]; seen {
				named := rpc
				if first.key != rpc || key != rpc {
					named = fmt.Sprintf("%s (written %q there and %q here)", key, first.key, rpc)
				}
				broken = append(broken, fmt.Errorf("%s and %s both define %s: an rpc has one contract, and "+
					"whichever file loads last would silently replace the other — keep the entry in one file",
					first.path, o.SourcePath, named))
				clash = true
				continue
			}
			definedIn[key] = definedAt{path: o.SourcePath, key: rpc}
		}
		if clash {
			continue
		}
		overlays = append(overlays, o)
	}
	lib := NewLibrary(overlays)
	kept := []*Overlay{}
	for _, o := range overlays {
		problems := []string{}
		for _, issue := range EffectProblems(&Library{Overlays: []*Overlay{o}, byRPC: lib.byRPC}, cat) {
			problems = append(problems, fmt.Sprintf("%s %s: %s", shortRPC(issue.RPC), issue.Field, issue.Message))
		}
		if len(problems) > 0 {
			broken = append(broken, fmt.Errorf("%s: %s", o.SourcePath, strings.Join(problems, "; ")))
			continue
		}
		kept = append(kept, o)
	}
	if len(kept) == len(overlays) {
		return lib, broken, nil
	}
	return NewLibrary(kept), broken, nil
}

type definedAt struct {
	path string
	key  string
}

func IgnoredOverlayFiles(dir string) ([]string, error) {
	out := []string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || filepath.Dir(path) == filepath.Clean(dir) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func NewLibrary(overlays []*Overlay) *Library {
	lib := &Library{
		Overlays:  overlays,
		byRPC:     map[string]*RPCContract{},
		domainOf:  map[string]string{},
		inherited: map[string][]Failure{},
		before:    map[string][]string{},
	}
	shared := map[*Overlay][]Failure{}
	for _, o := range overlays {
		for _, f := range o.Failures {
			if f.Scope == FailureScopeAll {
				shared[o] = append(shared[o], f)
			}
		}
	}
	for _, o := range overlays {
		for rpc, c := range o.RPCs {
			lib.byRPC[rpc] = c
			lib.domainOf[rpc] = o.Domain
			if len(o.Failures) > 0 {
				lib.inherited[rpc] = append([]Failure{}, o.Failures...)
			}
			for _, other := range overlays {
				if other != o {
					lib.inherited[rpc] = append(lib.inherited[rpc], shared[other]...)
				}
			}
			if len(lib.inherited[rpc]) == 0 {
				delete(lib.inherited, rpc)
			}
			for _, target := range c.Before {
				rpcOnly, _ := SplitNode(target)
				lib.before[rpcOnly] = append(lib.before[rpcOnly], rpc)
			}
		}
	}
	for k := range lib.before {
		sort.Strings(lib.before[k])
	}
	return lib
}

func (l *Library) Get(rpc string) (*RPCContract, bool) {
	if l == nil {
		return nil, false
	}
	c, ok := l.byRPC[rpc]
	return c, ok
}

func (l *Library) Domain(rpc string) string {
	if l == nil {
		return ""
	}
	return l.domainOf[rpc]
}

func (l *Library) DescriptionOf(domain string) string {
	for _, o := range l.Overlays {
		if o.Domain == domain {
			return o.Description
		}
	}
	return ""
}

func (l *Library) InheritedFailures(rpc string) []Failure {
	if l == nil {
		return nil
	}
	return l.inherited[rpc]
}

func (l *Library) AllFailures(rpc string) []Failure {
	out := append([]Failure{}, l.inherited[rpc]...)
	if c, ok := l.byRPC[rpc]; ok {
		out = append(out, c.Failures...)
	}
	return out
}

func (l *Library) RequiredBy(rpc string) []string {
	if l == nil {
		return nil
	}
	return l.before[rpc]
}

func (l *Library) Count() int {
	if l == nil {
		return 0
	}
	return len(l.byRPC)
}

func (l *Library) RPCs() []string {
	return sortedKeys(l.byRPC)
}

func (o *Overlay) Marshal() ([]byte, error) { return yaml.Marshal(o) }

func decodeStrict(raw []byte, into any) error {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return yamlkey.Explain(err, into, raw)
	}
	var extra yaml.Node
	err := d.Decode(&extra)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("this file holds more than one YAML document, and the one after the '---' does not "+
			"parse (%v); only the first is read, so the rest would be silently ignored. Split it into separate "+
			"files, or remove the '---'", err)
	}
	if err == nil && carriesContent(&extra) {
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
