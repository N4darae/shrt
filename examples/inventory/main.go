package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	inventoryv1 "github.com/N4darae/shrt/examples/inventory/gen/inventory/v1"
	"github.com/N4darae/shrt/examples/inventory/gen/inventory/v1/inventoryv1connect"
)

const (
	codeBadCredentials = 1001
	codeSkuTaken       = 1201
	codeInvalidQty     = 1203
	codeItemNotFound   = 1204
)

type server struct {
	release  int
	user     string
	password string

	mu     sync.Mutex
	tokens map[string]time.Time
	items  map[string]*inventoryv1.Item
	bySku  map[string]string
	seen   map[string]*inventoryv1.Item
	nextID int
}

func ok() *inventoryv1.Status { return &inventoryv1.Status{Code: "OK"} }

func refused(code int32, reason, message string) *inventoryv1.Status {
	return &inventoryv1.Status{
		Code:    "ERROR",
		Message: message,
		Details: []*inventoryv1.Detail{{AppCode: code, Reason: reason}},
	}
}

func clone(item *inventoryv1.Item) *inventoryv1.Item {
	return &inventoryv1.Item{IdItem: item.IdItem, Sku: item.Sku, Name: item.Name, QtyOnHand: item.QtyOnHand}
}

func (s *server) Login(_ context.Context, req *connect.Request[inventoryv1.LoginRequest]) (*connect.Response[inventoryv1.LoginResponse], error) {
	if req.Msg.Username != s.user || req.Msg.Password != s.password {
		return connect.NewResponse(&inventoryv1.LoginResponse{
			Status: refused(codeBadCredentials, "BadCredentials", "unknown user or wrong password"),
		}), nil
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	token := hex.EncodeToString(raw)
	expires := time.Now().Add(time.Hour)
	s.mu.Lock()
	s.tokens[token] = expires
	s.mu.Unlock()
	return connect.NewResponse(&inventoryv1.LoginResponse{Status: ok(), AccessToken: token, ExpiresAt: expires.Unix()}), nil
}

func (s *server) CreateItem(_ context.Context, req *connect.Request[inventoryv1.CreateItemRequest]) (*connect.Response[inventoryv1.CreateItemResponse], error) {
	sku, name := strings.TrimSpace(req.Msg.Sku), strings.TrimSpace(req.Msg.Name)
	if sku == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("sku is required"))
	}
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.bySku[sku]; taken {
		return connect.NewResponse(&inventoryv1.CreateItemResponse{
			Status: refused(codeSkuTaken, "SkuTaken", "another item already has this sku"),
		}), nil
	}
	s.nextID++
	item := &inventoryv1.Item{IdItem: fmt.Sprintf("item-%d", s.nextID), Sku: sku, Name: name}
	s.items[item.IdItem] = item
	s.bySku[sku] = item.IdItem
	return connect.NewResponse(&inventoryv1.CreateItemResponse{Status: ok(), Item: clone(item)}), nil
}

func (s *server) GetItem(_ context.Context, req *connect.Request[inventoryv1.GetItemRequest]) (*connect.Response[inventoryv1.GetItemResponse], error) {
	if req.Msg.IdItem == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id_item is required"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, found := s.items[req.Msg.IdItem]
	if !found {
		return connect.NewResponse(&inventoryv1.GetItemResponse{
			Status: refused(codeItemNotFound, "ItemNotFound", "no item has this id"),
		}), nil
	}
	if s.release >= 2 {
		if cached, hit := s.seen[item.IdItem]; hit {
			return connect.NewResponse(&inventoryv1.GetItemResponse{Status: ok(), Item: clone(cached)}), nil
		}
		s.seen[item.IdItem] = clone(item)
	}
	return connect.NewResponse(&inventoryv1.GetItemResponse{Status: ok(), Item: clone(item)}), nil
}

func (s *server) AddStock(_ context.Context, req *connect.Request[inventoryv1.AddStockRequest]) (*connect.Response[inventoryv1.AddStockResponse], error) {
	if req.Msg.IdItem == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id_item is required"))
	}
	if req.Msg.Qty <= 0 {
		return connect.NewResponse(&inventoryv1.AddStockResponse{
			Status: refused(codeInvalidQty, "InvalidQty", "qty must be greater than zero"),
		}), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, found := s.items[req.Msg.IdItem]
	if !found {
		return connect.NewResponse(&inventoryv1.AddStockResponse{
			Status: refused(codeItemNotFound, "ItemNotFound", "no item has this id"),
		}), nil
	}
	item.QtyOnHand += req.Msg.Qty
	return connect.NewResponse(&inventoryv1.AddStockResponse{Status: ok(), QtyOnHand: item.QtyOnHand}), nil
}

func (s *server) authenticate() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req.Spec().Procedure == inventoryv1connect.AuthServiceLoginProcedure {
				return next(ctx, req)
			}
			token, found := strings.CutPrefix(req.Header().Get("Authorization"), "Bearer ")
			s.mu.Lock()
			expires, known := s.tokens[token]
			s.mu.Unlock()
			if !found || !known || time.Now().After(expires) {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing, unknown or expired token"))
			}
			return next(ctx, req)
		}
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	release := flag.Int("release", 1, "1 is correct; 2 serves GetItem from a stale cache")
	user := flag.String("user", "demo", "the one account's username")
	password := flag.String("password", "demo-password", "the one account's password")
	flag.Parse()

	s := &server{
		release:  *release,
		user:     *user,
		password: *password,
		tokens:   map[string]time.Time{},
		items:    map[string]*inventoryv1.Item{},
		bySku:    map[string]string{},
		seen:     map[string]*inventoryv1.Item{},
	}
	auth := connect.WithInterceptors(s.authenticate())
	mux := http.NewServeMux()
	mux.Handle(inventoryv1connect.NewAuthServiceHandler(s, auth))
	mux.Handle(inventoryv1connect.NewItemServiceHandler(s, auth))
	log.Printf("inventory release %d listening on http://%s", *release, *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
