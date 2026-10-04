package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"net/textproto"
	"os"
	"slices"
	"strings"

	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/pathmask"
)

var secretHeaderWords = map[string]bool{
	"auth": true, "authorization": true, "authentication": true, "token": true, "tokens": true, "secret": true,
	"secrets": true, "key": true, "apikey": true, "password": true, "passwd": true, "pass": true, "cookie": true,
	"session": true, "signature": true, "sig": true, "hmac": true, "credential": true, "credentials": true,
	"otp": true, "pin": true, "jwt": true, "bearer": true, "csrf": true, "xsrf": true, "pwd": true, "pw": true,
	"authtoken": true, "accesstoken": true, "refreshtoken": true, "idtoken": true, "sessiontoken": true,
	"clientsecret": true, "secretkey": true, "accesskey": true, "privatekey": true,
}

var secretHeaderHints = []string{"password", "passwd", "passphrase", "passcode", "apikey", "credential", "privatekey", "cookie", "csrf", "xsrf"}

var secretHeaderWordEndings = []string{"token", "tokens", "secret", "secrets"}

var secondFactorHeaderWords = map[string]bool{
	"mfa": true, "totp": true, "2fa": true, "otp": true, "oauth": true, "authz": true, "authn": true,
	"recovery": true, "magic": true, "signed": true,
}

var weakSecretHeaderWords = map[string]bool{"pass": true, "session": true}

var clearAfterWeakSecretWord = map[string]bool{
	"through": true, "region": true, "zone": true, "locale": true, "language": true, "timezone": true, "mode": true, "type": true,
}

var notSecretHeaderSuffixes = map[string]bool{"id": true, "remaining": true, "count": true}

func secretHeader(name string) bool {
	lower := strings.ReplaceAll(strings.ToLower(name), "0", "o")
	words := strings.FieldsFunc(lower, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	if n := len(words); n > 1 && notSecretHeaderSuffixes[words[n-1]] && !(words[n-2] == "session" && words[n-1] == "id") {
		return false
	}
	if slices.ContainsFunc(secretHeaderHints, func(hint string) bool { return strings.Contains(lower, hint) }) {
		return true
	}
	for i, word := range words {
		if weakSecretHeaderWords[word] {
			if i+1 < len(words) && clearAfterWeakSecretWord[words[i+1]] {
				continue
			}
			return true
		}
		if secretHeaderWords[word] || secondFactorHeaderWords[word] {
			return true
		}
		for _, ending := range secretHeaderWordEndings {
			if strings.HasSuffix(word, ending) {
				return true
			}
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

func learnHeaderSecrets(redactor *pathmask.Masker, templates map[string]string, scope *chain.Scope) {
	env := scope.Env
	if env == nil {
		env = os.LookupEnv
	}
	for name, template := range templates {
		credential := secretHeader(name)
		for _, envName := range chain.AuthBodyEnvNames(map[string]any{"v": template}) {
			if !credential && !secretHeader(envName) {
				continue
			}
			if value, ok := env(envName); ok {
				redactor.AddSecret(value)
			}
		}
		if !credential {
			continue
		}
		for _, ref := range chain.VarRefs(template) {
			if value, err := scope.ResolveValue(ref); err == nil {
				learnSecret(redactor, value)
			}
		}
		if !readsOnlyInputs(template) {
			continue
		}
		if value, err := scope.ResolveValue(template); err == nil {
			learnSecret(redactor, value)
		}
	}
}

func learnSentHeaderSecrets(redactor *pathmask.Masker, templates map[string]string, resolved map[string][]string) {
	for name := range templates {
		if !secretHeader(name) {
			continue
		}
		for _, value := range resolved[name] {
			redactor.AddSecret(value)
		}
	}
}
