package runner

import (
	"encoding/json"
	"testing"
)

func TestJSONBodyOrString_NonJSONBodyStaysMarshalable(t *testing.T) {
	got := jsonBodyOrString([]byte("404 page not found\n"))
	if _, err := json.Marshal(struct {
		Response json.RawMessage `json:"response"`
	}{Response: got}); err != nil {
		t.Fatalf("a run record must stay writable when the server answers with a non-JSON body: %v", err)
	}
	var back string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("non-JSON body should be recorded as a JSON string: %v", err)
	}
	if back != "404 page not found\n" {
		t.Fatalf("body text not preserved: %q", back)
	}
}

func TestJSONBodyOrString_JSONBodyIsUntouched(t *testing.T) {
	in := []byte(`{"error":{"code":"internal"}}`)
	got := jsonBodyOrString(in)
	if string(got) != string(in) {
		t.Fatalf("a JSON body must be recorded byte-for-byte, got %s", got)
	}
}

func TestJSONBodyOrString_EmptyBodyIsOmittable(t *testing.T) {
	if got := jsonBodyOrString(nil); got != nil {
		t.Fatalf("an empty body should stay empty so omitempty drops the field, got %s", got)
	}
}
