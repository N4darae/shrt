package runner_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/N4darae/shrt/runner"
)

func TestADroppedCallSaysSoWhenLaterStepsWereAnswered(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after bool
		want  string
	}{
		{"a later step answered", true, "The backend answered later steps of this run, so this request itself likely broke it"},
		{"nothing after it answered", false, "This is not a verdict about the rpc: check the backend is up and run again"},
	} {
		r := bareRunner(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			if strings.HasSuffix(r.URL.Path, "/Create") {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = conn.Close()
				}
				return
			}
			answering(200, "application/json", `{"error":{"code":"OK"},"id":"a"}`)(w, r)
		}, nil)
		shown := ""
		r.OnStep = func(sr *runner.StepRecord) { shown += sr.Error }
		c := flow(skipAuth(step("create", "ThingService/Create", map[string]any{"name": "widget"})))
		if tc.after {
			c = flow(c.Steps[0], fetchA().Steps[0])
		}
		msg := run(t, r, normalized(t, c), runner.Options{KeepGoing: true}).Steps[0].Error
		if !strings.Contains(msg, tc.want) || !strings.Contains(shown, tc.want) || strings.Count(msg, "run again")+strings.Count(msg, "likely broke it") != 1 {
			t.Fatalf("%s: want %q in the record and the step line, got %q / %q", tc.name, tc.want, msg, shown)
		}
	}
}
