package config

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/N4darae/shrt/yamlkey"
	"gopkg.in/yaml.v3"
)

const (
	DirName    = ".shrt"
	FileName   = "config.yaml"
	TokensFile = "tokens.json"
	DocsDir    = DirName + "/docs"
	ScratchDir = DirName + "/scratch/"
)

type Config struct {
	Root string `yaml:"-"`

	Target      Target      `yaml:"target"`
	Descriptor  Descriptor  `yaml:"descriptor"`
	Auth        *Auth       `yaml:"auth,omitempty"`
	Paths       Paths       `yaml:"paths"`
	Conventions Conventions `yaml:"conventions,omitempty"`
	Latency     *Latency    `yaml:"latency,omitempty"`
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
	if a == nil {
		return "Authorization", "Bearer"
	}
	scheme := "Bearer"
	if a.Scheme != "" {
		scheme = strings.TrimSpace(a.Scheme)
	}
	return cmp.Or(a.Header, "Authorization"), scheme
}

func (a *Auth) inherit(parent *Auth) {
	if a == nil || parent == nil {
		return
	}
	a.TokenPath = cmp.Or(a.TokenPath, parent.TokenPath)
	a.ExpiresPath = cmp.Or(a.ExpiresPath, parent.ExpiresPath)
	a.Header = cmp.Or(a.Header, parent.Header)
	a.Scheme = cmp.Or(a.Scheme, parent.Scheme)
	a.LeewaySecs = cmp.Or(a.LeewaySecs, parent.LeewaySecs)
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

var ConventionsGuide = ConventionsGuideFor("")

func ConventionsGuideFor(envelopePath string) string {
	envelopePath = cmp.Or(strings.TrimSpace(envelopePath), "error.code")
	row := func(setting, why string) string {
		return "    " + setting + strings.Repeat(" ", max(46-len(setting), 2)) + why + "\n"
	}
	okRow := row("envelope_ok: OK", "the value at that path meaning success")
	head := "no conventions: block declared, so shrt assumes the defaults. Declare only what\n" +
		"differs, under a top-level conventions: key in .shrt/config.yaml; every key is optional:\n"
	if envelopePath != "error.code" {
		head = "no conventions: block declared, so shrt assumes its default envelope, which this backend's\n" +
			"responses do not carry: they carry " + envelopePath + ", so the block below uses it. Paste what applies\n" +
			"under a top-level conventions: key in .shrt/config.yaml, with envelope_ok set to its success value:\n"
		okRow = row("envelope_ok: <success value>", "the value at "+envelopePath+" meaning success; shrt cannot guess it")
	}
	return head +
		"    read_only_prefixes: [Fetch, Get, List, Preview, Search, Read, Query, Find, Lookup, Describe, Show, Count, Export, Download, Retrieve]\n" +
		row("envelope_path: "+envelopePath, `where a response states its own verdict; MOVE it, "" does not disable it`) +
		okRow +
		row("item_envelope_path: results[]."+envelopePath, "per-item verdict in a batch response; unset = none") +
		row("code_fields: [app_code, reason, error_code]", "detail field names 'shrt chain which -code' searches") +
		row("validate_output: true", "a response the descriptor does not match FAILS the step instead of warning")
}

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
	var cfg Config
	if c != nil {
		cfg = *c
	}
	return []string{
		strings.TrimSuffix(cmp.Or(cfg.Paths.Runs, DirName+"/runs"), "/") + "/",
		cmp.Or(cfg.Descriptor.File, DirName+"/descriptor.binpb"),
		DocsDir + "/",
		DirName + "/" + TokensFile,
		strings.TrimSuffix(cmp.Or(cfg.Paths.SafeSpots, DirName+"/safespots"), "/") + "/pending/",
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
	if err := yamlkey.DecodeStrict(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.Root = root
	if err := cfg.normalizeAuth(); err != nil {
		return nil, err
	}
	if err := cfg.Latency.validate(); err != nil {
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

func (c *Config) HandWrittenAuthHeaders() []string {
	if c == nil || c.Auth == nil {
		return nil
	}
	auth := map[string]bool{"authorization": true}
	for _, p := range c.AuthProfiles() {
		header, _ := p.HeaderScheme()
		auth[strings.ToLower(strings.TrimSpace(header))] = true
	}
	out := []string{}
	for name := range c.Target.Headers {
		if auth[strings.ToLower(strings.TrimSpace(name))] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func HandWrittenAuthProblem(names []string) string {
	return fmt.Sprintf("target.headers writes %s by hand while the config declares auth: it would be sent on every "+
		"call no profile covers (a login, skip_auth, auth.skip_calls) while the run record says auth_profile none, "+
		"and overwritten on every call a profile covers. Remove it from target.headers and let an auth profile "+
		"carry the principal", strings.Join(names, ", "))
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
