package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/N4darae/shrt/diff"
)

func TestARegressionVerdictSaysWhatToDoIfTheChangeIsIntended(t *testing.T) {
	added := false
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/shrt.test.v1.ThingService/Create":
			next++
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "OK"}, "id": "thing-" + itoa(next)})
		case "/shrt.test.v1.ThingService/Fetch":
			out := map[string]any{"error": map[string]any{"code": "OK"}, "id": body["id"], "name": "widget"}
			if added {
				out["total"] = 5
			}
			_ = json.NewEncoder(w).Encode(out)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	chdirToFreshCLIWorkspace(t, srv.URL)
	ctx := context.Background()
	captureStdout(t, func() {
		if err := runRun(ctx, []string{"cli-thing-flow", "-quiet"}); err != nil {
			t.Fatalf("shrt run: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-note", "baseline"}); err != nil {
			t.Fatalf("propose: %v", err)
		}
		if err := runConfirm(ctx, []string{"cli-thing-flow", "-approve", "-by", "alice@example.test"}); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	added = true
	var verr error
	out := captureStdout(t, func() { verr = runVerify(ctx, []string{"cli-thing-flow", "-quiet"}) })
	if code := exitCodeOf(verr); code != 1 || !strings.Contains(verr.Error(), "regression") {
		t.Fatalf("an added response field is a change: want a regression, got %d: %v\n%s", code, verr, out)
	}
	for _, want := range []string{"If the change is intended", "shrt confirm cli-thing-flow -supersede", "approves"} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("the verdict lacks %q: %v\n%s", want, verr, out)
		}
	}
}

func TestARegressionOfOnlyAddedFieldsSaysSo(t *testing.T) {
	r := &diff.Report{Changes: []diff.Change{
		{Step: "create_order", Path: "order.currency", Kind: diff.KindUnexpected, Got: "EUR"},
		{Step: "list_orders", Path: "orders.0.currency", Kind: diff.KindUnexpected, Got: "EUR"},
	}}
	if got := regressionShape(r); !strings.Contains(got, "field(s) the safe spot does not have: create_order order.currency, list_orders orders.0.currency") {
		t.Fatalf("added fields are named as added: %q", got)
	}
	r.Changes = append(r.Changes, diff.Change{Step: "x", Path: "total", Kind: diff.KindChanged, Want: 1, Got: 2})
	if got := regressionShape(r); got != "" {
		t.Fatalf("a changed value is not an added field: %q", got)
	}
}
