package catalogtest

import (
	"github.com/N4darae/shrt/catalog"
)

const ForeignPackage = "shrt.foreign.v1"

func Foreign() *catalog.Catalog {
	return parse(descriptor(protoFile("shrt/foreign/v1/foreign.proto", ForeignPackage, nil, nil, messages(
		message("Status", str("code", 1), str("message", 2)),
		message("Request", str("id", 1)),
		message("CreateResponse", msg("status", 1, ".shrt.foreign.v1.Status"), str("id", 2)),
		message("FetchResponse", msg("status", 1, ".shrt.foreign.v1.Status"), str("name", 2)),
		message("Line", msg("status", 1, ".shrt.foreign.v1.Status"), str("amount", 2)),
		message("BatchResponse", msg("status", 1, ".shrt.foreign.v1.Status"), repeated(msg("lines", 2, ".shrt.foreign.v1.Line"))),
	),
		service("ForeignService",
			method("Create", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.CreateResponse"),
			method("Fetch", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.FetchResponse"),
			method("Batch", ".shrt.foreign.v1.Request", ".shrt.foreign.v1.BatchResponse"),
		),
	)))
}
