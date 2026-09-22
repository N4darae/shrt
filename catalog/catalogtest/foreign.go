package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const ForeignPackage = "shrt.foreign.v1"

func ForeignDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{foreignFile()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Foreign() *catalog.Catalog {
	cat, err := catalog.Parse(ForeignDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func foreignFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/foreign/v1/foreign.proto"),
		Package: proto.String(ForeignPackage),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			message("Status", str("code", 1), str("message", 2)),
			message("Request", str("id", 1)),
			message("CreateResponse", msg("status", 1, ".shrt.foreign.v1.Status"), str("id", 2)),
			message("FetchResponse", msg("status", 1, ".shrt.foreign.v1.Status"), str("name", 2)),
			message("Line", msg("status", 1, ".shrt.foreign.v1.Status"), str("amount", 2)),
			message("BatchResponse",
				msg("status", 1, ".shrt.foreign.v1.Status"),
				repeated(msg("lines", 2, ".shrt.foreign.v1.Line")),
			),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("ForeignService",
				method("Create", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.CreateResponse"),
				method("Fetch", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.FetchResponse"),
				method("Batch", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.BatchResponse"),
			),
		},
	}
}
