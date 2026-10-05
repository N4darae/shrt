package catalogtest

import (
	"github.com/N4darae/shrt/catalog"
)

const MinorityPackage = "shrt.minority.v1"

func Minority() *catalog.Catalog {
	return parse(descriptor(protoFile("shrt/minority/v1/minority.proto", MinorityPackage, nil, nil, messages(
		message("Err", str("code", 1), str("message", 2)),
		message("Stat", str("code", 1)),
		message("Req", str("id", 1)),
		message("MajorityA", msg("error", 1, ".shrt.minority.v1.Err"), str("a", 2)),
		message("MajorityB", msg("error", 1, ".shrt.minority.v1.Err"), str("b", 2)),
		message("MajorityC", msg("error", 1, ".shrt.minority.v1.Err"), str("c", 2)),
		message("Odd", msg("status", 1, ".shrt.minority.v1.Stat"), str("d", 2)),
	),
		service("MixedService",
			method("A", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityA"),
			method("B", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityB"),
			method("C", ".shrt.minority.v1.Req", ".shrt.minority.v1.MajorityC"),
			method("D", ".shrt.minority.v1.Req", ".shrt.minority.v1.Odd"),
		),
	)))
}
