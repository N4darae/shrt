package chain

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	Request    any
	Response   any
	Synthetic  bool
	NoResponse bool
}

func NewScope(vars map[string]any) *Scope {
	return &Scope{
		Vars:    maps.Collect(maps.All(vars)),
		Exports: map[string]any{},
		Steps:   map[string]*StepView{},
		Now:     time.Now,
		Env:     os.LookupEnv,
	}
}

func (s *Scope) Record(id string, request, response any) {
	s.Steps[id] = &StepView{Request: request, Response: response}
}

func (s *Scope) RecordRequest(id string, request any) {
	s.Steps[id] = &StepView{Request: request, NoResponse: true}
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
		switch val.(type) {
		case map[string]any, []any:
			return nil, fmt.Errorf("reference ${%s} is interpolated inside other text (%q) but holds a message, list or map, "+
				"which has no text form and would be sent as Go syntax; interpolate one scalar field of it instead",
				strings.TrimSpace(in[m[2]:m[3]]), in)
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
		return nil, &UnresolvedRefError{Expr: expr, Detail: fmt.Sprintf("no prior step %q", id)}
	}
	root := view.Response
	readsRequest := false
	if section, sub, _ := strings.Cut(tail, "."); section == "request" || section == "response" {
		readsRequest = section == "request"
		if readsRequest {
			root = view.Request
		}
		tail = sub
	}
	if view.NoResponse && !readsRequest {
		return nil, fmt.Errorf("unresolved reference ${%s}: step %q has no response to read — its call "+
			"was refused or never completed, so only its request (${steps.%s.request...}) resolves", expr, id, id)
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
		if view.Synthetic {
			return nil, fmt.Errorf("unresolved reference ${%s}: path %q missing in step %q", expr, tail, id)
		}
		return nil, &MissingPathError{Expr: expr, Step: id, Path: tail, Near: nearestPresent(root, tail)}
	}
	return v, nil
}

type MissingPathError struct {
	Expr string
	Step string
	Path string
	Near string
}

func (e *MissingPathError) Error() string {
	return withNote(fmt.Sprintf("unresolved reference ${%s}: step %q answered without %s", e.Expr, e.Step, e.Path), e.Near)
}

func nearestPresent(root any, path string) string {
	segs := SplitPath(path)
	for n := len(segs) - 1; n > 0; n-- {
		prefix := strings.Join(segs[:n], ".")
		v, ok := Get(root, prefix)
		if !ok {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		text := string(raw)
		if len(text) > 80 {
			text = text[:77] + "..."
		}
		return prefix + " is " + text
	}
	return ""
}

func (s *Scope) require(src map[string]any, path, expr string) (any, error) {
	if path == "" {
		return src, nil
	}
	v, ok := Get(src, path)
	if !ok {
		return nil, &UnresolvedRefError{Expr: expr}
	}
	return v, nil
}

type UnresolvedRefError struct {
	Expr   string
	Detail string
}

func (e *UnresolvedRefError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("unresolved reference ${%s}", e.Expr)
	}
	return fmt.Sprintf("unresolved reference ${%s}: %s", e.Expr, e.Detail)
}

func ExplainLaterRef(c *Chain, stepIndex int, err error) error {
	var u *UnresolvedRefError
	if c == nil || !errors.As(err, &u) {
		return err
	}
	r := ParseRef(u.Expr)
	idx := newRefIndex(c)
	here := stepIndex + 1
	name := r.Head
	if r.Kind == RefExports {
		name, _, _ = strings.Cut(r.Rest, ".")
	}
	why := ""
	switch r.Kind {
	case RefExports:
		if idx.exportedBy[name] > here {
			why = idx.laterExport(name)
		}
	case RefBare:
		if idx.exportedBy[name] > here {
			why = idx.laterExport(name)
		} else if idx.stepAt[name] > here {
			why = idx.laterStep(name)
		}
	case RefStep:
		if idx.stepAt[r.Head] > here {
			why = idx.laterStep(r.Head)
		}
	}
	if why == "" {
		return err
	}
	return fmt.Errorf("unresolved reference ${%s}: it %s", u.Expr, why)
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
