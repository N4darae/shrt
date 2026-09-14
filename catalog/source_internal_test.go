package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsesProtocRecognisesTheToolNotTheWord(t *testing.T) {
	for _, yes := range []string{"protoc", "/usr/bin/protoc", "./protoc", "protoc.exe", "PROTOC"} {
		if !UsesProtoc(yes) {
			t.Errorf("%q not recognised as protoc", yes)
		}
	}
	for _, no := range []string{"buf", "/usr/local/bin/buf", "protoc-gen-go", "protoc-gen-connect-go", ""} {
		if UsesProtoc(no) {
			t.Errorf("%q was treated as protoc", no)
		}
	}
}

func TestBuildArgsSpeaksEachToolsOwnCommandLine(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "acme", "v1")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "a.proto"), []byte("syntax = \"proto3\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := BuildSpec{Input: root, Output: filepath.Join(root, "out.binpb")}

	buf, err := buildArgs("buf", spec)
	if err != nil {
		t.Fatal(err)
	}
	if buf[0] != "build" || !contains(buf, "--as-file-descriptor-set") {
		t.Fatalf("buf args are not buf's grammar: %v", buf)
	}

	pc, err := buildArgs("protoc", spec)
	if err != nil {
		t.Fatal(err)
	}
	if contains(pc, "build") || contains(pc, "--as-file-descriptor-set") {
		t.Fatalf("protoc was handed buf's grammar, which is what made descriptor.binary a trap: %v", pc)
	}
	for _, want := range []string{"--include_imports", "--include_source_info", "-I"} {
		if !contains(pc, want) {
			t.Errorf("protoc args missing %s: %v", want, pc)
		}
	}
	if !strings.HasSuffix(pc[len(pc)-1], "a.proto") {
		t.Errorf("protoc was given no .proto file to compile: %v", pc)
	}
}

func TestBuildArgsRefusesProtocWithNoProtoFiles(t *testing.T) {
	root := t.TempDir()
	_, err := buildArgs("protoc", BuildSpec{Input: root, Output: filepath.Join(root, "o.binpb")})
	if err == nil {
		t.Fatal("protoc would have been run with no files, which fails with protoc's own message instead of ours")
	}
	if !strings.Contains(err.Error(), "descriptor.source") {
		t.Errorf("the error does not name the key to fix: %v", err)
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
