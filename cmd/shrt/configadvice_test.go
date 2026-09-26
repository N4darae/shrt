package main

import (
	"context"
	"strings"
	"testing"
)

func TestABrokenConfigsAdviceMatchesWhatInitDoes(t *testing.T) {
	srv := newFakeCLIBackend()
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/config.yaml", "target:\n    base_url: "+srv.URL+"\n    timeuot: 5s\ndescriptor:\n    file: .shrt/descriptor.binpb\n")
	_, err := loadEnv(false)
	if err == nil {
		t.Fatal("a config with an unknown key loaded")
	}
	msg := err.Error()
	if strings.Contains(msg, "writes a fresh config") {
		t.Fatalf("init keeps an existing config, so it does not write a fresh one: %q", msg)
	}
	if !strings.Contains(msg, "-force-config") || !strings.Contains(msg, `unknown key "timeuot" at line 3 (did you mean "timeout"?)`) {
		t.Fatalf("name the key and the line, and the one init flag that does rebuild the config: %q", msg)
	}
	var initErr error
	captureStdout(t, func() { initErr = runInit(context.Background(), nil) })
	if initErr == nil || !strings.Contains(initErr.Error(), `unknown key "timeuot"`) {
		t.Fatalf("init stops on the same parse error the advice describes, got %v", initErr)
	}
}
