package catalogtest

import (
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const Package = "shrt.test.v1"

func Descriptor() []byte { return descriptor(file()) }

func New() *catalog.Catalog { return parse(Descriptor()) }

func descriptor(files ...*descriptorpb.FileDescriptorProto) []byte {
	raw, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: files})
	if err != nil {
		panic(err)
	}
	return raw
}

func parse(raw []byte) *catalog.Catalog {
	cat, err := catalog.Parse(raw)
	if err != nil {
		panic(err)
	}
	return cat
}

func file() *descriptorpb.FileDescriptorProto {
	return protoFile("shrt/test/v1/test.proto", Package, nil, enums(enum("Kind", "KIND_UNSPECIFIED", "KIND_A", "KIND_B")), messages(
		message("ErrorMessage", str("code", 1), str("message", 2)),
		message("LoginRequest", str("username", 1), str("password", 2), str("old_password", 3), str("new_password", 4)),
		message("LoginResponse", msg("error", 1, ".shrt.test.v1.ErrorMessage"), str("access_token", 2), int64Field("expires_at", 3)),
		message("Meta", str("source", 1), str("trace_id", 2)),
		message("CreateRequest", str("name", 1), enumField("kind", 2, ".shrt.test.v1.Kind"), str("idempotency_key", 3), msg("meta", 4, ".shrt.test.v1.Meta"), int64Field("qty", 5)),
		message("CreateResponse", msg("error", 1, ".shrt.test.v1.ErrorMessage"), str("id", 2), str("name", 3), int32Field("total", 4)),
		message("FetchRequest", str("id", 1)),
		message("FetchResponse", msg("error", 1, ".shrt.test.v1.ErrorMessage"), str("id", 2), str("name", 3), str("created_at", 4), int32Field("total", 5)),
	),
		service("AuthService", method("Login", ".shrt.test.v1.LoginRequest", ".shrt.test.v1.LoginResponse")),
		service("PartnerAuthService", method("Login", ".shrt.test.v1.LoginRequest", ".shrt.test.v1.LoginResponse")),
		service("PartnerService", method("FetchMine", ".shrt.test.v1.FetchRequest", ".shrt.test.v1.FetchResponse")),
		service("ThingService",
			method("Create", ".shrt.test.v1.CreateRequest", ".shrt.test.v1.CreateResponse"),
			method("Fetch", ".shrt.test.v1.FetchRequest", ".shrt.test.v1.FetchResponse"),
		),
	)
}

func protoFile(name, pkg string, deps []string, enumTypes []*descriptorpb.EnumDescriptorProto, messageTypes []*descriptorpb.DescriptorProto, services ...*descriptorpb.ServiceDescriptorProto) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{Name: proto.String(name), Package: proto.String(pkg), Syntax: proto.String("proto3"),
		Dependency: deps, EnumType: enumTypes, MessageType: messageTypes, Service: services}
}

func messages(ms ...*descriptorpb.DescriptorProto) []*descriptorpb.DescriptorProto { return ms }

func enums(es ...*descriptorpb.EnumDescriptorProto) []*descriptorpb.EnumDescriptorProto { return es }

func message(name string, fields ...*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{Name: proto.String(name), Field: fields}
}

func enum(name string, values ...string) *descriptorpb.EnumDescriptorProto {
	e := &descriptorpb.EnumDescriptorProto{Name: proto.String(name)}
	for i, v := range values {
		e.Value = append(e.Value, &descriptorpb.EnumValueDescriptorProto{
			Name: proto.String(v), Number: proto.Int32(int32(i)),
		})
	}
	return e
}

func service(name string, methods ...*descriptorpb.MethodDescriptorProto) *descriptorpb.ServiceDescriptorProto {
	return &descriptorpb.ServiceDescriptorProto{Name: proto.String(name), Method: methods}
}

func method(name, in, out string) *descriptorpb.MethodDescriptorProto {
	return &descriptorpb.MethodDescriptorProto{
		Name: proto.String(name), InputType: proto.String(in), OutputType: proto.String(out),
	}
}

func field(name string, number int32, t descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		JsonName: proto.String(jsonName(name)),
		Number:   proto.Int32(number),
		Type:     t.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

func jsonName(protoName string) string {
	parts := strings.Split(protoName, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func str(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return field(name, number, descriptorpb.FieldDescriptorProto_TYPE_STRING)
}

func int64Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return field(name, number, descriptorpb.FieldDescriptorProto_TYPE_INT64)
}

func int32Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return field(name, number, descriptorpb.FieldDescriptorProto_TYPE_INT32)
}

func msg(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, number, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
	f.TypeName = proto.String(typeName)
	return f
}

func enumField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, number, descriptorpb.FieldDescriptorProto_TYPE_ENUM)
	f.TypeName = proto.String(typeName)
	return f
}
