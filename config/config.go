package config

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

const (
	DirName    = ".shrt"
	FileName   = "config.yaml"
	TokensFile = "tokens.json"
	DocsDir    = DirName + "/docs"
)

type Config struct {
	Root string `yaml:"-"`

	Target      Target      `yaml:"target"`
	Descriptor  Descriptor  `yaml:"descriptor"`
	Auth        *Auth       `yaml:"auth,omitempty"`
	Paths       Paths       `yaml:"paths"`
	Conventions Conventions `yaml:"conventions,omitempty"`
	Volatile    []string    `yaml:"volatile,omitempty"`
	Redact      []string    `yaml:"redact,omitempty"`
}

type Target struct {
	BaseURL      string            `yaml:"base_url"`
	HostOverride string            `yaml:"host_override,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	Timeout      string            `yaml:"timeout,omitempty"`
	BuildHeader  string            `yaml:"build_header,omitempty"`
}

type Descriptor struct {
	File   string `yaml:"file"`
	Source string `yaml:"source,omitempty"`
	Binary string `yaml:"binary,omitempty"`
}

const DefaultAuthProfile = "default"

const InvalidTokenProfile = "invalid"

type Auth struct {
	Call        string           `yaml:"call"`
	Body        map[string]any   `yaml:"body"`
	TokenPath   string           `yaml:"token_path"`
	ExpiresPath string           `yaml:"expires_path,omitempty"`
	Header      string           `yaml:"header,omitempty"`
	Scheme      string           `yaml:"scheme,omitempty"`
	SkipCalls   []string         `yaml:"skip_calls,omitempty"`
	LeewaySecs  int              `yaml:"leeway_seconds,omitempty"`
	Calls       []string         `yaml:"calls,omitempty"`
	Profiles    map[string]*Auth `yaml:"profiles,omitempty"`
}

func (a *Auth) HeaderScheme() (string, string) {
	header, scheme := "Authorization", "Bearer"
	if a == nil {
		return header, scheme
	}
	if a.Header != "" {
		header = a.Header
	}
	if a.Scheme != "" {
		scheme = strings.TrimSpace(a.Scheme)
	}
	return header, scheme
}

func (a *Auth) inherit(parent *Auth) {
	if a == nil || parent == nil {
		return
	}
	if a.TokenPath == "" {
		a.TokenPath = parent.TokenPath
	}
	if a.ExpiresPath == "" {
		a.ExpiresPath = parent.ExpiresPath
	}
	if a.Header == "" {
		a.Header = parent.Header
	}
	if a.Scheme == "" {
		a.Scheme = parent.Scheme
	}
	if a.LeewaySecs == 0 {
		a.LeewaySecs = parent.LeewaySecs
	}
}

type Paths struct {
	Chains    string `yaml:"chains"`
	Contracts string `yaml:"contracts,omitempty"`
	Runs      string `yaml:"runs"`
	SafeSpots string `yaml:"safespots"`
}

type Conventions struct {
	ReadOnlyPrefixes []string `yaml:"read_only_prefixes,omitempty"`
	EnvelopePath     string   `yaml:"envelope_path,omitempty"`
	EnvelopeOK       string   `yaml:"envelope_ok,omitempty"`
	ItemEnvelopePath string   `yaml:"item_envelope_path,omitempty"`
	CodeFields       []string `yaml:"code_fields,omitempty"`
	ValidateOutput   bool     `yaml:"validate_output,omitempty"`
}

const ConventionsGuide = `no conventions: block declared, so shrt assumes the defaults. Declare only what
differs, under a top-level conventions: key in .shrt/config.yaml; every key is optional:
    read_only_prefixes: [Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve]
    envelope_path: error.code                     where a response states its own verdict; MOVE it, "" does not disable it
    envelope_ok: OK                               the value at that path meaning success
    item_envelope_path: results[].error.code      per-item verdict in a batch response; unset = none
    code_fields: [app_code, reason, error_code]   detail field names 'shrt chain which -code' searches
    validate_output: true                         a response the descriptor does not match FAILS the step instead of warning
`

func Default() *Config {
	return &Config{
		Target:     Target{BaseURL: "http://127.0.0.1:8080", Timeout: "30s"},
		Descriptor: Descriptor{File: DirName + "/descriptor.binpb", Source: ".", Binary: "buf"},
		Redact:     DefaultRedact(),
		Paths: Paths{
			Chains:    DirName + "/chains",
			Contracts: DirName + "/contracts",
			Runs:      DirName + "/runs",
			SafeSpots: DirName + "/safespots",
		},
	}
}

func (c *Config) NeverCommit() []string {
	runs := DirName + "/runs"
	descriptor := DirName + "/descriptor.binpb"
	if c != nil {
		if c.Paths.Runs != "" {
			runs = c.Paths.Runs
		}
		if c.Descriptor.File != "" {
			descriptor = c.Descriptor.File
		}
	}
	return []string{
		strings.TrimSuffix(runs, "/") + "/",
		descriptor,
		DocsDir + "/",
		DirName + "/" + TokensFile,
	}
}

func DefaultRedact() []string {
	return []string{
		"**.*password",
		"**.access_token",
		"**.refresh_token",
		"**.token",
		"**.*secret",
		"**.*pin",
		"**.*pin_code",
		"**.*passcode",
		"**.*otp",
		"**.api_key",
		"**.authorization",
	}
}

func Discover(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DirName, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s/%s found from %q upwards", DirName, FileName, start)
		}
		dir = parent
	}
}

func Load(start string) (*Config, error) {
	root, err := Discover(start)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, DirName, FileName))
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := decodeStrict(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.Root = root
	if err := cfg.normalizeAuth(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	dir := filepath.Join(c.Root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName), raw, 0o644)
}

func (c *Config) Abs(rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(c.Root, filepath.FromSlash(rel))
}

func (c *Config) AuthHeader() (string, string) {
	return c.Auth.HeaderScheme()
}

func (c *Config) AuthProfiles() map[string]*Auth {
	if c.Auth == nil {
		return nil
	}
	out := map[string]*Auth{DefaultAuthProfile: c.Auth}
	for name, p := range c.Auth.Profiles {
		if p == nil {
			continue
		}
		resolved := *p
		resolved.Profiles = nil
		resolved.inherit(c.Auth)
		out[name] = &resolved
	}
	return out
}

func (c *Config) AuthProfileNames() []string {
	names := make([]string, 0, len(c.AuthProfiles()))
	for name := range c.AuthProfiles() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) normalizeAuth() error {
	if c.Auth == nil {
		return nil
	}
	for name, p := range c.Auth.Profiles {
		if name == DefaultAuthProfile {
			return fmt.Errorf("auth.profiles: %q is the name of the top-level auth block, pick another", DefaultAuthProfile)
		}
		if name == InvalidTokenProfile {
			return fmt.Errorf("auth.profiles: %q is reserved: a step with auth: %s sends a token the backend "+
				"never issued, to probe that it is refused. Pick another name", InvalidTokenProfile, InvalidTokenProfile)
		}
		if p == nil {
			return fmt.Errorf("auth.profiles.%s: is empty", name)
		}
		if strings.TrimSpace(p.Call) == "" {
			return fmt.Errorf("auth.profiles.%s.call is required", name)
		}
		if len(p.Profiles) > 0 {
			return fmt.Errorf("auth.profiles.%s: profiles do not nest", name)
		}
	}
	return nil
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
