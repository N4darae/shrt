package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const MinorityPackage = "shrt.minority.v1"

func MinorityDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{minorityFile()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Minority() *catalog.Catalog {
	cat, err := catalog.Parse(MinorityDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func minorityFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/minority/v1/minority.proto"),
		Package: proto.String(MinorityPackage),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			message("Err", str("code", 1), str("message", 2)),
			message("Stat", str("code", 1)),
			message("Req", str("id", 1)),
			message("MajorityA", msg("error", 1, ".shrt.minority.v1.Err"), str("a", 2)),
			message("MajorityB", msg("error", 1, ".shrt.minority.v1.Err"), str("b", 2)),
			message("MajorityC", msg("error", 1, ".shrt.minority.v1.Err"), str("c", 2)),
			message("Odd", msg("status", 1, ".shrt.minority.v1.Stat"), str("d", 2)),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("MixedService",
				method("A", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityA"),
				method("B", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityB"),
				method("C", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityC"),
				method("D", ".shrt.minority.v1.Req", ".shrt.minority.v1.Odd"),
			),
		},
	}
}
