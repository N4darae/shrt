package catalogtest

import (
	"github.com/N4darae/shrt/catalog"
)

const StampedPackage = "shrt.stamped.v1"

func Stamped() *catalog.Catalog {
	return parse(descriptor(protoFile("shrt/stamped/v1/stamped.proto", StampedPackage, nil, nil, messages(
		message("Status", str("code", 1), str("reason", 2)),
		message("Item", str("id_item", 1), str("name", 2), str("created_at", 3), str("updated_at", 4), str("expires_at", 5)),
		message("CreateItemRequest", str("name", 1)),
		message("ItemIDRequest", str("id_item", 1)),
		message("ItemResponse", msg("status", 1, ".shrt.stamped.v1.Status"), msg("item", 2, ".shrt.stamped.v1.Item")),
	),
		service("ItemService",
			method("CreateItem", ".shrt.stamped.v1.CreateItemRequest", ".shrt.stamped.v1.ItemResponse"),
			method("GetItem", ".shrt.stamped.v1.ItemIDRequest", ".shrt.stamped.v1.ItemResponse"),
		),
	)))
}
