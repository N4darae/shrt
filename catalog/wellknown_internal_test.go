package catalog

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestEveryWellKnownPlaceholderIsAcceptedByProtojson(t *testing.T) {
	covered := map[string]bool{}
	for _, m := range []proto.Message{
		&timestamppb.Timestamp{}, &durationpb.Duration{}, &fieldmaskpb.FieldMask{},
		&structpb.Struct{}, &structpb.Value{}, &structpb.ListValue{},
		&emptypb.Empty{}, &anypb.Any{},
		&wrapperspb.DoubleValue{}, &wrapperspb.FloatValue{}, &wrapperspb.Int64Value{},
		&wrapperspb.UInt64Value{}, &wrapperspb.Int32Value{}, &wrapperspb.UInt32Value{},
		&wrapperspb.BoolValue{}, &wrapperspb.StringValue{}, &wrapperspb.BytesValue{},
	} {
		md := m.ProtoReflect().Descriptor()
		name := string(md.FullName())
		covered[name] = true

		value, ok := wellKnownValue(md)
		if !ok {
			t.Errorf("%s is not treated as a well-known type", name)
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Errorf("%s: placeholder is not JSON encodable: %v", name, err)
			continue
		}
		if err := protojson.Unmarshal(raw, dynamicpb.NewMessage(md)); err != nil {
			t.Errorf("%s: protojson rejects the placeholder %s: %v", name, raw, err)
		}
		if form, ok := WellKnownForm(md); !ok || form == "" {
			t.Errorf("%s has no JSON form to show a reader", name)
		}
	}
	for name := range wellKnownForms {
		if !covered[name] {
			t.Errorf("%s is claimed as well-known but no test exercises it", name)
		}
	}
}
