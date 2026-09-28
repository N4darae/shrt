package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/N4darae/shrt/chain"
)

func TestANewFailureInAListSaysWhatKindOfChangeItIs(t *testing.T) {
	response, _ := json.Marshal(map[string]any{"orders": []any{
		map[string]any{"id_order": "ord-b", "status": "PENDING", "total_minor": "2750"},
		map[string]any{"id_order": "ord-c", "status": "CONFIRMED", "total_minor": "100"},
		map[string]any{"id_order": "ord-a", "status": "CANCELLED", "total_minor": "4250"},
	}})
	sr := &StepRecord{ID: "list", Status: StatusFailed, Response: response, Expect: []chain.ExpectResult{
		{Path: "orders.0.id_order", Rule: "equals", Want: "ord-a", Got: "ord-b"},
		{Path: "orders.0.status", Rule: "equals", Want: "CANCELLED", Got: "PENDING"},
		{Path: "orders.1.id_order", Rule: "equals", Want: "ord-z", Got: "ord-c"},
		{Path: "orders.2.total_minor", Rule: "equals", Want: "4000", Got: "4250"},
		{Path: "orders.3", Rule: "exists", Want: false, Got: true},
		{Path: "orders.2", Rule: "exists", Want: false, Got: true},
	}}
	_, fresh, _ := stepMismatch("list", sr, []chain.Pin{{Step: "list", Path: "orders.2"}})
	joined := strings.Join(fresh, "\n")
	for _, want := range []string{
		"orders.0.id_order want=ord-a got=ord-b (reordered: ord-a is at orders.2)",
		"orders.0.status want=CANCELLED got=PENDING (another item at orders.0, see orders.0.id_order)",
		"orders.1.id_order want=ord-z got=ord-c (item missing: no item of orders has id_order=ord-z)",
		"orders.2.total_minor want=4000 got=4250 (value changed: the item at orders.2 holds another total_minor)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if kind := ListChangeKind("list " + fresh[0]); kind != "reordered" {
		t.Errorf("kind of %q is %q", fresh[0], kind)
	}
	kinds := listChangeKinds(sr)
	if !strings.HasPrefix(kinds[4], "item added: orders holds 3 item(s)") {
		t.Errorf("an extra item: %q", kinds[4])
	}
}

func TestAListIsSameItemsInAnotherOrderOnlyWhenNoItemWasAddedOrMissing(t *testing.T) {
	response, _ := json.Marshal(map[string]any{"orders": []any{
		map[string]any{"id_order": "ord-b"},
		map[string]any{"id_order": "ord-a"},
		map[string]any{"id_order": "ord-x"},
	}})
	moved := chain.ExpectResult{Path: "orders.0.id_order", Rule: "equals", Want: "ord-a", Got: "ord-b"}
	sr := &StepRecord{ID: "list", Status: StatusFailed, Response: response, Expect: []chain.ExpectResult{moved}}
	if !ReorderedPaths(sr)["orders.0.id_order"] {
		t.Errorf("an asserted id found at another index is a reorder")
	}
	sr.Expect = append(sr.Expect, chain.ExpectResult{Path: "orders.2", Rule: "exists", Want: false, Got: true})
	if got := ReorderedPaths(sr); len(got) != 0 {
		t.Errorf("a list holding an item it should not is a changed set, not a reorder: %v", got)
	}
}

func TestAValueUnderAListThatEchoesTheRequestInAnotherOrderIsAReorder(t *testing.T) {
	request, _ := json.Marshal(map[string]any{"lines": []any{
		map[string]any{"id_product": "a", "qty": 2},
		map[string]any{"id_product": "a", "qty": 3},
		map[string]any{"id_product": "b", "qty": 1},
	}})
	reversed, _ := json.Marshal(map[string]any{"order": map[string]any{"lines": []any{
		map[string]any{"id_product": "b", "qty": 1, "price_minor": "799"},
		map[string]any{"id_product": "a", "qty": 3, "price_minor": "1250"},
		map[string]any{"id_product": "a", "qty": 2, "price_minor": "1250"},
	}}})
	qty := chain.ExpectResult{Path: "order.lines.2.qty", Rule: "equals", Want: float64(1), Got: float64(2)}
	sr := &StepRecord{ID: "create", Status: StatusFailed, Request: request, Response: reversed, Expect: []chain.ExpectResult{qty}}
	if !ReorderedPaths(sr)["order.lines.2.qty"] {
		t.Errorf("the lines sent, answered in another order, are a reorder: %v", listChangeKinds(sr))
	}
	doubled, _ := json.Marshal(map[string]any{"order": map[string]any{"lines": []any{
		map[string]any{"id_product": "a", "qty": 2},
		map[string]any{"id_product": "a", "qty": 3},
		map[string]any{"id_product": "b", "qty": 2},
	}}})
	sr.Response = doubled
	if ReorderedPaths(sr)["order.lines.2.qty"] {
		t.Errorf("a line answered with another qty is a value change, not a reorder")
	}
}

