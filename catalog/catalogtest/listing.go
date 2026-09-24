package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

const ListingPackage = "shrt.listing.v1"

func ListingDescriptor() []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{listingFile()}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Listing() *catalog.Catalog {
	cat, err := catalog.Parse(ListingDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func listingFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("shrt/listing/v1/listing.proto"),
		Package: proto.String(ListingPackage),
		Syntax:  proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{
			enum("WidgetState", "WIDGET_STATE_UNSPECIFIED", "WIDGET_STATE_OPEN", "WIDGET_STATE_CLOSED"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			message("Outcome", str("code", 1), str("message", 2)),
			message("Note", str("code", 1), str("text", 2)),
			message("Request", str("id", 1)),
			message("Widget",
				str("id", 1),
				enumField("status", 2, ".shrt.listing.v1.WidgetState"),
			),
			message("Gadget",
				str("id", 1),
				str("status", 2),
			),
			message("ListWidgetsResponse",
				msg("result", 1, ".shrt.listing.v1.Outcome"),
				repeated(msg("widgets", 2, ".shrt.listing.v1.Widget")),
			),
			message("SearchWidgetsResponse",
				msg("result", 1, ".shrt.listing.v1.Outcome"),
				repeated(msg("widgets", 2, ".shrt.listing.v1.Widget")),
			),
			message("ListGadgetsResponse",
				msg("result", 1, ".shrt.listing.v1.Outcome"),
				repeated(msg("gadgets", 2, ".shrt.listing.v1.Gadget")),
			),
			message("Annotated",
				str("id", 1),
				msg("result", 2, ".shrt.listing.v1.Note"),
			),
			message("ListAnnotatedResponse",
				msg("result", 1, ".shrt.listing.v1.Outcome"),
				repeated(msg("annotated", 2, ".shrt.listing.v1.Annotated")),
			),
			message("EntryResult",
				str("id", 1),
				msg("result", 2, ".shrt.listing.v1.Outcome"),
			),
			message("SubmitEntriesResponse",
				msg("result", 1, ".shrt.listing.v1.Outcome"),
				repeated(msg("entries", 2, ".shrt.listing.v1.EntryResult")),
			),
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			service("WidgetService",
				method("ListWidgets", ".shrt.listing.v1.Request", ".shrt.listing.v1.ListWidgetsResponse"),
				method("SearchWidgets", ".shrt.listing.v1.Request", ".shrt.listing.v1.SearchWidgetsResponse"),
				method("ListGadgets", ".shrt.listing.v1.Request", ".shrt.listing.v1.ListGadgetsResponse"),
				method("ListAnnotated", ".shrt.listing.v1.Request", ".shrt.listing.v1.ListAnnotatedResponse"),
				method("SubmitEntries", ".shrt.listing.v1.Request", ".shrt.listing.v1.SubmitEntriesResponse"),
			),
		},
	}
}
