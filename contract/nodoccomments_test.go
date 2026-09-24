package contract_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func documentedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	str := func(name string, n int32) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), JsonName: proto.String(name), Number: proto.Int32(n),
			Type:  descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		}
	}
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("doc/v1/doc.proto"),
		Package: proto.String("doc.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("PutRequest"), Field: []*descriptorpb.FieldDescriptorProto{str("label", 1)}},
			{Name: proto.String("PutResponse"), Field: []*descriptorpb.FieldDescriptorProto{str("token", 1)}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("DocService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name: proto.String("Put"), InputType: proto.String(".doc.v1.PutRequest"), OutputType: proto.String(".doc.v1.PutResponse"),
			}},
		}},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{Location: []*descriptorpb.SourceCodeInfo_Location{
			{Path: []int32{4, 0, 2, 0}, Span: []int32{1, 0, 1}, LeadingComments: proto.String(" the shelf label printed on the tag\n")},
			{Path: []int32{4, 1, 2, 0}, Span: []int32{2, 0, 1}, LeadingComments: proto.String(" opaque receipt\n")},
			{Path: []int32{6, 0, 2, 0}, Span: []int32{3, 0, 1}, LeadingComments: proto.String(" stores one label\n")},
		}},
	}
	raw, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestProtoDocsNeverBecomeCommentsInGeneratedFiles(t *testing.T) {
	cat := documentedCatalog(t)
	m, err := cat.Lookup("doc.v1.DocService/Put")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog.DescribeMessage(m.Input()).Fields[0].Doc, "shelf label") {
		t.Fatal("fixture: the field doc did not load, so this test proves nothing")
	}

	raw, err := contract.RenderOverlay(contract.ScaffoldOverlay("doc", cat.Methods(), nil, cat.Methods()))
	if err != nil {
		t.Fatal(err)
	}
	assertNoYAMLComments(t, raw)
	if !strings.Contains(string(raw), "(proto: the shelf label printed on the tag)") {
		t.Fatalf("the proto doc must survive as part of the TODO text, not be dropped:\n%s", raw)
	}

	nodes, _, err := contract.ScaffoldSteps([]string{m.FullName}, []string{"put"}, contract.NewLibrary(nil), cat)
	if err != nil {
		t.Fatal(err)
	}
	step, err := yaml.Marshal(&yaml.Node{Kind: yaml.SequenceNode, Content: nodes})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(step), "#") {
		t.Fatalf("chain new writes the proto doc as a YAML comment into a file adopters commit:\n%s", step)
	}
}
