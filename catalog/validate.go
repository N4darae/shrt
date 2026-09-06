package catalog

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func (c *Catalog) ValidateInput(m *Method, body []byte) error {
	return c.validate(m.Input(), body, "request")
}

func (c *Catalog) ValidateOutput(m *Method, body []byte) error {
	return c.validate(m.Output(), body, "response")
}

func (c *Catalog) validate(md protoreflect.MessageDescriptor, body []byte, side string) error {
	if len(body) == 0 {
		return nil
	}
	msg := dynamicpb.NewMessage(md)
	opts := protojson.UnmarshalOptions{Resolver: c.types, DiscardUnknown: false}
	if err := opts.Unmarshal(body, msg); err != nil {
		return fmt.Errorf("%s does not match %s: %w", side, md.FullName(), err)
	}
	return nil
}

func (c *Catalog) Canonicalize(md protoreflect.MessageDescriptor, body []byte) ([]byte, error) {
	full, _, err := c.CanonicalizeWithPresence(md, body)
	return full, err
}

func (c *Catalog) CanonicalizeWithPresence(md protoreflect.MessageDescriptor, body []byte) (full, present []byte, err error) {
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{Resolver: c.types}).Unmarshal(body, msg); err != nil {
		return nil, nil, err
	}
	full, err = protojson.MarshalOptions{
		Resolver:        c.types,
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	present, err = protojson.MarshalOptions{
		Resolver:      c.types,
		UseProtoNames: true,
	}.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	return full, present, nil
}
