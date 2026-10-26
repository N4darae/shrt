package config

import (
	"fmt"
	"net"
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

func (t Target) HostRefusal() error {
	hosts := map[string]string{}
	if u, err := url.Parse(strings.TrimSpace(t.BaseURL)); err == nil && u.Hostname() != "" {
		hosts["target.base_url"] = u.Hostname()
	}
	if h := bareHost(t.HostOverride); h != "" {
		hosts["target.host_override"] = h
	}
	for _, key := range []string{"target.base_url", "target.host_override"} {
		host, ok := hosts[key]
		if !ok || loopbackHost(host) || t.allows(host) {
			continue
		}
		return fmt.Errorf("%s names %s, which is not this machine and not in target.allow_hosts, so nothing was sent. "+
			"shrt creates and changes data on the backend it calls; list a host only when it is a test backend that may "+
			"take those writes: target: {allow_hosts: [%s]}", key, host, host)
	}
	return nil
}

func (t Target) allows(host string) bool {
	for _, allowed := range t.AllowHosts {
		if strings.EqualFold(bareHost(allowed), host) {
			return true
		}
	}
	return false
}

func bareHost(s string) string {
	s = strings.TrimSpace(s)
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return strings.Trim(s, "[]")
}

func loopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
