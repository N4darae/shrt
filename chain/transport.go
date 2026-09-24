package chain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
)

const (
	TransportOK     = "ok"
	TransportPrefix = "transport"
)

var TransportFields = map[string]string{
	"code":        "The Connect error code of a refused call (`unauthenticated`, `permission_denied`, …), `http_<status>` when the error body is not Connect JSON, or `" + TransportOK + "` when the call was answered 200. Always present.",
	"http_status": "The HTTP status the backend answered with, as a number. Always present.",
	"message":     "The Connect error message of a refused call. ABSENT when the call succeeded, so `exists: false` asserts success at this layer.",
}

func TransportFieldNames() []string {
	out := make([]string, 0, len(TransportFields))
	for name := range TransportFields {
		out = append(out, TransportPrefix+"."+name)
	}
	sort.Strings(out)
	return out
}

func IsTransportPath(path string) bool {
	head, _, _ := strings.Cut(path, ".")
	return head == TransportPrefix
}

func KnownTransportPath(path string) bool {
	head, rest, ok := strings.Cut(path, ".")
	if !ok || head != TransportPrefix {
		return false
	}
	_, known := TransportFields[rest]
	return known
}

func TransportOutcome(status int, code, message string) map[string]any {
	inner := map[string]any{"http_status": float64(status)}
	if code == "" {
		inner["code"] = TransportOK
		return map[string]any{TransportPrefix: inner}
	}
	inner["code"] = code
	inner["message"] = message
	return map[string]any{TransportPrefix: inner}
}

func HasTransportExpectation(expect []Expectation) bool {
	for _, e := range expect {
		if IsTransportPath(e.Path) {
			return true
		}
	}
	return false
}

const InvalidTokenAuth = "invalid"

func ExpectsTransportRefusal(e Expectation) bool {
	var ok string
	switch e.Path {
	case TransportPrefix + ".code":
		ok = TransportOK
	case TransportPrefix + ".http_status":
		ok = "200"
	default:
		return false
	}
	if e.Equals != nil && stringify(e.Equals) != ok {
		return true
	}
	return e.NotEqual != nil && stringify(e.NotEqual) == ok
}

func alwaysPresentTransportPath(path string) bool {
	return path == TransportPrefix+".code" || path == TransportPrefix+".http_status"
}

func transportHint(path string) string {
	switch SplitPath(path)[0] {
	case "code", "message", "http_status", "status":
		return fmt.Sprintf(". If this is the refusal a Connect error carries (HTTP 4xx/5xx with a "+
			"{\"code\", \"message\"} body) rather than a field of the response, assert it with one of %s",
			strings.Join(TransportFieldNames(), ", "))
	}
	return ""
}

func lintTransport(s *Step, m *catalog.Method) []Issue {
	issues := []Issue{}
	shadowed := catalog.HasPath(catalog.DescribeMessage(m.Output()).Fields, []string{TransportPrefix})
	for _, e := range s.Expect {
		if !IsTransportPath(e.Path) {
			continue
		}
		if !KnownTransportPath(e.Path) {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
				"expect on %q: %s. is reserved for the recorded transport result, and the only paths "+
					"under it are %s — this one can never be present",
				e.Path, TransportPrefix, strings.Join(TransportFieldNames(), ", "))})
			continue
		}
		if shadowed {
			issues = append(issues, Issue{Step: s.ID, Severity: SeverityWarn, Message: fmt.Sprintf(
				"expect on %q reads the transport result, not the field %q that %s also declares — the "+
					"reserved path wins, so the response field cannot be asserted under that name",
				e.Path, TransportPrefix, m.Output().FullName())})
		}
	}
	if s.Auth == InvalidTokenAuth && len(s.Export) > 0 {
		issues = append(issues, Issue{Step: s.ID, Severity: SeverityError, Message: fmt.Sprintf(
			"auth: %s sends a token the backend never issued, to prove it is refused, so there is no "+
				"response to export from. Export from a step that runs as a real principal",
			InvalidTokenAuth)})
	}
	return issues
}
