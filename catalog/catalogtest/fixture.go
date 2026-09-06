package catalogtest

import (
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const Package = "shrt.test.v1"

func Descriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func New() *catalog.Catalog {
	cat, err := catalog.Parse(Descriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func file() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/test/v1/test.proto"),
		Package: proto.String(Package),
		Syntax:  proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{
			enum("Kind", "KIND_UNSPECIFIED", "KIND_A", "KIND_B"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			message("ErrorMessage",
				str("code", 1),
				str("message", 2),
			),
			message("LoginRequest",
				str("username", 1),
				str("password", 2),
				str("old_password", 3),
				str("new_password", 4),
			),
			message("LoginResponse",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				str("access_token", 2),
				num("expires_at", 3, descriptorpb.FieldDescriptorProto_TYPE_INT64),
			),
			message("Meta",
				str("source", 1),
				str("trace_id", 2),
			),
			message("CreateRequest",
				str("name", 1),
				enumField("kind", 2, ".shrt.test.v1.Kind"),
				str("idempotency_key", 3),
				msg("meta", 4, ".shrt.test.v1.Meta"),
			),
			message("CreateResponse",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				str("id", 2),
			),
			message("FetchRequest",
				str("id", 1),
			),
			message("FetchResponse",
				msg("error", 1, ".shrt.test.v1.ErrorMessage"),
				str("id", 2),
				str("name", 3),
				str("created_at", 4),
			),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("AuthService",
				method("Login", ".shrt.test.v1.LoginRequest", ".shrt.test.v1.LoginResponse"),
			),
			service("PartnerAuthService",
				method("Login", ".shrt.test.v1.LoginRequest", ".shrt.test.v1.LoginResponse"),
			),
			service("PartnerService",
				method("FetchMine", ".shrt.test.v1.FetchRequest", ".shrt.test.v1.FetchResponse"),
			),
			service("ThingService",
				method("Create", ".shrt.test.v1.CreateRequest", ".shrt.test.v1.CreateResponse"),
				method("Fetch", ".shrt.test.v1.FetchRequest", ".shrt.test.v1.FetchResponse"),
			),
		},
	}
}

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

func num(name string, number int32, t descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return field(name, number, t)
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
