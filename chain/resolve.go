package chain

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var refPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

type Scope struct {
	Vars    map[string]any
	Exports map[string]any
	Steps   map[string]*StepView
	Now     func() time.Time
	Env     func(string) (string, bool)

	pinned time.Time
}

type StepView struct {
	Request   any
	Response  any
	Synthetic bool
}

func NewScope(vars map[string]any) *Scope {
	return &Scope{
		Vars:    cloneMap(vars),
		Exports: map[string]any{},
		Steps:   map[string]*StepView{},
		Now:     time.Now,
		Env:     os.LookupEnv,
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Scope) Record(id string, request, response any) {
	s.Steps[id] = &StepView{Request: request, Response: response}
}

func (s *Scope) RecordSynthetic(id string, request, response any) {
	s.Steps[id] = &StepView{Request: request, Response: response, Synthetic: true}
}

func (s *Scope) ResolveValue(v any) (any, error) {
	switch t := v.(type) {
	case string:
		return s.resolveString(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			resolved, err := s.ResolveValue(item)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			resolved, err := s.ResolveValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved)
		}
		return out, nil
	default:
		return v, nil
	}
}

func (s *Scope) resolveString(in string) (any, error) {
	matches := refPattern.FindAllStringSubmatchIndex(in, -1)
	if len(matches) == 0 {
		return in, nil
	}
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(in) {
		return s.lookup(in[matches[0][2]:matches[0][3]])
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(in[last:m[0]])
		val, err := s.lookup(in[m[2]:m[3]])
		if err != nil {
			return nil, err
		}
		b.WriteString(stringify(val))
		last = m[1]
	}
	b.WriteString(in[last:])
	return b.String(), nil
}

type RefKind int

const (
	RefStep RefKind = iota
	RefBare
	RefVars
	RefExports
	RefEnv
	RefUUID
	RefClock
)

type Ref struct {
	Expr   string
	Kind   RefKind
	Head   string
	Rest   string
	Offset time.Duration
	Err    error
}

func ParseRef(expr string) Ref {
	expr = strings.TrimSpace(expr)
	head, rest, _ := strings.Cut(expr, ".")
	r := Ref{Expr: expr, Head: head, Rest: rest}
	base, offset, err := splitClockOffset(head)
	if err != nil {
		r.Kind = RefClock
		r.Err = err
		return r
	}
	r.Offset = offset
	switch base {
	case "uuid":
		r.Kind = RefUUID
	case "now", "nowunix", "today":
		r.Kind, r.Head = RefClock, base
	case "vars":
		r.Kind = RefVars
	case "exports":
		r.Kind = RefExports
	case "env":
		r.Kind = RefEnv
	case "steps":
		r.Kind = RefStep
		r.Head, r.Rest, _ = strings.Cut(rest, ".")
	default:
		r.Kind = RefStep
		if rest == "" {
			r.Kind = RefBare
		}
	}
	return r
}

func (r Ref) StepID() (string, bool) {
	switch r.Kind {
	case RefStep:
		return r.Head, r.Head != ""
	case RefBare:
		return r.Head, true
	}
	return "", false
}

func (r Ref) ExportName() (string, bool) {
	switch r.Kind {
	case RefExports:
		name, _, _ := strings.Cut(r.Rest, ".")
		return name, name != ""
	case RefBare:
		return r.Head, true
	}
	return "", false
}

func (s *Scope) lookup(expr string) (any, error) {
	r := ParseRef(expr)
	expr = r.Expr
	if r.Err != nil {
		return nil, fmt.Errorf("unresolved reference ${%s}: %w", expr, r.Err)
	}
	switch r.Kind {
	case RefUUID:
		return newUUID(), nil
	case RefClock:
		switch r.Head {
		case "now":
			return s.clock().Add(r.Offset).Format(time.RFC3339), nil
		case "nowunix":
			return strconv.FormatInt(s.clock().Add(r.Offset).Unix(), 10), nil
		default:
			return strconv.FormatInt(s.midnight().Add(r.Offset).Unix(), 10), nil
		}
	case RefVars:
		return s.require(s.Vars, r.Rest, expr)
	case RefExports:
		return s.require(s.Exports, r.Rest, expr)
	case RefEnv:
		v, ok := s.env(r.Rest)
		if !ok {
			return nil, fmt.Errorf("unresolved reference ${%s}: env %s is not set", expr, r.Rest)
		}
		return v, nil
	case RefBare:
		if v, ok := s.Exports[r.Head]; ok {
			return v, nil
		}
		return s.lookupStep(expr, expr)
	default:
		path := r.Head
		if r.Rest != "" {
			path += "." + r.Rest
		}
		return s.lookupStep(path, expr)
	}
}

func (s *Scope) lookupStep(rest, expr string) (any, error) {
	id, tail, _ := strings.Cut(rest, ".")
	view, ok := s.Steps[id]
	if !ok {
		return nil, fmt.Errorf("unresolved reference ${%s}: no prior step %q", expr, id)
	}
	root := view.Response
	if section, sub, _ := strings.Cut(tail, "."); section == "request" || section == "response" {
		if section == "request" {
			root = view.Request
		}
		tail = sub
	}
	if tail == "" {
		return root, nil
	}
	lookup := Get
	if view.Synthetic {
		lookup = GetSynthetic
	}
	v, ok := lookup(root, tail)
	if !ok {
		return nil, fmt.Errorf("unresolved reference ${%s}: path %q missing in step %q", expr, tail, id)
	}
	return v, nil
}

func (s *Scope) require(src map[string]any, path, expr string) (any, error) {
	if path == "" {
		return src, nil
	}
	v, ok := Get(src, path)
	if !ok {
		return nil, fmt.Errorf("unresolved reference ${%s}", expr)
	}
	return v, nil
}

func (s *Scope) clock() time.Time {
	if s.pinned.IsZero() {
		if s.Now != nil {
			s.pinned = s.Now().UTC()
		} else {
			s.pinned = time.Now().UTC()
		}
	}
	return s.pinned
}

func (s *Scope) midnight() time.Time {
	t := s.clock()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func splitClockOffset(head string) (string, time.Duration, error) {
	cut := strings.LastIndexAny(head, "+-")
	if cut <= 0 {
		return head, 0, nil
	}
	base := head[:cut]
	switch base {
	case "now", "nowunix", "today":
	default:
		return head, 0, nil
	}
	seconds, err := strconv.ParseInt(head[cut+1:], 10, 64)
	if err != nil || seconds < 0 {
		return head, 0, fmt.Errorf("%s takes a whole number of seconds after %c, like ${%s%c86400}, not %q",
			base, head[cut], base, head[cut], head[cut+1:])
	}
	if head[cut] == '-' {
		seconds = -seconds
	}
	return base, time.Duration(seconds) * time.Second, nil
}

func (s *Scope) env(k string) (string, bool) {
	if s.Env != nil {
		return s.Env(k)
	}
	return os.LookupEnv(k)
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
