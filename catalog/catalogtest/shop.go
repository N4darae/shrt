package catalogtest

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/N4darae/shrt/catalog"
)

func ShopDescriptor() []byte {
	return shopDescriptor(shopCommonFile())
}

func shopDescriptor(common *descriptorpb.FileDescriptorProto) []byte {
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		common, shopCatalogFile(), shopCustomersFile(), shopOrdersFile(),
	}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		panic(err)
	}
	return raw
}

func Shop() *catalog.Catalog {
	cat, err := catalog.Parse(ShopDescriptor())
	if err != nil {
		panic(err)
	}
	return cat
}

func ShopWithErrorDetails() *catalog.Catalog {
	common := shopFile("shop.common.v1", nil, []*descriptorpb.DescriptorProto{
		message("ErrorDetail", int64Field("app_code", 1), str("reason", 2)),
		message("Status", str("code", 1), str("message", 2), repeated(msg("details", 3, ".shop.common.v1.ErrorDetail"))),
	})
	cat, err := catalog.Parse(shopDescriptor(common))
	if err != nil {
		panic(err)
	}
	return cat
}

func int64Field(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return num(name, number, descriptorpb.FieldDescriptorProto_TYPE_INT64)
}

func shopFile(pkg string, deps []string, messages []*descriptorpb.DescriptorProto, services ...*descriptorpb.ServiceDescriptorProto) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String(pkg + ".proto"),
		Package:     proto.String(pkg),
		Syntax:      proto.String("proto3"),
		Dependency:  deps,
		MessageType: messages,
		Service:     services,
	}
}

func shopCommonFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.common.v1", nil, []*descriptorpb.DescriptorProto{
		message("Status", str("code", 1), str("message", 2)),
	})
}

func shopCatalogFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.catalog.v1", []string{"shop.common.v1.proto"}, []*descriptorpb.DescriptorProto{
		message("Product", str("id_product", 1), str("sku", 2), int64Field("price_minor", 3)),
		message("CreateProductRequest", str("sku", 1), str("name", 2), int64Field("price_minor", 3)),
		message("CreateProductResponse", msg("status", 1, ".shop.common.v1.Status"), msg("product", 2, ".shop.catalog.v1.Product")),
		message("GetProductRequest", str("id_product", 1)),
		message("GetProductResponse", msg("status", 1, ".shop.common.v1.Status"), msg("product", 2, ".shop.catalog.v1.Product")),
		message("ListProductsRequest", str("sku_prefix", 1)),
		message("ListProductsResponse", msg("status", 1, ".shop.common.v1.Status"), repeated(msg("products", 2, ".shop.catalog.v1.Product"))),
		message("AddStockRequest", str("id_product", 1), int64Field("qty", 2)),
		message("AddStockResponse", msg("status", 1, ".shop.common.v1.Status"), int64Field("qty_on_hand", 2)),
	},
		service("ProductService",
			method("CreateProduct", ".shop.catalog.v1.CreateProductRequest", ".shop.catalog.v1.CreateProductResponse"),
			method("GetProduct", ".shop.catalog.v1.GetProductRequest", ".shop.catalog.v1.GetProductResponse"),
			method("ListProducts", ".shop.catalog.v1.ListProductsRequest", ".shop.catalog.v1.ListProductsResponse"),
		),
		service("StockService",
			method("AddStock", ".shop.catalog.v1.AddStockRequest", ".shop.catalog.v1.AddStockResponse"),
		),
	)
}

func shopCustomersFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.customers.v1", []string{"shop.common.v1.proto"}, []*descriptorpb.DescriptorProto{
		message("Customer", str("id_customer", 1), str("email", 2)),
		message("CreateCustomerRequest", str("email", 1)),
		message("CreateCustomerResponse", msg("status", 1, ".shop.common.v1.Status"), msg("customer", 2, ".shop.customers.v1.Customer")),
	},
		service("CustomerService",
			method("CreateCustomer", ".shop.customers.v1.CreateCustomerRequest", ".shop.customers.v1.CreateCustomerResponse"),
		),
	)
}

func shopOrdersFile() *descriptorpb.FileDescriptorProto {
	return shopFile("shop.orders.v1", []string{"shop.common.v1.proto"}, []*descriptorpb.DescriptorProto{
		message("OrderLine", str("id_product", 1), int64Field("qty", 2)),
		message("Order", str("id_order", 1), str("id_customer", 2), repeated(msg("lines", 3, ".shop.orders.v1.OrderLine")), int64Field("total_minor", 4)),
		message("CreateOrderRequest", str("id_customer", 1), repeated(msg("lines", 2, ".shop.orders.v1.OrderLine")), str("idempotency_key", 3)),
		message("OrderResponse", msg("status", 1, ".shop.common.v1.Status"), msg("order", 2, ".shop.orders.v1.Order")),
		message("OrderIDRequest", str("id_order", 1)),
	},
		service("OrderService",
			method("CreateOrder", ".shop.orders.v1.CreateOrderRequest", ".shop.orders.v1.OrderResponse"),
			method("ConfirmOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			method("CancelOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			method("FetchOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse"),
			streamingMethod("WatchOrder", ".shop.orders.v1.OrderIDRequest", ".shop.orders.v1.OrderResponse", false, true),
		),
	)
}
