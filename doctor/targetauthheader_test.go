package doctor_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/doctor"
)

func TestAnAuthorizationWrittenIntoTargetHeadersFails(t *testing.T) {
	cfg := repo(t)
	cfg.Auth = acmeAuth("${env.ACME_PASSWORD}", nil)
	cfg.Target.Headers = map[string]string{"Authorization": "Bearer hand-written"}

	got := find(t, run(t, cfg, options()), doctor.CheckAuth)

	if got.Level != doctor.LevelError || !strings.Contains(got.Detail, "target.headers") {
		t.Fatalf("a hand-written Authorization in target.headers is sent on every call no profile covers; want FAIL "+
			"naming target.headers, got %s: %s", got.Level, got.Detail)
	}
}
