package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithoutEditSourceCutsStepsAndTheirPinsOnly(t *testing.T) {
	src := `name: edit
steps:
  - call: A/Create
    id: a
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: A/Update
    id: b
    body: {id_a: "${a.id}"}
    expect:
      - {path: status.code, equals: SUCCESS}
  - call: A/Get
    id: c
kept_red:
  - {step: b, path: status.code}
`
	path := filepath.Join(t.TempDir(), "edit.yaml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Without(c, []string{"b"}, c.Name)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.EditSource([]byte(src), path)
	want := src[:strings.Index(src, "  - call: A/Update")] + "  - call: A/Get\n    id: c\n"
	if !ok || string(got) != want {
		t.Fatalf("ok=%v\n%s\nwant:\n%s", ok, got, want)
	}
}

func TestWithoutEditSourceRefusesWhenTheCutBreaksAnAlias(t *testing.T) {
	src := `name: alias
steps:
  - id: a
    call: A/Create
    expect:
      - &ok {path: status.code, equals: SUCCESS}
  - id: b
    call: A/Get
    expect:
      - *ok
`
	path := filepath.Join(t.TempDir(), "alias.yaml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Without(c, []string{"a"}, c.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.EditSource([]byte(src), path); ok {
		t.Fatalf("a cut that leaves an alias without its anchor is not taken")
	}
}
