package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/runner"
)

func TestABuildLabelIsStampedIntoTheRecord(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()

	rec, err := newRunner(t, srv).Run(context.Background(), testChain(), runner.Options{Build: "rc-1"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Build != "rc-1" {
		t.Fatalf("record build = %q, want rc-1: two builds behind one base_url are otherwise indistinguishable", rec.Build)
	}
}

func buildHeaderRunner(t *testing.T, srv *fakeServer) *runner.Runner {
	t.Helper()
	cfg := testConfig(srv.URL)
	cfg.Target.BuildHeader = "X-Build"
	r, _, err := runner.NewFromConfig(context.Background(), cfg, catalogtest.New())
	if err != nil {
		t.Fatalf("runner from config: %v", err)
	}
	return r
}

func TestTheConfiguredBuildHeaderIsReadFromTheServer(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.builds = []string{"b-7"}

	rec, err := buildHeaderRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rec.Passed() {
		t.Fatalf("want passed, got %s: %s", rec.Status, rec.Failure)
	}
	if rec.Build != "b-7" {
		t.Fatalf("record build = %q, want the server's own X-Build value b-7", rec.Build)
	}
}

func TestABuildThatChangesMidRunIsRecordedAsBoth(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.builds = []string{"b-7", "b-7", "b-8"}

	rec, err := buildHeaderRunner(t, srv).Run(context.Background(), testChain(), runner.Options{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(rec.Build, "b-7") || !strings.Contains(rec.Build, "b-8") {
		t.Fatalf("record build = %q, want both builds named: a deploy landed mid-run", rec.Build)
	}
	fetch := stepByID(t, rec, "fetch")
	if !strings.Contains(fetch.Warning, "b-8") {
		t.Errorf("the step that first saw the new build should warn, got %q", fetch.Warning)
	}
}

func TestALabelThatDisagreesWithTheServerIsFlagged(t *testing.T) {
	srv := newFakeServer()
	defer srv.Close()
	srv.builds = []string{"b-7"}

	rec, err := buildHeaderRunner(t, srv).Run(context.Background(), testChain(), runner.Options{Build: "rc-1"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rec.Build != "rc-1" {
		t.Fatalf("an explicit -build label is what the caller asked to be recorded, got %q", rec.Build)
	}
	if !strings.Contains(stepByID(t, rec, "create").Warning, "b-7") {
		t.Errorf("a label contradicting the server's own header must be visible in the record")
	}
}
