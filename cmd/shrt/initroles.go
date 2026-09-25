package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/config"
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
