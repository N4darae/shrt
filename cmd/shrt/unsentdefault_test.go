package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func descriptorWithoutFetchTotal(t *testing.T) []byte {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(catalogtest.Descriptor(), fds); err != nil {
		t.Fatal(err)
	}
	for _, m := range fds.File[0].MessageType {
		if m.GetName() != "FetchResponse" {
			continue
		}
		kept := m.Field[:0]
		for _, f := range m.Field {
			if f.GetName() != "total" {
				kept = append(kept, f)
			}
		}
		m.Field = kept
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func addedFieldWorkspace(t *testing.T, total *int) {
	t.Helper()
	nextID := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			nextID++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(nextID)})
		case "/shrt.test.v1.ThingService/Fetch":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"}
			if *total != 0 {
				out["total"] = *total
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	chdirToFreshCLIWorkspace(t, srv.URL)
	writeFile(t, ".shrt/descriptor.binpb", string(descriptorWithoutFetchTotal(t)))
	captureStdout(t, func() {
		if err := runRun(context.Background(), []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(context.Background(), []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.Descriptor()))
}

func TestCLIVerifyDoesNotReportAFieldTheBackendNeverSent(t *testing.T) {
	total := 0
	addedFieldWorkspace(t, &total)
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow"})
	})
	if verr != nil {
		t.Fatalf("a field the descriptor gained but the backend never sends is not drift, got %v\n%s", verr, out)
	}
	if !strings.Contains(out, "not on the wire") || !strings.Contains(out, "fetch total") {
		t.Fatalf("the uncounted field should be named:\n%s", out)
	}
}

func TestCLIVerifyStillReportsANewFieldTheBackendSends(t *testing.T) {
	total := 0
	addedFieldWorkspace(t, &total)
	total = 7
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	if verr == nil || !strings.Contains(out, "unexpected total") {
		t.Fatalf("a new field sent with a value is a change, got %v\n%s", verr, out)
	}
}
