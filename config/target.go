package config

import (
	"net/url"
	"strings"
)

func NormalTarget(target string) string {
	t := strings.TrimRight(strings.TrimSpace(target), "/")
	u, err := url.Parse(t)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return t
	}
	scheme, rest, _ := strings.Cut(t, "://")
	host, path := rest, ""
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		host, path = rest[:i], rest[i:]
	}
	return strings.ToLower(scheme) + "://" + strings.ToLower(host) + path
}

func SameTarget(a, b string) bool {
	return NormalTarget(a) == NormalTarget(b)
}
