package catalog_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
)

func TestLookupAcceptsShortAndFullNames(t *testing.T) {
	cat := catalogtest.New()
	for _, ref := range []string{
		"shrt.test.v1.ThingService/Create",
		"/shrt.test.v1.ThingService/Create",
		"ThingService/Create",
		"Create",
	} {
		m, err := cat.Lookup(ref)
		if err != nil {
			t.Fatalf("lookup %q: %v", ref, err)
		}
		if m.Procedure() != "/shrt.test.v1.ThingService/Create" {
			t.Fatalf("lookup %q resolved to %s", ref, m.Procedure())
		}
	}
}

func TestLookupRejectsUnknownAndAmbiguousNames(t *testing.T) {
	cat := catalogtest.New()
	if _, err := cat.Lookup("NoSuchRpc"); err == nil {
		t.Fatal("unknown rpc should error")
	}
	if _, err := cat.Lookup("shrt.test.v1"); err == nil {
		t.Fatal("a package name is not an rpc")
	}
}

func TestValidateInputRejectsUnknownFieldsAndBadEnums(t *testing.T) {
	cat := catalogtest.New()
	m, err := cat.Lookup("ThingService/Create")
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.ValidateInput(m, []byte(`{"name":"a","kind":"KIND_A"}`)); err != nil {
		t.Fatalf("valid body rejected: %v", err)
	}
	if err := cat.ValidateInput(m, []byte(`{"nope":"a"}`)); err == nil {
		t.Fatal("unknown field should be rejected")
	}
	if err := cat.ValidateInput(m, []byte(`{"kind":"KIND_MISSING"}`)); err == nil {
		t.Fatal("unknown enum value should be rejected")
	}
	if err := cat.ValidateInput(m, []byte(`{"name":123}`)); err == nil {
		t.Fatal("wrong scalar type should be rejected")
	}
}

func TestScaffoldUsesRealFieldNamesAndEnumValues(t *testing.T) {
	cat := catalogtest.New()
	m, _ := cat.Lookup("ThingService/Create")
	body := catalog.Scaffold(m.Input())
	if _, ok := body["idempotency_key"]; !ok {
		t.Fatalf("scaffold is missing idempotency_key: %v", body)
	}
	if body["kind"] != "KIND_UNSPECIFIED" {
		t.Fatalf("want the zero enum value, got %v", body["kind"])
	}
	if err := cat.ValidateInput(m, mustJSON(t, body)); err != nil {
		t.Fatalf("a scaffolded body must be valid: %v", err)
	}
}

func TestDescribeMessageWalksNestedFields(t *testing.T) {
	cat := catalogtest.New()
	m, _ := cat.Lookup("ThingService/Create")
	s := catalog.DescribeMessage(m.Output())
	if !strings.Contains(s.Text(), "code") {
		t.Fatalf("nested error fields missing from:\n%s", s.Text())
	}
}

func TestMethodsAreSorted(t *testing.T) {
	cat := catalogtest.New()
	methods := cat.Methods()
	want := []string{
		"shrt.test.v1.AuthService/Login",
		"shrt.test.v1.PartnerAuthService/Login",
		"shrt.test.v1.PartnerService/FetchMine",
		"shrt.test.v1.ThingService/Create",
		"shrt.test.v1.ThingService/Fetch",
	}
	got := []string{}
	for _, m := range methods {
		got = append(got, m.FullName)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("catalog holds %v, want %v", got, want)
	}
	for i := 1; i < len(methods); i++ {
		if methods[i-1].FullName > methods[i].FullName {
			t.Fatalf("methods are not sorted: %s before %s", methods[i-1].FullName, methods[i].FullName)
		}
	}
}
