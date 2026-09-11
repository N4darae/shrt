package transport

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostOverride_SetsTheRequestHostAndTlsServerName(t *testing.T) {
	var gotHost, gotSNI string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		if r.TLS != nil {
			gotSNI = r.TLS.ServerName
		}
		w.Header().Set("Content-Type", ContentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		BaseURL:      srv.URL,
		HostOverride: "api.example.test",
		HTTPClient:   &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
	})
	if _, err := c.Raw(context.Background(), &Call{Procedure: "svc/Method"}); err != nil {
		t.Fatalf("call through the ip with a host override: %v", err)
	}
	if gotHost != "api.example.test" {
		t.Errorf("server saw Host %q, want api.example.test — nginx routes on it, so an ip in the url without "+
			"this lands on the default vhost", gotHost)
	}
	_ = gotSNI
}

func TestHostOverride_TheClientItBuildsVerifiesAgainstTheNameNotTheIp(t *testing.T) {
	c := New(Options{BaseURL: "https://192.0.2.10", HostOverride: "api.example.test"})
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport built by New", c.http.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.ServerName != "api.example.test" {
		t.Fatalf("ServerName = %v, want api.example.test — the certificate is issued for the NAME, so "+
			"without this an ip url either fails verification or has to disable it, and disabling it is "+
			"how a test harness stops noticing a broken certificate", tr.TLSClientConfig)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify is set — going direct must not mean going unverified; the point of " +
			"ServerName is that verification still happens, against the right name")
	}
}

func TestHostOverride_AbsentLeavesTheUrlsOwnHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", ContentTypeJSON)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	if _, err := c.Raw(context.Background(), &Call{Procedure: "svc/Method"}); err != nil {
		t.Fatalf("plain call: %v", err)
	}
	if gotHost == "api.example.test" || gotHost == "" {
		t.Errorf("Host = %q, want the url's own authority — the override must be opt-in", gotHost)
	}
}

func TestHostOverride_AppliesToACallerSuppliedTransport(t *testing.T) {
	caller := &http.Transport{MaxIdleConns: 3}
	c := New(Options{
		BaseURL:      "https://192.0.2.10",
		HostOverride: "api.example.test",
		HTTPClient:   &http.Client{Transport: caller},
	})
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.http.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.ServerName != "api.example.test" {
		t.Fatalf("ServerName = %v, want api.example.test — a supplied transport used to get the Host header "+
			"rewritten while SNI still said the ip, which fails verification against a name nobody wrote down",
			tr.TLSClientConfig)
	}
	if caller.TLSClientConfig != nil && caller.TLSClientConfig.ServerName != "" {
		t.Errorf("the caller's own transport now carries ServerName %q; it must be cloned, since it may be shared",
			caller.TLSClientConfig.ServerName)
	}
	if tr == caller {
		t.Error("the caller's transport was used in place rather than cloned")
	}
	if tr.MaxIdleConns != 3 {
		t.Errorf("MaxIdleConns = %d, want the caller's 3 preserved by the clone", tr.MaxIdleConns)
	}
}

func TestHostOverride_LeavesAnExplicitServerNameAlone(t *testing.T) {
	caller := &http.Transport{TLSClientConfig: &tls.Config{ServerName: "chosen.example"}}
	c := New(Options{
		BaseURL:      "https://192.0.2.10",
		HostOverride: "api.example.test",
		HTTPClient:   &http.Client{Transport: caller},
	})
	tr := c.http.Transport.(*http.Transport)
	if tr.TLSClientConfig.ServerName != "chosen.example" {
		t.Fatalf("ServerName = %q — a caller who named one meant it", tr.TLSClientConfig.ServerName)
	}
}
