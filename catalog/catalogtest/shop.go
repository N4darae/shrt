package catalogtest

import (
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

func ShopDescriptor() []byte {
	return shopDescriptor(shopFile("shop.common.v1", nil, messages(message("Status", str("code", 1), str("message", 2)))))
}

func shopDescriptor(common *descriptorpb.FileDescriptorProto) []byte {
	return descriptor(common, shopCatalogFile(), shopCustomersFile(), shopOrdersFile())
}

func Shop() *catalog.Catalog { return parse(ShopDescriptor()) }

func ShopWithErrorDetails() *catalog.Catalog {
	return parse(shopDescriptor(shopFile("shop.common.v1", nil, messages(
		message("ErrorDetail", int64Field("app_code", 1), str("reason", 2)),
		message("Status", str("code", 1), str("message", 2), repeated(msg("details", 3, ".shop.common.v1.ErrorDetail"))),
	))))
}

func shopFile(pkg string, deps []string, messageTypes []*descriptorpb.DescriptorProto, services ...*descriptorpb.ServiceDescriptorProto) *descriptorpb.FileDescriptorProto {
	return protoFile(pkg+".proto", pkg, deps, nil, messageTypes, services...)
}

func shopCatalogFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.catalog.v1", []string{"shop.common.v1.proto"}, messages(
		message("Product", str("id_product", 1), str("sku", 2), int64Field("price_minor", 3), int64Field("qty_on_hand", 4)),
		message("CreateProductRequest", str("sku", 1), str("name", 2), int64Field("price_minor", 3)),
		message("CreateProductResponse", msg("status", 1, ".shop.common.v1.Status"), msg("product", 2, ".shop.catalog.v1.Product")),
		message("GetProductRequest", str("id_product", 1)),
		message("GetProductResponse", msg("status", 1, ".shop.common.v1.Status"), msg("product", 2, ".shop.catalog.v1.Product")),
		message("ListProductsRequest", str("sku_prefix", 1)),
		message("ListProductsResponse", msg("status", 1, ".shop.common.v1.Status"), repeated(msg("products", 2, ".shop.catalog.v1.Product"))),
		message("AddStockRequest", str("id_product", 1), int64Field("qty", 2)),
		message("AddStockResponse", msg("status", 1, ".shop.common.v1.Status"), int64Field("qty_on_hand", 2)),
		message("AddStockBatchRequest", repeated(msg("lines", 1, ".shop.catalog.v1.AddStockRequest"))),
		message("StockResult", str("id_product", 1), int64Field("qty_on_hand", 2)),
		message("AddStockBatchResponse", msg("status", 1, ".shop.common.v1.Status"), repeated(msg("results", 2, ".shop.catalog.v1.StockResult"))),
	),
		service("ProductService",
			method("CreateProduct", ".shop.catalog.v1.CreateProductRequest", ".shop.catalog.v1.CreateProductResponse"),
			method("GetProduct", ".shop.catalog.v1.GetProductRequest", ".shop.catalog.v1.GetProductResponse"),
			method("ListProducts", ".shop.catalog.v1.ListProductsRequest", ".shop.catalog.v1.ListProductsResponse"),
		),
		service("StockService",
			method("AddStock", ".shop.catalog.v1.AddStockRequest", ".shop.catalog.v1.AddStockResponse"),
			method("AddStockBatch", ".shop.catalog.v1.AddStockBatchRequest", ".shop.catalog.v1.AddStockBatchResponse"),
		),
	)
}

func shopCustomersFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.customers.v1", []string{"shop.common.v1.proto"}, messages(
		message("Customer", str("id_customer", 1), str("email", 2)),
		message("CreateCustomerRequest", str("email", 1)),
		message("CreateCustomerResponse", msg("status", 1, ".shop.common.v1.Status"), msg("customer", 2, ".shop.customers.v1.Customer")),
	),
		service("CustomerService", method("CreateCustomer", ".shop.customers.v1.CreateCustomerRequest", ".shop.customers.v1.CreateCustomerResponse")),
	)
}

func shopOrdersFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.orders.v1", []string{"shop.common.v1.proto"}, messages(
		message("OrderLine", str("id_product", 1), int64Field("qty", 2)),
		message("Order", str("id_order", 1), str("id_customer", 2), repeated(msg("lines", 3, ".shop.orders.v1.OrderLine")), int64Field("total_minor", 4)),
		message("CreateOrderRequest", str("id_customer", 1), repeated(msg("lines", 2, ".shop.orders.v1.OrderLine")), str("idempotency_key", 3)),
		message("OrderResponse", msg("status", 1, ".shop.common.v1.Status"), msg("order", 2, ".shop.orders.v1.Order")),
		message("OrderIDRequest", str("id_order", 1)),
	),
		service("OrderService",
			method("CreateOrder", ".shop.orders.v1.CreateOrderRequest", ".shop.orders.v1.OrderResponse"),
			method("ConfirmOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			method("CancelOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			method("FetchOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			streamingMethod("WatchOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse", false, true),
		),
	)
}
