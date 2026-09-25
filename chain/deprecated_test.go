package chain_test

import (
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func deprecatedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(catalogtest.Descriptor(), fds); err != nil {
		t.Fatal(err)
	}
	f := fds.File[0]
	for _, svc := range f.Service {
		if svc.GetName() != "ThingService" {
			continue
		}
		for _, m := range svc.Method {
			if m.GetName() == "Fetch" {
				m.Options = &descriptorpb.MethodOptions{Deprecated: proto.Bool(true)}
			}
		}
	}
	for _, m := range f.MessageType {
		for _, fd := range m.Field {
			if (m.GetName() == "FetchResponse" && fd.GetName() == "name") || (m.GetName() == "CreateRequest" && fd.GetName() == "kind") {
				fd.Options = &descriptorpb.FieldOptions{Deprecated: proto.Bool(true)}
			}
		}
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestLintWarnsOnADeprecatedRPCAndDeprecatedFields(t *testing.T) {
	cat := deprecatedCatalog(t)
	c := &chain.Chain{Name: "deprecated", Steps: []*chain.Step{
		{ID: "create", Call: "ThingService/Create", Body: map[string]any{"name": "w", "kind": "KIND_A"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}}},
		{ID: "fetch", Call: "ThingService/Fetch", Body: map[string]any{"id": "${create.id}"},
			Expect: []chain.Expectation{{Path: "error.code", Equals: "OK"}, {Path: "name", Equals: "w"}}},
	}}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, i := range chain.Lint(c, cat) {
		if i.Kind == chain.KindDeprecated {
			if i.IsError() {
				t.Fatalf("a deprecation is a warning, not an error: %+v", i)
			}
			got = append(got, i.Step+": "+i.Message)
		}
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"fetch: calls shrt.test.v1.ThingService/Fetch, which the proto marks deprecated",
		`create: body field "kind" is deprecated`,
		`fetch: expect on "name" reads a field`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("want %q among the deprecation warnings:\n%s", want, joined)
		}
	}
	if len(got) != 3 {
		t.Fatalf("want exactly three deprecation warnings, got:\n%s", joined)
	}
	m, err := cat.Lookup("ThingService/Fetch")
	if err != nil || !m.Deprecated() {
		t.Fatalf("the method reports its deprecation: %v", err)
	}
	if !strings.Contains(catalog.DescribeMessage(m.Output()).Text(), "DEPRECATED") {
		t.Fatalf("describe marks a deprecated field:\n%s", catalog.DescribeMessage(m.Output()).Text())
	}
}
