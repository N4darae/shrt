package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const headerInputChain = `apiVersion: shrt/v1
name: cli-headers
vars:
    key: alpha
steps:
    - id: fetch
      call: ThingService/Fetch
      headers:
          X-Tenant-Key: ${vars.key}
          X-Region: ${env.SHRT_TEST_REGION}
      body:
          id: thing-1
      expect:
          - path: error.code
            equals: OK
`

func TestVerifyNamesAChangedHeaderInputAsTheCause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := "widget"
		if r.Header.Get("X-Tenant-Key") != "alpha" || r.Header.Get("X-Region") != "us" {
			name = "other"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-1", "name": name})
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/chains/cli-headers.yaml", headerInputChain)
	t.Setenv("SHRT_TEST_REGION", "us")
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-headers", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-headers", "-note", "fetch"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-headers", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	for _, tc := range []struct {
		name, region string
		args         []string
		header       string
	}{
		{"credential-named header from a var", "us", []string{"-var", "key=beta"}, "headers.X-Tenant-Key"},
		{"header from env", "eu", nil, "headers.X-Region"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHRT_TEST_REGION", tc.region)
			var err error
			out := captureStdout(t, func() {
				err = runVerify(ctx, append([]string{"cli-headers", "-quiet"}, tc.args...))
			})
			if err == nil || !strings.Contains(err.Error(), "drift with different input") {
				t.Fatalf("the header sent differs, so the drift is with different input: %v\n%s", err, out)
			}
			if !strings.Contains(out, "request differs from the confirmed run at fetch "+tc.header) {
				t.Fatalf("want the request-differs line for %s:\n%s", tc.header, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "headers.X-Tenant-Key") && (strings.Contains(line, "beta") || strings.Contains(line, "alpha")) {
					t.Fatalf("a credential-named header's value must not be printed: %s", line)
				}
			}
		})
	}
}
