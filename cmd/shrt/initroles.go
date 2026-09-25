package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/config"
	"gopkg.in/yaml.v3"
)

type credentialSet struct {
	prefix   string
	user     string
	password string
}

func envCredentialSets(environ []string) []credentialSet {
	set := map[string]bool{}
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if value != "" {
			set[name] = true
		}
	}
	out := []credentialSet{}
	for name := range set {
		for _, suffix := range []string{"_USER", "_USERNAME"} {
			prefix, ok := strings.CutSuffix(name, suffix)
			if !ok || prefix == "" || prefix == "API" {
				continue
			}
			for _, pw := range []string{"_PASSWORD", "_PASS"} {
				if set[prefix+pw] {
					out = append(out, credentialSet{prefix: prefix, user: name, password: prefix + pw})
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix < out[j].prefix })
	return out
}

var tableRow = regexp.MustCompile(`^\s*\|(.*)\|\s*$`)

func readmeAccounts(readme string) []string {
	out := []string{}
	col := -1
	for _, line := range strings.Split(readme, "\n") {
		m := tableRow.FindStringSubmatch(line)
		if m == nil {
			col = -1
			continue
		}
		cells := strings.Split(m[1], "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(strings.Trim(strings.TrimSpace(cells[i]), "`"))
		}
		if col < 0 {
			for i, c := range cells {
				switch strings.ToLower(c) {
				case "username", "user", "account", "login", "user name":
					col = i
				}
			}
			continue
		}
		if col >= len(cells) || strings.Trim(cells[col], "-: ") == "" {
			continue
		}
		out = append(out, cells[col])
	}
	return out
}

func wordIn(text, word string) bool {
	if word == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9])`)
	return re.MatchString(text)
}

func rootReadme(root string) string {
	for _, name := range []string{"README.md", "README", "readme.md", "README.rst"} {
		if raw, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			return string(raw)
		}
	}
	return ""
}

func addRoleProfiles(cfg *config.Config, best catalog.LoginCandidate, environ []string) []string {
	readme := rootReadme(cfg.Root)
	added := []string{}
	for _, cs := range envCredentialSets(environ) {
		name := strings.ToLower(cs.prefix)
		if !wordIn(readme, name) {
			continue
		}
		if _, exists := cfg.Auth.Profiles[name]; exists {
			continue
		}
		body := map[string]any{}
		if best.UserField != "" {
			body[best.UserField] = "${env." + cs.user + "}"
		}
		if best.PasswordName != "" {
			body[best.PasswordName] = "${env." + cs.password + "}"
		}
		if cfg.Auth.Profiles == nil {
			cfg.Auth.Profiles = map[string]*config.Auth{}
		}
		cfg.Auth.Profiles[name] = &config.Auth{Call: best.Method.FullName, Body: body, TokenPath: best.TokenPath, ExpiresPath: best.ExpiresPath}
		added = append(added, fmt.Sprintf("%s (%s and %s are set, and README names %s)", name, cs.user, cs.password, name))
	}
	return added
}

func roleProfileHint(cfg *config.Config) string {
	accounts := readmeAccounts(rootReadme(cfg.Root))
	missing := []string{}
	for _, a := range accounts {
		name := strings.ToLower(a)
		if _, has := cfg.Auth.Profiles[name]; has || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(name) {
			continue
		}
		missing = append(missing, name)
	}
	example := "<role>"
	if len(missing) > 0 {
		example = missing[len(missing)-1]
	}
	envName := strings.ToUpper(example)
	if example == "<role>" {
		envName = "<ROLE>"
	}
	lead := "      the default login reads ${env.API_USER}; a backend with more than one role needs one profile per role."
	switch {
	case len(missing) >= 2 || (len(missing) == 1 && len(cfg.Auth.Profiles) == 0 && len(accounts) > 1):
		lead = fmt.Sprintf("      README lists accounts %s; the default login reads ${env.API_USER}, so it is one of them.", strings.Join(accounts, ", "))
	case len(cfg.Auth.Profiles) > 0 || len(accounts) > 0:
		return ""
	}
	return lead + "\n" + fmt.Sprintf("      For each other role add auth.profiles.%s: the same call, token_path and expires_path, a body reading\n"+
		"      ${env.%s_USER} and ${env.%s_PASSWORD}, and put auth: %s on the steps that act as it (GRAMMAR.md §4).\n"+
		"      init writes such a profile itself when %s_USER and %s_PASSWORD are exported and the README names %s",
		example, envName, envName, example, envName, envName, example)
}

func addMissingRoleProfiles(cfg *config.Config, cfgPath string) ([]string, error) {
	if cfg.Auth == nil {
		return nil, nil
	}
	cat, err := catalog.Load(cfg.Abs(cfg.Descriptor.File))
	if err != nil {
		return nil, nil
	}
	found := catalog.DetectLogins(cat)
	var best *catalog.LoginCandidate
	for i := range found {
		if found[i].Method.FullName == cfg.Auth.Call {
			best = &found[i]
			break
		}
	}
	if best == nil {
		return nil, nil
	}
	probe := &config.Config{Root: cfg.Root, Auth: &config.Auth{Profiles: map[string]*config.Auth{}}}
	for name, p := range cfg.Auth.Profiles {
		probe.Auth.Profiles[name] = p
	}
	added := addRoleProfiles(probe, *best, os.Environ())
	if len(added) == 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, err
	}
	auth := mappingValue(doc.Content[0], "auth")
	if auth == nil || auth.Kind != yaml.MappingNode {
		return nil, nil
	}
	profiles := mappingValue(auth, "profiles")
	if profiles == nil {
		profiles = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		auth.Content = append(auth.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "profiles"}, profiles)
	}
	names := []string{}
	for name := range probe.Auth.Profiles {
		if _, had := cfg.Auth.Profiles[name]; !had {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var value yaml.Node
		if err := value.Encode(probe.Auth.Profiles[name]); err != nil {
			return nil, err
		}
		profiles.Content = append(profiles.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &value)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cfgPath, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	if cfg.Auth.Profiles == nil {
		cfg.Auth.Profiles = map[string]*config.Auth{}
	}
	for _, name := range names {
		cfg.Auth.Profiles[name] = probe.Auth.Profiles[name]
	}
	return added, nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
