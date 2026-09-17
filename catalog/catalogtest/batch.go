package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

func BatchDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file(), batchFile()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Batch() *catalog.Catalog {
	cat, err := catalog.Parse(BatchDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func batchFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String("shrt/test/v1/batch.proto"),
		Package:    proto.String(Package),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"shrt/test/v1/test.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			message("BatchRequest",
				repeated(str("lines", 1)),
			),
			message("PreviewResult",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				str("amount", 2),
			),
			message("PreviewResponse",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				repeated(msg("results", 2, ".shrt.test.v1.PreviewResult")),
			),
			message("ReceiptResult",
				str("id", 1),
			),
			message("ReceiptResponse",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				repeated(msg("results", 2, ".shrt.test.v1.ReceiptResult")),
			),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("BatchService",
				method("Preview", ".shrt.test.v1.BatchRequest", ".shrt.test.v1.PreviewResponse"),
				method("Receipt", ".shrt.test.v1.BatchRequest", ".shrt.test.v1.ReceiptResponse"),
			),
		},
	}
}
