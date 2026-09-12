package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/N4darae/shrt/catalog"
)

const RichPackage = "shrt.test.rich.v1"

func RichDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: wellKnownFiles()}
	fds.File = append(fds.File, richFile())
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Rich() *catalog.Catalog {
	cat, err := catalog.Parse(RichDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func wellKnownFiles() []*descriptorpb.FileDescriptorProto {
	seen := map[string]bool{}
	out := []*descriptorpb.FileDescriptorProto{}
	for _, m := range []proto.Message{
		&timestamppb.Timestamp{}, &durationpb.Duration{}, &fieldmaskpb.FieldMask{},
		&structpb.Struct{}, &emptypb.Empty{}, &anypb.Any{}, &wrapperspb.StringValue{},
	} {
		fd := m.ProtoReflect().Descriptor().ParentFile()
		if seen[fd.Path()] {
			continue
		}
		seen[fd.Path()] = true
		out = append(out, protodesc.ToFileDescriptorProto(fd))
	}
	return out
}

func richFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/test/rich/v1/rich.proto"),
		Package: proto.String(RichPackage),
		Syntax:  proto.String("proto3"),
		Dependency: []string{
			"google/protobuf/timestamp.proto",
			"google/protobuf/duration.proto",
			"google/protobuf/field_mask.proto",
			"google/protobuf/struct.proto",
			"google/protobuf/empty.proto",
			"google/protobuf/any.proto",
			"google/protobuf/wrappers.proto",
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{
			enum("Channel", "CHANNEL_UNSPECIFIED", "CHANNEL_WEB", "CHANNEL_BRANCH"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			message("Line", str("sku", 1), num("qty", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64)),
			orderRequest(),
			message("RichError", str("code", 1), str("message", 2)),
			orderResponse(),
			message("WatchRequest", str("id_order", 1)),
			message("WatchEvent", str("id_order", 1), str("state", 2)),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("OrderService",
				method("PlaceOrder", ".shrt.test.rich.v1.OrderRequest", ".shrt.test.rich.v1.OrderResponse"),
				streamingMethod("WatchOrder", ".shrt.test.rich.v1.WatchRequest", ".shrt.test.rich.v1.WatchEvent", false, true),
				streamingMethod("UploadOrders", ".shrt.test.rich.v1.OrderRequest", ".shrt.test.rich.v1.OrderResponse", true, false),
				streamingMethod("SyncOrders", ".shrt.test.rich.v1.OrderRequest", ".shrt.test.rich.v1.WatchEvent", true, true),
			),
		},
	}
}

func orderResponse() *descriptorpb.DescriptorProto {
	m := message("OrderResponse",
		msg("error", 1, ".shrt.test.rich.v1.RichError"),
		str("id_order", 2),
		mapField("labels", 3, ".shrt.test.rich.v1.OrderResponse.LabelsEntry"),
	)
	m.NestedType = append(m.NestedType, mapEntryScalar("LabelsEntry"))
	return m
}

func orderRequest() *descriptorpb.DescriptorProto {
	m := message("OrderRequest",
		str("id_order", 1),
		msg("due_at", 3, ".google.protobuf.Timestamp"),
		msg("window", 4, ".google.protobuf.Duration"),
		inOneof(str("card_token", 5), 0),
		inOneof(str("bank_ref", 6), 0),
		inOneof(str("wallet_id", 7), 0),
		msg("update_mask", 8, ".google.protobuf.FieldMask"),
		msg("note", 9, ".google.protobuf.StringValue"),
		msg("amount_minor", 10, ".google.protobuf.Int64Value"),
		msg("metadata", 11, ".google.protobuf.Struct"),
		msg("ping", 12, ".google.protobuf.Empty"),
		msg("anything", 13, ".google.protobuf.Value"),
		msg("tags", 14, ".google.protobuf.ListValue"),
		msg("payload", 15, ".google.protobuf.Any"),
		repeated(msg("lines", 16, ".shrt.test.rich.v1.Line")),
		mapField("lines_by_id", 17, ".shrt.test.rich.v1.OrderRequest.LinesByIdEntry"),
		enumField("channel", 18, ".shrt.test.rich.v1.Channel"),
		proto3Optional(str("memo", 19), 2),
		inOneof(str("fast", 20), 1),
		inOneof(str("cheap", 21), 1),
		msg("flagged", 22, ".google.protobuf.BoolValue"),
		msg("ratio", 23, ".google.protobuf.DoubleValue"),
		msg("first_line", 24, ".shrt.test.rich.v1.Line"),
	)
	m.NestedType = []*descriptorpb.DescriptorProto{
		mapEntry("LinesByIdEntry", ".shrt.test.rich.v1.Line"),
	}
	m.OneofDecl = []*descriptorpb.OneofDescriptorProto{
		{Name: proto.String("payment")},
		{Name: proto.String("route")},
		{Name: proto.String("_memo")},
	}
	return m
}

func inOneof(f *descriptorpb.FieldDescriptorProto, index int32) *descriptorpb.FieldDescriptorProto {
	f.OneofIndex = proto.Int32(index)
	return f
}

func proto3Optional(f *descriptorpb.FieldDescriptorProto, index int32) *descriptorpb.FieldDescriptorProto {
	f.OneofIndex = proto.Int32(index)
	f.Proto3Optional = proto.Bool(true)
	return f
}

func repeated(f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
	f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return f
}

func mapField(name string, number int32, entryType string) *descriptorpb.FieldDescriptorProto {
	return repeated(msg(name, number, entryType))
}

func mapEntryScalar(name string) *descriptorpb.DescriptorProto {
	entry := message(name, str("key", 1), str("value", 2))
	entry.Options = &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)}
	return entry
}

func mapEntry(name, valueType string) *descriptorpb.DescriptorProto {
	entry := message(name, str("key", 1), msg("value", 2, valueType))
	entry.Options = &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)}
	return entry
}

func streamingMethod(name, in, out string, client, server bool) *descriptorpb.MethodDescriptorProto {
	m := method(name, in, out)
	m.ClientStreaming = proto.Bool(client)
	m.ServerStreaming = proto.Bool(server)
	return m
}
