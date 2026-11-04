package runner

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNullStepInRecordIsRejected(t *testing.T) {
	var rec Record
	err := json.Unmarshal([]byte(`{"steps":[{},null]}`), &rec)
	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("want an indexed empty-step error, got %v", err)
	}
}

func TestRecordStepSkipsNullEntries(t *testing.T) {
	want := &StepRecord{ID: "fetch"}
	rec := &Record{Steps: []*StepRecord{nil, want, nil}}
	if got, ok := rec.Step("fetch"); !ok || got != want {
		t.Fatal("a null entry must not hide a valid step")
	}
	if _, ok := rec.Step("missing"); ok {
		t.Fatal("a null entry is not a matching step")
	}
}
