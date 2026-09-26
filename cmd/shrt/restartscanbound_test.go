package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/N4darae/shrt/runner"
)

func bigReadRecord(n, refusedAt int) *runner.Record {
	rec := &runner.Record{RunID: "20990101T000000Z-big", Chain: "big"}
	rec.Steps = append(rec.Steps, &runner.StepRecord{ID: "create", Index: 1, Call: "ThingService/Create", Status: runner.StatusPassed,
		HTTPStatus: 200, AuthProfile: "default", Request: json.RawMessage(`{"name":"widget"}`),
		Response: json.RawMessage(`{"error":{"code":"OK"},"id":"th-1","tags":["a","b"]}`)})
	for i := 0; i < n; i++ {
		st := &runner.StepRecord{ID: fmt.Sprintf("get_%d", i), Index: i + 2, Call: "ThingService/Fetch", Status: runner.StatusPassed,
			HTTPStatus: 200, AuthProfile: "default", Request: json.RawMessage(`{"id":"th-1"}`),
			BodyRefs: map[string]string{"id": "${create.id}"},
			Response: json.RawMessage(`{"error":{"code":"OK"},"thing":{"id":"th-1","tags":["a","b"]}}`)}
		if i == refusedAt {
			st.AuthRetry = runner.AuthRetryResent
		}
		rec.Steps = append(rec.Steps, st)
	}
	return rec
}

func TestSessionRestartEvidenceOnALongReadChainIsNotQuadratic(t *testing.T) {
	rec := bigReadRecord(20000, 10000)
	start := time.Now()
	if got := sessionRestartEvidence(rec, 10001); got != "" {
		t.Fatalf("identical reads after the refusal are no restart evidence, got %q", got)
	}
	if got := restartEvidence(rec, 10001); got == "" {
		t.Fatal("the resent read accepted after a fresh login is restart evidence")
	}
	if got := readBackAfter(rec, 10001); got != "get_10001" {
		t.Fatalf("the first clean read after the refusal reads the created id back, got %q", got)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("scanning a 20000-step record for restart evidence took %s; it must stay linear", took)
	}
}

func bruteShrunkList(rec *runner.Record, index int, st *runner.StepRecord) string {
	if !answeredCleanly(st) {
		return ""
	}
	var after any
	if json.Unmarshal(st.Response, &after) != nil {
		return ""
	}
	key, ok := requestKey(st)
	if !ok {
		return ""
	}
	for _, prior := range rec.Steps[:index] {
		if !answeredCleanly(prior) || prior.Call != st.Call {
			continue
		}
		if pk, ok := requestKey(prior); !ok || pk != key {
			continue
		}
		var before any
		if json.Unmarshal(prior.Response, &before) == nil && listShrank(before, after) {
			return prior.ID
		}
	}
	return ""
}

func listShrank(before, after any) bool {
	switch b := before.(type) {
	case map[string]any:
		a, ok := after.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range b {
			if list, isList := v.([]any); isList && len(list) > 0 {
				other, _ := a[k].([]any)
				if len(other) < len(list) {
					return true
				}
				continue
			}
			if listShrank(v, a[k]) {
				return true
			}
		}
	}
	return false
}

func TestShrunkListIndexAgreesWithAPairwiseScan(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	bodies := func() string {
		list := func() string {
			n := r.Intn(4)
			out := "["
			for i := 0; i < n; i++ {
				if i > 0 {
					out += ","
				}
				out += fmt.Sprint(i)
			}
			return out + "]"
		}
		switch r.Intn(5) {
		case 0:
			return fmt.Sprintf(`{"items":%s}`, list())
		case 1:
			return fmt.Sprintf(`{"page":{"items":%s,"other":%s}}`, list(), list())
		case 2:
			return fmt.Sprintf(`{"items":%s,"page":{"items":%s}}`, list(), list())
		case 3:
			return `{"page":"none"}`
		}
		return `{}`
	}
	for round := 0; round < 200; round++ {
		rec := &runner.Record{}
		for i := 0; i < 30; i++ {
			rec.Steps = append(rec.Steps, &runner.StepRecord{ID: fmt.Sprintf("s%d", i), Call: fmt.Sprintf("S/List%d", r.Intn(2)),
				Status: runner.StatusPassed, HTTPStatus: 200, Request: json.RawMessage(fmt.Sprintf(`{"q":%d}`, r.Intn(2))),
				Response: json.RawMessage(bodies())})
		}
		index := r.Intn(len(rec.Steps))
		scan := newPriorScan(rec, index)
		for _, st := range rec.Steps[index:] {
			if got, want := scan.shrunkList(st), bruteShrunkList(rec, index, st); got != want {
				t.Fatalf("round %d step %s: index says %q, pairwise scan %q", round, st.ID, got, want)
			}
		}
	}
}
