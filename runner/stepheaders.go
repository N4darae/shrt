package runner

import (
	"net/textproto"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

var secretHeaderWords = map[string]bool{
	"auth": true, "authorization": true, "authentication": true, "token": true, "secret": true, "key": true,
	"apikey": true, "password": true, "passwd": true, "pass": true, "cookie": true, "session": true,
	"signature": true, "sig": true, "credential": true, "credentials": true, "otp": true, "pin": true,
	"jwt": true, "bearer": true, "csrf": true, "xsrf": true,
}

func secretHeader(name string) bool {
	lower := strings.ToLower(name)
	for _, hint := range []string{"token", "secret", "password", "auth", "cookie", "apikey"} {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		if secretHeaderWords[word] {
			return true
		}
	}
	return false
}

func recordedHeaders(templates map[string]string, resolved map[string][]string) map[string]string {
	out := map[string]string{}
	for name, template := range templates {
		key := textproto.CanonicalMIMEHeaderKey(name)
		switch {
		case secretHeader(name):
			out[key] = pathmask.MaskRedacted
		case readsOnlyVars(template):
			out[key] = strings.Join(resolved[name], ", ")
		default:
			out[key] = chain.CanonicalRefs(template)
		}
	}
	return out
}

func readsOnlyVars(template string) bool {
	for _, m := range bodyRef.FindAllStringSubmatch(template, -1) {
		if chain.ParseRef(m[1]).Kind != chain.RefVars {
			return false
		}
	}
	return true
}
