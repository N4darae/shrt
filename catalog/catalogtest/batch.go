package catalogtest

import (
	"github.com/N4darae/shrt/catalog"
)

func Batch() *catalog.Catalog {
	return parse(descriptor(file(), protoFile("shrt/test/v1/batch.proto", Package, []string{"shrt/test/v1/test.proto"}, nil, messages(
		message("BatchRequest", repeated(str("lines", 1))),
		message("PreviewResult", msg("error", 1, ".shrt.test.v1.ErrorMessage"), str("amount", 2)),
		message("PreviewResponse", msg("error", 1, ".shrt.test.v1.ErrorMessage"), repeated(msg("results", 2, ".shrt.test.v1.PreviewResult"))),
		message("ReceiptResult", str("id", 1)),
		message("ReceiptResponse", msg("error", 1, ".shrt.test.v1.ErrorMessage"), repeated(msg("results", 2, ".shrt.test.v1.ReceiptResult"))),
	),
		service("BatchService",
			method("Preview", ".shrt.test.v1.BatchRequest", ".shrt.test.v1.PreviewResponse"),
			method("Receipt", ".shrt.test.v1.BatchRequest", ".shrt.test.v1.ReceiptResponse"),
		),
	)))
}
