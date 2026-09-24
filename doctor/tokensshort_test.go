package doctor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/N4darae/shrt/config"
	"github.com/N4darae/shrt/doctor"
)

func tokensDetail(t *testing.T, cache string) string {
	t.Helper()
	cfg := repo(t)
	cfg.Target.BaseURL = "http://new.example.test"
	write(t, cfg.Abs(filepath.Join(config.DirName, config.TokensFile)), cache)
	opts := options()
	opts.TokenKeys = func(_ *config.Config, target string) []string {
		switch target {
		case "http://new.example.test":
			return []string{"default#aaaa", "default#dddd"}
		case "http://old.example.test":
			return []string{"default#bbbb"}
		}
		return nil
	}
	out := ""
	for _, f := range findAll(run(t, cfg, opts), doctor.CheckTokens) {
		if f.Level == doctor.LevelOK {
			out += f.Detail + "\n"
		}
	}
	return out
}

func TestDoctorTokensLineIsTheCountsWhenNothingIsOff(t *testing.T) {
	ok := tokensDetail(t, `{"default#aaaa":{"token":"t1","expires_at":"2099-01-01T00:00:00Z"},"default#dddd":{"token":"t2","expires_at":"2099-01-01T00:00:00Z"}}`)
	if strings.TrimSpace(ok) != "2 cached token(s) for this target's logins, 0 expired" {
		t.Fatalf("with nothing off the tokens line is only the counts, got %d chars:\n%s", len(ok), ok)
	}
	off := tokensDetail(t, `{"default#aaaa":{"token":"t1","expires_at":"2000-01-01T00:00:00Z"},"default#bbbb":{"token":"t2","expires_at":"2099-01-01T00:00:00Z"}}`)
	for _, want := range []string{"1 of them expired", "1 for another login", "forgotten"} {
		if !strings.Contains(off, want) {
			t.Errorf("with an expired or foreign token the line explains it (%q):\n%s", want, off)
		}
	}
}
