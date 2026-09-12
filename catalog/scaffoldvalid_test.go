package catalog_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

const corpusDescriptor = "../../.shrt/descriptor.binpb"

func TestScaffoldedBodyValidatesForEveryRPCInTheCorpus(t *testing.T) {
	if _, err := os.Stat(corpusDescriptor); err != nil {
		t.Skipf("no descriptor at %s — run 'shrt catalog build' first", corpusDescriptor)
	}
	cat, err := catalog.Load(corpusDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	methods := cat.Methods()
	if len(methods) == 0 {
		t.Fatal("the corpus descriptor holds no rpc")
	}
	for _, m := range methods {
		body, err := catalog.ScaffoldJSON(m.Input())
		if err != nil {
			t.Fatalf("%s: scaffold: %v", m.FullName, err)
		}
		if err := cat.ValidateInput(m, body); err != nil {
			t.Errorf("%s: the scaffolded body does not validate against its own request message: %v\n%s",
				m.FullName, err, body)
		}
	}
}

func TestScaffoldedBodyValidatesForEveryRPCInTheRichFixture(t *testing.T) {
	cat := catalogtest.Rich()
	for _, m := range cat.Methods() {
		body, err := catalog.ScaffoldJSON(m.Input())
		if err != nil {
			t.Fatalf("%s: scaffold: %v", m.FullName, err)
		}
		if err := cat.ValidateInput(m, body); err != nil {
			t.Errorf("%s: the scaffolded body does not validate against its own request message: %v\n%s",
				m.FullName, err, body)
		}
	}
}

func TestScaffoldEmitsOneMemberOfEachOneof(t *testing.T) {
	cat := catalogtest.Rich()
	m, err := cat.Lookup("OrderService/PlaceOrder")
	if err != nil {
		t.Fatal(err)
	}
	body := catalog.Scaffold(m.Input())
	armed := []string{}
	for _, name := range []string{"card_token", "bank_ref", "wallet_id"} {
		if _, ok := body[name]; ok {
			armed = append(armed, name)
		}
	}
	if len(armed) != 1 || armed[0] != "card_token" {
		t.Fatalf("want only the first member of the payment oneof, got %v", armed)
	}
	if _, ok := body["fast"]; !ok {
		t.Fatalf("the route oneof lost its member: %v", body)
	}
	if _, ok := body["cheap"]; ok {
		t.Fatalf("both members of the route oneof are armed: %v", body)
	}
	if _, ok := body["memo"]; !ok {
		t.Fatalf("proto3 optional is a synthetic oneof and must still be scaffolded: %v", body)
	}
}

func TestScaffoldHonoursAPreferredOneofMember(t *testing.T) {
	cat := catalogtest.Rich()
	m, _ := cat.Lookup("OrderService/PlaceOrder")
	body := catalog.ScaffoldWith(m.Input(), catalog.ScaffoldOptions{Prefer: []string{"bank_ref", "cheap"}})
	if _, ok := body["bank_ref"]; !ok {
		t.Fatalf("the named member must win: %v", body)
	}
	if _, ok := body["card_token"]; ok {
		t.Fatalf("the default member must step aside for the named one: %v", body)
	}
	if _, ok := body["cheap"]; !ok {
		t.Fatalf("a preference applies to every oneof it names: %v", body)
	}
	if err := cat.ValidateInput(m, mustJSON(t, body)); err != nil {
		t.Fatalf("a preferred scaffold must still validate: %v", err)
	}
}

func TestScaffoldUsesTheJSONFormOfEveryWellKnownType(t *testing.T) {
	cat := catalogtest.Rich()
	m, _ := cat.Lookup("OrderService/PlaceOrder")
	body := catalog.Scaffold(m.Input())
	want := map[string]any{
		"due_at":       "1970-01-01T00:00:00Z",
		"window":       "0s",
		"update_mask":  "",
		"note":         "",
		"amount_minor": "0",
		"metadata":     map[string]any{},
		"ping":         map[string]any{},
		"anything":     nil,
		"tags":         []any{},
		"payload":      map[string]any{},
	}
	got := map[string]any{}
	if err := json.Unmarshal(mustJSON(t, body), &got); err != nil {
		t.Fatal(err)
	}
	for name, expected := range want {
		actual := got[name]
		if mustJSONString(t, actual) != mustJSONString(t, expected) {
			t.Errorf("%s scaffolds as %s, want %s", name, mustJSONString(t, actual), mustJSONString(t, expected))
		}
	}
}

func TestSchemaMarksOneofMembersAndWellKnownForms(t *testing.T) {
	cat := catalogtest.Rich()
	m, _ := cat.Lookup("OrderService/PlaceOrder")
	schema := catalog.DescribeMessage(m.Input())
	byName := map[string]*catalog.Field{}
	for _, f := range schema.Fields {
		byName[f.Name] = f
	}
	card := byName["card_token"]
	if card.Oneof != "payment" || strings.Join(card.OneofMembers, ",") != "card_token,bank_ref,wallet_id" {
		t.Fatalf("card_token carries oneof %q members %v", card.Oneof, card.OneofMembers)
	}
	if byName["memo"].Oneof != "" {
		t.Fatalf("proto3 optional is a SYNTHETIC oneof and must not be reported as one: %q", byName["memo"].Oneof)
	}
	if byName["due_at"].JSONForm == "" || len(byName["due_at"].Fields) != 0 {
		t.Fatalf("a well-known type must show its JSON form, not its seconds/nanos fields: %+v", byName["due_at"])
	}
	text := schema.Text()
	for _, want := range []string{"oneof payment", "send AT MOST ONE", "RFC3339"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the schema listing does not mention %q:\n%s", want, text)
		}
	}
}

func TestStreamingFlagsRideOnTheMethod(t *testing.T) {
	cat := catalogtest.Rich()
	for ref, want := range map[string]string{
		"OrderService/PlaceOrder":   "",
		"OrderService/WatchOrder":   catalog.StreamKindServer,
		"OrderService/UploadOrders": catalog.StreamKindClient,
		"OrderService/SyncOrders":   catalog.StreamKindBidi,
	} {
		m, err := cat.Lookup(ref)
		if err != nil {
			t.Fatal(err)
		}
		if m.StreamKind() != want {
			t.Errorf("%s is %q, want %q", ref, m.StreamKind(), want)
		}
		if m.Streaming() != (want != "") {
			t.Errorf("%s: Streaming() is %v", ref, m.Streaming())
		}
		if (m.StreamRefusal() != "") != (want != "") {
			t.Errorf("%s: refusal text is %q", ref, m.StreamRefusal())
		}
	}
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	return string(mustJSON(t, v))
}
