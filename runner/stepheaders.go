package runner

import (
	"crypto/sha256"
	"encoding/hex"
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
		value := strings.Join(resolved[name], ", ")
		static := readsOnlyInputs(template)
		switch {
		case static && (secretHeader(name) || readsSecretEnv(template)):
			out[key] = HeaderDigest(key, value)
		case secretHeader(name):
			out[key] = pathmask.MaskRedacted
		case static:
			out[key] = value
		default:
			out[key] = chain.CanonicalRefs(template)
		}
	}
	return out
}

const headerDigestPrefix = pathmask.MaskRedacted + " digest:"

func HeaderDigest(name, value string) string {
	sum := sha256.Sum256([]byte("shrt-header\x00" + name + "\x00" + value))
	return headerDigestPrefix + hex.EncodeToString(sum[:6])
}

func HeaderDigested(value string) bool {
	return strings.HasPrefix(value, headerDigestPrefix)
}

func readsOnlyInputs(template string) bool {
	for _, m := range bodyRef.FindAllStringSubmatch(template, -1) {
		switch chain.ParseRef(m[1]).Kind {
		case chain.RefVars, chain.RefEnv:
		default:
			return false
		}
	}
	return true
}

func readsSecretEnv(template string) bool {
	for _, m := range bodyRef.FindAllStringSubmatch(template, -1) {
		r := chain.ParseRef(m[1])
		if r.Kind == chain.RefEnv && secretHeader(r.Rest) {
			return true
		}
	}
	return false
}
