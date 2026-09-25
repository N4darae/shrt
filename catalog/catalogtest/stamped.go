package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const StampedPackage = "shrt.stamped.v1"

func StampedDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{stampedFile()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Stamped() *catalog.Catalog {
	cat, err := catalog.Parse(StampedDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func stampedFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/stamped/v1/stamped.proto"),
		Package: proto.String(StampedPackage),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			message("Status", str("code", 1), str("reason", 2)),
			message("Item",
				str("id_item", 1),
				str("name", 2),
				str("created_at", 3),
				str("updated_at", 4),
				str("expires_at", 5),
			),
			message("CreateItemRequest", str("name", 1)),
			message("ItemIDRequest", str("id_item", 1)),
			message("ItemResponse",
				msg("status", 1, ".shrt.stamped.v1.Status"),
				msg("item", 2, ".shrt.stamped.v1.Item"),
			),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("ItemService",
				method("CreateItem", ".shrt.stamped.v1.CreateItemRequest", ".shrt.stamped.v1.ItemResponse"),
				method("GetItem", ".shrt.stamped.v1.ItemIDRequest", ".shrt.stamped.v1.ItemResponse"),
			),
		},
	}
}
