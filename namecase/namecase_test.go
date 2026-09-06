package namecase_test

import (
	"testing"

	"github.com/N4darae/shrt/namecase"
)

func TestFoldErasesSeparatorsAndCase(t *testing.T) {
	cases := map[string]string{
		"access_token": "accesstoken",
		"accessToken":  "accesstoken",
		"ACCESS-TOKEN": "accesstoken",
		"AccessToken":  "accesstoken",
		"id":           "id",
		"":             "",
		"_":            "",
	}
	for in, want := range cases {
		if got := namecase.Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEqualMatchesProtoNamesAgainstProtojsonOutput(t *testing.T) {
	same := [][2]string{
		{"access_token", "accessToken"},
		{"expires_at", "expiresAt"},
		{"id_book", "idBook"},
		{"error", "error"},
	}
	for _, pair := range same {
		if !namecase.Equal(pair[0], pair[1]) {
			t.Errorf("%q and %q must compare equal", pair[0], pair[1])
		}
	}
	differ := [][2]string{
		{"access_token", "refreshToken"},
		{"id_book", "id_books"},
		{"qty", "quantity"},
	}
	for _, pair := range differ {
		if namecase.Equal(pair[0], pair[1]) {
			t.Errorf("%q and %q must not compare equal", pair[0], pair[1])
		}
	}
}

func TestLookupKeyFindsACamelCaseKeyByItsProtoName(t *testing.T) {
	body := map[string]any{"accessToken": "t", "expiresAt": "1", "error": nil}

	key, ok := namecase.LookupKey(body, "access_token")
	if !ok || key != "accessToken" {
		t.Fatalf("LookupKey(access_token) = %q, %v; want accessToken, true", key, ok)
	}
	if key, ok := namecase.LookupKey(body, "error"); !ok || key != "error" {
		t.Fatalf("an exact key must be returned unchanged, got %q %v", key, ok)
	}
	if _, ok := namecase.LookupKey(body, "refresh_token"); ok {
		t.Fatal("a missing key must not resolve")
	}
	if _, ok := namecase.LookupKey(map[string]any{}, "access_token"); ok {
		t.Fatal("an empty body must not resolve anything")
	}
}

func TestLookupKeyPrefersAnExactMatchOverAFoldedOne(t *testing.T) {
	body := map[string]any{"access_token": "snake", "accessToken": "camel"}
	if key, _ := namecase.LookupKey(body, "access_token"); key != "access_token" {
		t.Fatalf("exact key must win, got %q", key)
	}
	if key, _ := namecase.LookupKey(body, "accessToken"); key != "accessToken" {
		t.Fatalf("exact key must win, got %q", key)
	}
}
