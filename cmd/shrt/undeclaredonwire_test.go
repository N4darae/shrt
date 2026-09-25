package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
)

func undeclaredFieldWorkspace(t *testing.T, total *int, forgetValues bool) {
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
	if forgetValues {
		e, err := loadEnv(false)
		if err != nil {
			t.Fatal(err)
		}
		spot, err := e.store.LoadSafeSpot("cli-thing-flow")
		if err != nil {
			t.Fatal(err)
		}
		held := false
		for _, st := range spot.Steps {
			held = held || len(st.Undeclared) > 0
			st.Undeclared = nil
		}
		if !held {
			t.Fatalf("the safe spot's run had total on the wire undeclared, so the record should hold its value")
		}
		spot.Digest = spot.ComputeDigest()
		out, err := json.MarshalIndent(spot, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, ".shrt/safespots/cli-thing-flow.json", string(out))
	}
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.Descriptor()))
}

func verifyThingFlow(t *testing.T) (string, error) {
	t.Helper()
	var verr error
	out := captureStdout(t, func() {
		verr = runVerify(context.Background(), []string{"cli-thing-flow", "-quiet"})
	})
	return out, verr
}

func TestCLIVerifyReportsAnUndeclaredFieldTheSafeSpotHadOnTheWireThatIsGoneNow(t *testing.T) {
	for _, forget := range []bool{false, true} {
		total := 7
		undeclaredFieldWorkspace(t, &total, forget)
		total = 0
		out, verr := verifyThingFlow(t)
		if verr == nil || !strings.Contains(out, "total") || strings.Contains(out, "not on the wire (left at the proto3 default, the same bytes") {
			t.Fatalf("value recorded %v: total was on the wire, undeclared, in the safe spot's run and is gone now, which is a change, got %v\n%s", !forget, verr, out)
		}
		if !forget && !strings.Contains(out, "want=7") {
			t.Fatalf("the change should show the value the safe spot's run received:\n%s", out)
		}
	}
}

func TestCLIVerifyDoesNotReportAnUndeclaredFieldDeclaredNowWithTheSameValue(t *testing.T) {
	total := 7
	undeclaredFieldWorkspace(t, &total, false)
	out, verr := verifyThingFlow(t)
	if verr != nil || strings.Contains(out, "unexpected total") {
		t.Fatalf("total was on the wire with the same value before it was declared, so this is no change, got %v\n%s", verr, out)
	}
	if !strings.Contains(out, "same value") || !strings.Contains(out, "fetch total") {
		t.Fatalf("the field declared since should be named as not counted:\n%s", out)
	}
	total = 8
	out, verr = verifyThingFlow(t)
	if verr == nil || !strings.Contains(out, "want=7") || !strings.Contains(out, "got=8") {
		t.Fatalf("a value that differs from the one the safe spot's run received is a change, got %v\n%s", verr, out)
	}
}

func TestCLIVerifyDoesNotCompareAnUndeclaredValueAnOlderSafeSpotDidNotRecord(t *testing.T) {
	total := 7
	undeclaredFieldWorkspace(t, &total, true)
	out, verr := verifyThingFlow(t)
	if verr != nil || strings.Contains(out, "unexpected total") {
		t.Fatalf("total was on the wire in the safe spot's run, its value unknown, so a value now is not a regression, got %v\n%s", verr, out)
	}
	if !strings.Contains(out, "not compared") || !strings.Contains(out, "fetch total") {
		t.Fatalf("the uncompared field should be named:\n%s", out)
	}
}
