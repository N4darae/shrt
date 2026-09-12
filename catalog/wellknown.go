package catalog

import (
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

var wellKnownForms = map[string]string{
	"google.protobuf.Timestamp":   `RFC3339 string, e.g. "2026-09-12T00:00:00Z"`,
	"google.protobuf.Duration":    `seconds with an "s" suffix, e.g. "3.5s"`,
	"google.protobuf.FieldMask":   `comma-joined lowerCamelCase paths, e.g. "dueAt,note"`,
	"google.protobuf.Struct":      "JSON object",
	"google.protobuf.Value":       "any JSON value; null when unset",
	"google.protobuf.ListValue":   "JSON array",
	"google.protobuf.Empty":       "{}",
	"google.protobuf.Any":         `JSON object carrying "@type": an empty {} is accepted, anything else needs the type URL`,
	"google.protobuf.DoubleValue": "bare number, or null",
	"google.protobuf.FloatValue":  "bare number, or null",
	"google.protobuf.Int64Value":  `string number, e.g. "0", or null`,
	"google.protobuf.UInt64Value": `string number, e.g. "0", or null`,
	"google.protobuf.Int32Value":  "bare number, or null",
	"google.protobuf.UInt32Value": "bare number, or null",
	"google.protobuf.BoolValue":   "bare true/false, or null",
	"google.protobuf.StringValue": "bare string, or null",
	"google.protobuf.BytesValue":  "base64 string, or null",
}

func WellKnownForm(md protoreflect.MessageDescriptor) (string, bool) {
	form, ok := wellKnownForms[string(md.FullName())]
	return form, ok
}

func wellKnownValue(md protoreflect.MessageDescriptor) (any, bool) {
	if _, ok := wellKnownForms[string(md.FullName())]; !ok {
		return nil, false
	}
	raw, err := protojson.Marshal(dynamicpb.NewMessage(md))
	if err != nil {
		return nil, true
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, true
	}
	return v, true
}
