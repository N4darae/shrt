package config_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
)

func TestOnlyLoopbackAndAllowedHostsAreSentTo(t *testing.T) {
	allowed := []config.Target{
		{BaseURL: "http://127.0.0.1:8080"},
		{BaseURL: "http://localhost:8080"},
		{BaseURL: "http://[::1]:8080"},
		{BaseURL: "http://api.shop.localhost"},
		{BaseURL: "https://Staging.Example.test", AllowHosts: []string{"staging.example.test"}},
		{BaseURL: "https://staging.example.test:8443", AllowHosts: []string{"staging.example.test:9999"}},
		{BaseURL: "http://127.0.0.1:8080", HostOverride: "staging.example.test", AllowHosts: []string{"staging.example.test"}},
	}
	for _, target := range allowed {
		if err := target.HostRefusal(); err != nil {
			t.Errorf("%+v: want it allowed, got %v", target, err)
		}
	}
	refused := map[string]config.Target{
		"target.base_url":      {BaseURL: "https://api.example.test", AllowHosts: []string{"staging.example.test"}},
		"target.host_override": {BaseURL: "http://127.0.0.1:8080", HostOverride: "api.example.test:443"},
	}
	for key, target := range refused {
		err := target.HostRefusal()
		if err == nil || !strings.Contains(err.Error(), key+" names api.example.test") ||
			!strings.Contains(err.Error(), "allow_hosts: [api.example.test]") {
			t.Errorf("%+v: want a refusal naming %s and the line to add, got %v", target, key, err)
		}
	}
}