func TestALineMissingFromLinesOfOneProductIsMissingNotReordered(t *testing.T) {
	response, _ := json.Marshal(map[string]any{"order": map[string]any{"lines": []any{
		map[string]any{"id_product": "a", "qty": 6},
	}}})
	sr := &StepRecord{ID: "fetch", Status: StatusFailed, Response: response, Expect: []chain.ExpectResult{
		{Path: "order.lines.0.id_product", Rule: "equals", Want: "a", Got: "a", Passed: true},
		{Path: "order.lines.1.id_product", Rule: "equals", Want: "a"},
		{Path: "order.lines.1.qty", Rule: "equals", Want: float64(5)},
	}}
	kinds := listChangeKinds(sr)
	if kinds[1] != "item missing: order.lines holds 1 of the 2 item(s) with id_product=a" {
		t.Errorf("a repeated id is counted, not looked up: %q", kinds[1])
	}
	if len(ReorderedPaths(sr)) != 0 {
		t.Errorf("a dropped line is not a reorder: %v", ReorderedPaths(sr))
	}
}

func TestPinsHeldWhenEveryPinFailsAsPinnedAndOnlyOtherStepsFail(t *testing.T) {
	got := "${create.thing.id}"
	c := &chain.Chain{Name: "c", Steps: []*chain.Step{{ID: "create"}, {ID: "list"}},
		KeptRed: []chain.Pin{{Step: "list", Path: "things.0.id", Got: &got}}}
	rec := func(listed string) *Record {
		return &Record{KeptRed: KeptRedNotAsPinned, Steps: []*StepRecord{
			{ID: "create", Status: StatusFailed, Response: json.RawMessage(`{"thing":{"id":"t1","price":"249"}}`),
				Expect: []chain.ExpectResult{{Path: "thing.price", Rule: "equals", Want: "250", Got: "249"}}},
			{ID: "list", Status: StatusFailed, Response: json.RawMessage(`{"things":[{"id":"` + listed + `"}]}`),
				Expect: []chain.ExpectResult{{Path: "things.0.id", Rule: "equals", Want: "t0", Got: listed}}},
		}}
	}
	if !PinsHeld(c, rec("t1")) {
		t.Error("the pin failed as pinned, with its got resolved from the record; only an unpinned step failed besides")
	}
	if PinsHeld(c, rec("t9")) {
		t.Error("a pinned step that now gets another value did not fail as pinned")
	}
}

func TestThePrincipalDigestLeavesOutANestedSecret(t *testing.T) {
	digest := func(secret string) string {
		bs := AuthBindings{{Profile: "default", Procedure: "/shop.auth.v1.AuthService/Login",
			BodyFields: map[string]any{"username": "admin", "password": secret,
				"device": map[string]any{"name": "ci", "client_secret": secret}}}}
		return bs.principals()["default"]
	}
	if a, b := digest("pw-one"), digest("pw-two"); a == "" || a != b {
		t.Fatalf("a secret, nested or not, is not part of who logged in, got %q and %q", a, b)
	}
	other := AuthBindings{{Profile: "default", Procedure: "/shop.auth.v1.AuthService/Login",
		BodyFields: map[string]any{"username": "clerk", "password": "pw-one"}}}.principals()["default"]
	if other == digest("pw-one") {
		t.Fatal("another username is another principal")
	}
}

