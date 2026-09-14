package catalog

import (
	"github.com/N4darae/shrt/namecase"
	"sort"
	"strings"
)

type LoginCandidate struct {
	Method       *Method
	UserField    string
	PasswordName string
	TokenPath    string
	ExpiresPath  string
	Score        int
}

var loginVerbs = []string{"login", "signin", "logon", "authenticate", "issuetoken", "createtoken", "gettoken"}

var userFieldNames = []string{"username", "email", "user", "login", "user_name", "account", "identity", "identifier", "operator", "operator_name", "principal", "actor", "handle"}

var passwordFieldNames = []string{"password", "secret", "passphrase", "pass_phrase", "credential", "pass"}

var tokenFieldNames = []string{"access_token", "id_token", "token", "jwt", "session_token", "bearer"}

var expiresFieldNames = []string{"expires_at", "expires_in", "expiry", "expire_at", "valid_until", "good_until", "not_after"}

func DetectLogins(cat *Catalog) []LoginCandidate {
	if cat == nil {
		return nil
	}
	out := []LoginCandidate{}
	for _, m := range cat.Methods() {
		if m.Streaming() {
			continue
		}
		c := scoreLogin(m)
		if c.Score <= 0 {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Method.FullName < out[j].Method.FullName
	})
	return out
}

func scoreLogin(m *Method) LoginCandidate {
	c := LoginCandidate{Method: m}
	lower := strings.ToLower(m.Name)
	named := false
	for _, verb := range loginVerbs {
		if strings.Contains(strings.ReplaceAll(lower, "_", ""), verb) {
			named = true
			break
		}
	}

	in := DescribeMessage(m.Input()).Fields
	c.UserField = firstMatch(in, userFieldNames)
	c.PasswordName = firstMatch(in, passwordFieldNames)
	if c.UserField == "" && c.PasswordName != "" {
		c.UserField = firstStringFieldExcept(in, c.PasswordName)
	}

	out := DescribeMessage(m.Output()).Fields
	c.TokenPath = firstMatch(out, tokenFieldNames)
	c.ExpiresPath = firstMatch(out, expiresFieldNames)

	if c.TokenPath == "" {
		return LoginCandidate{}
	}
	if named {
		c.Score += 4
	}
	if c.UserField != "" {
		c.Score++
	}
	if c.PasswordName != "" {
		c.Score += 2
	}
	if c.ExpiresPath != "" {
		c.Score++
	}
	if c.PasswordName == "" && !named {
		return LoginCandidate{}
	}
	return c
}

func firstMatch(fields []*Field, wanted []string) string {
	byName := map[string]string{}
	for _, f := range fields {
		byName[strings.ToLower(f.Name)] = f.Name
		byName[namecase.Fold(f.Name)] = f.Name
	}
	for _, w := range wanted {
		if real, ok := byName[w]; ok {
			return real
		}
		if real, ok := byName[namecase.Fold(w)]; ok {
			return real
		}
	}
	for _, f := range fields {
		name := strings.ToLower(f.Name)
		for _, w := range wanted {
			if strings.Contains(name, w) {
				return f.Name
			}
		}
	}
	return ""
}

func firstStringFieldExcept(fields []*Field, except string) string {
	for _, f := range fields {
		if f.Name == except || f.Kind != "string" || f.Repeated {
			continue
		}
		return f.Name
	}
	return ""
}
