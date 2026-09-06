package contract

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

const OverlayAPIVersion = "shrt/contract/v1"

const (
	StatusDraft    = "draft"
	StatusVerified = "verified"
)

const (
	RefSeparator       = "->"
	LegacyRefSeparator = "#"
)

const NoneLiteral = "NONE"

const RoleNone = NoneLiteral

const RequiredNone = NoneLiteral

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

func (c *RPCContract) DeclaresNothingRequired() bool {
	return len(c.Required) == 1 && strings.TrimSpace(c.Required[0]) == RequiredNone
}

func IsRequiredNone(name string) bool {
	return strings.TrimSpace(name) == RequiredNone
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

type Failure struct {
	Code        int    `yaml:"code,omitempty" json:"code,omitempty"`
	ConnectCode string `yaml:"connect_code,omitempty" json:"connect_code,omitempty"`
	Reason      string `yaml:"reason,omitempty" json:"reason,omitempty"`
	Message     string `yaml:"message,omitempty" json:"message,omitempty"`
	Field       string `yaml:"field,omitempty" json:"field,omitempty"`
	When        string `yaml:"when,omitempty" json:"when,omitempty"`
	Unreachable string `yaml:"unreachable,omitempty" json:"unreachable,omitempty"`

	PendingDeploy string `yaml:"pending_deploy,omitempty" json:"pending_deploy,omitempty"`
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

func (r Ref) String() string { return r.Node() + RefSeparator + r.Path }

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
	for _, name := range sortedFieldNames(c.Fields) {
		if ref, err := ParseRef(c.Fields[name].From); err == nil {
			add(ref.Node())
		}
		if ref, err := ParseRef(c.Fields[name].SameAs); err == nil {
			add(ref.Node())
		}
	}
	for _, alias := range sortedAliasNames(c.Aliases) {
		for _, name := range sortedFieldNames(c.Aliases[alias].Fields) {
			if ref, err := ParseRef(c.Aliases[alias].Fields[name].From); err == nil {
				add(ref.Node())
			}
			if ref, err := ParseRef(c.Aliases[alias].Fields[name].SameAs); err == nil {
				add(ref.Node())
			}
		}
	}
	return out
}

func (c *RPCContract) DependenciesFor(alias string) []string {
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
	fields := c.FieldsFor(alias)
	for _, name := range sortedFieldNames(fields) {
		if ref, err := ParseRef(fields[name].From); err == nil {
			add(ref.Node())
		}
		if ref, err := ParseRef(fields[name].SameAs); err == nil {
			add(ref.Node())
		}
	}
	return out
}

func (c *RPCContract) FieldsFor(alias string) map[string]*FieldContract {
	merged := map[string]*FieldContract{}
	for name, f := range c.Fields {
		copied := *f
		merged[name] = &copied
	}
	if alias == "" {
		return merged
	}
	override, ok := c.Aliases[alias]
	if !ok {
		return merged
	}
	for name, f := range override.Fields {
		copied := *f
		merged[name] = &copied
	}
	return merged
}

func sortedFieldNames(m map[string]*FieldContract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedAliasNames(m map[string]*AliasContract) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	return o, nil
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

func LoadLibrary(dir string) (*Library, []error, error) {
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
	for _, n := range names {
		o, err := LoadOverlay(filepath.Join(dir, n))
		if err != nil {
			broken = append(broken, err)
			continue
		}
		overlays = append(overlays, o)
	}
	return NewLibrary(overlays), broken, nil
}

func NewLibrary(overlays []*Overlay) *Library {
	lib := &Library{
		Overlays:  overlays,
		byRPC:     map[string]*RPCContract{},
		domainOf:  map[string]string{},
		inherited: map[string][]Failure{},
		before:    map[string][]string{},
	}
	for _, o := range overlays {
		for rpc, c := range o.RPCs {
			lib.byRPC[rpc] = c
			lib.domainOf[rpc] = o.Domain
			if len(o.Failures) > 0 {
				lib.inherited[rpc] = o.Failures
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
	out := make([]string, 0, len(l.byRPC))
	for rpc := range l.byRPC {
		out = append(out, rpc)
	}
	sort.Strings(out)
	return out
}

func (o *Overlay) Marshal() ([]byte, error) { return yaml.Marshal(o) }

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
