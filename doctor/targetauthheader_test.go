package doctor_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

func TestAnAuthorizationWrittenIntoTargetHeadersFails(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = &config.Auth{
		Call:      "acme.iam.v1.AuthService/Login",
		Body:      map[string]any{"username": "${env.ACME_USER}", "password": "${env.ACME_PASSWORD}"},
		TokenPath: "access_token",
	}
	cfg.Target.Headers = map[string]string{"Authorization": "Bearer hand-written"}

	got := find(t, run(t, cfg, options()), doctor.CheckAuth)

	if got.Level != doctor.LevelError || !strings.Contains(got.Detail, "target.headers") {
		t.Fatalf("a hand-written Authorization in target.headers is sent on every call no profile covers; want FAIL "+
			"naming target.headers, got %s: %s", got.Level, got.Detail)
	}
}