func TestJSONBodyOrStringKeepsJSONAndQuotesTheRest(t *testing.T) {
	var back string
	if err := json.Unmarshal(jsonBodyOrString([]byte("404 page not found\n")), &back); err != nil || back != "404 page not found\n" {
		t.Fatalf("a non-JSON body is recorded as a JSON string with its text: %q %v", back, err)
	}
	if in := []byte(`{"error":{"code":"internal"}}`); string(jsonBodyOrString(in)) != string(in) {
		t.Fatal("a JSON body must be recorded byte-for-byte")
	}
	if jsonBodyOrString(nil) != nil {
		t.Fatal("an empty body stays empty so omitempty drops the field")
	}
}

func TestSeedingNoteForARefusedLoginIsOneSentenceListingEachProfile(t *testing.T) {
	cases := []seeding{
		{tokenless: []string{"clerk", "default"}},
		{refused: []string{"clerk"}, tokenless: []string{"default"}},
	}
	for _, s := range cases {
		note := seedingNote(s)
		if strings.Count(note, "did not seed") != 1 || strings.Contains(note, "\n") {
			t.Errorf("%+v: want one did-not-seed sentence, got %q", s, note)
		}
		if !strings.Contains(note, "(clerk, shared (default))") {
			t.Errorf("%+v: want both profiles listed once, got %q", s, note)
		}
	}
}

func TestSeedingNoteNamesEveryOtherProfileOnTheRpcWhenOneWasSeeded(t *testing.T) {
	note := seedingNote(seeding{seeded: []string{"clerk"}, refused: []string{"default", "ops"}, tokenless: nil})
	if !strings.Contains(note, "the ops, shared (default) profiles on the same login rpc were not seeded") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "did not seed") {
		t.Fatalf("a seeded login must not read as a failed one: %q", note)
	}
}

func TestRecordedHeadersKeepInputAndHideSecrets(t *testing.T) {
	templates := map[string]string{
		"x-dry-run":     "1",
		"X-Tenant":      "${vars.tenant}",
		"Authorization": "Bearer ${env.TOKEN}",
		"X-Api-Key":     "k-123",
		"X-Request-Id":  "${uuid}",
		"X-Order":       "${order.order.id_order}",
		"X-Shipping":    "fast",
		"X-Region":      "${env.REGION}",
		"X-Session":     "${login.session}",
		"X-Signed":      "${env.API_PASSWORD}",
	}
	resolved := map[string][]string{
		"x-dry-run":     {"1"},
		"X-Tenant":      {"acme"},
		"Authorization": {"Bearer abc"},
		"X-Api-Key":     {"k-123"},
		"X-Request-Id":  {"0f0f"},
		"X-Order":       {"o-1"},
		"X-Shipping":    {"fast"},
		"X-Region":      {"eu"},
		"X-Session":     {"s-1"},
		"X-Signed":      {"pw"},
	}
	got := recordedHeaders(templates, resolved)
	want := map[string]string{
		"X-Dry-Run":     "1",
		"X-Tenant":      "acme",
		"Authorization": HeaderDigest("Authorization", "Bearer abc"),
		"X-Api-Key":     HeaderDigest("X-Api-Key", "k-123"),
		"X-Request-Id":  "${uuid}",
		"X-Order":       "${steps.order.response.order.id_order}",
		"X-Shipping":    "fast",
		"X-Region":      "eu",
		"X-Session":     "<redacted>",
		"X-Signed":      HeaderDigest("X-Signed", "pw"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header %s: got %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	for _, secret := range []string{"Bearer abc", "k-123", "pw"} {
		for k, v := range got {
			if strings.Contains(v, secret) {
				t.Errorf("header %s stores the credential %q: %q", k, secret, v)
			}
		}
	}
	if changed := recordedHeaders(map[string]string{"X-Api-Key": "k-124"}, map[string][]string{"X-Api-Key": {"k-124"}}); changed["X-Api-Key"] == got["X-Api-Key"] {
		t.Errorf("another credential value must record another digest, so the change is seen: %v", changed)
	}
	if none := recordedHeaders(nil, nil); none == nil || len(none) != 0 {
		t.Fatalf("a step sent with no headers records an empty set, so a header added later is seen: %v", none)
	}
}
