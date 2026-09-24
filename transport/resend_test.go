package transport

import (
	"context"
	"strings"
	"testing"
)

func unauthOnce(sent *int) Handler {
	return func(ctx context.Context, call *Call) (*Result, error) {
		*sent++
		if *sent == 1 {
			return &Result{Status: 401, Error: &Error{Code: "unauthenticated", Message: "token expired"}}, nil
		}
		return &Result{Status: 200}, nil
	}
}

func isRead(procedure string) bool { return strings.Contains(procedure, "/Get") }

func TestAWriteAnswered401IsNotResent(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	sent := 0
	router := AuthRouter{Profiles: []*AuthProfile{{Name: DefaultProfile, Source: src}}, Default: DefaultProfile, Resend: isRead}
	call := &Call{Procedure: "/shop.v1.StockService/AddStock"}
	res, err := WithAuthRouter(router)(unauthOnce(&sent))(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("a write answered 401 may already have been performed; re-sending it performs it twice, sent %d time(s)", sent)
	}
	if res.Status != 401 {
		t.Fatalf("the step must see the 401 it got, got %d", res.Status)
	}
	if src.invalidated != 1 {
		t.Fatalf("the rejected token must still be dropped so the next call logs in fresh, invalidated=%d", src.invalidated)
	}
	if got, _ := call.Meta[MetaAuthRetry].(string); got != AuthRetryNotResent {
		t.Fatalf("the call must say it was not re-sent, meta=%v", call.Meta)
	}
}

func TestARead401IsResentAndSaysSo(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	sent := 0
	router := AuthRouter{Profiles: []*AuthProfile{{Name: DefaultProfile, Source: src}}, Default: DefaultProfile, Resend: isRead}
	call := &Call{Procedure: "/shop.v1.ProductService/GetProduct"}
	res, err := WithAuthRouter(router)(unauthOnce(&sent))(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 2 || res.Status != 200 {
		t.Fatalf("a read is safe to re-send after a fresh login: sent=%d status=%d", sent, res.Status)
	}
	if got, _ := call.Meta[MetaAuthRetry].(string); got != AuthRetryResent {
		t.Fatalf("the call must say it was re-sent, meta=%v", call.Meta)
	}
}

func TestWithoutAReadTestNothingIsResent(t *testing.T) {
	src := &fakeTokenSource{token: "tok-1"}
	sent := 0
	router := AuthRouter{Profiles: []*AuthProfile{{Name: DefaultProfile, Source: src}}, Default: DefaultProfile}
	if _, err := WithAuthRouter(router)(unauthOnce(&sent))(context.Background(), &Call{Procedure: "/x.v1.S/GetThing"}); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("with no way to tell a read, nothing is re-sent, sent %d", sent)
	}
}
