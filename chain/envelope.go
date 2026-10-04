package chain

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/namecase"
)

const (
	DefaultEnvelopeField = "error"
	DefaultEnvelopePath  = "error.code"
	DefaultEnvelopeOK    = "OK"
)

type conventions struct {
	field    string
	path     string
	ok       string
	itemPath string
	readOnly []string
	codes    []string
}

var (
	conventionsMu sync.RWMutex
	active        = conventions{
		field:    DefaultEnvelopeField,
		path:     DefaultEnvelopePath,
		ok:       DefaultEnvelopeOK,
		readOnly: DefaultReadOnlyPrefixes(),
		codes:    DefaultCodeFields(),
	}
)

func DefaultCodeFields() []string {
	return []string{"app_code", "reason", "error_code"}
}

func current[T any](get func(conventions) T) T {
	conventionsMu.RLock()
	defer conventionsMu.RUnlock()
	return get(active)
}

func CodeFields() []string {
	return current(func(c conventions) []string { return append([]string(nil), c.codes...) })
}

func EnvelopeLeaf() string {
	return current(func(c conventions) string { return c.path[strings.LastIndex(c.path, ".")+1:] })
}

func EnvelopeField() string { return current(func(c conventions) string { return c.field }) }

func EnvelopePath() string { return current(func(c conventions) string { return c.path }) }

func EnvelopeOK() string { return current(func(c conventions) string { return c.ok }) }

func ItemEnvelope() string { return current(func(c conventions) string { return c.itemPath }) }

func ReadOnlyPrefixes() []string {
	return current(func(c conventions) []string { return append([]string(nil), c.readOnly...) })
}

func SetItemEnvelope(path string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	active.itemPath = strings.TrimSpace(path)
}

func setReadOnlyPrefixesLocked(prefixes []string) {
	if len(prefixes) == 0 {
		active.readOnly = DefaultReadOnlyPrefixes()
		return
	}
	active.readOnly = append([]string(nil), prefixes...)
}

func setEnvelopeLocked(path, ok string) {
	path, ok = cmp.Or(strings.TrimSpace(path), DefaultEnvelopePath), cmp.Or(strings.TrimSpace(ok), DefaultEnvelopeOK)
	active.path, active.ok, active.field = path, ok, path
	if i := strings.Index(path, "."); i > 0 {
		active.field = path[:i]
	}
}

type ItemRefusal struct {
	Path string
	Code string
	Line string
}

func (r ItemRefusal) String() string { return r.Path + " = " + r.Code }

func splitItemEnvelope(path string) (listPath, field string, err error) {
	list, field, ok := strings.Cut(path, "[].")
	if !ok {
		return "", "", fmt.Errorf("conventions.item_envelope_path is %q, which is not of the form "+
			"<list>[].<path> — the per-item verdict is not being checked at all, and a batch rpc that "+
			"refuses every line still reports success", path)
	}
	return strings.TrimSuffix(list, "."), field, nil
}

func listDeclares(fields []*catalog.Field, listPath, field string) bool {
	list, found := catalog.FieldAt(fields, SplitPath(listPath))
	return found && list != nil && list.Repeated && (list.Truncated || catalog.HasPath(list.Fields, SplitPath(field)))
}

func ItemRefusals(response any) ([]ItemRefusal, error) {
	if ItemEnvelope() == "" {
		return nil, nil
	}
	listPath, field, err := splitItemEnvelope(ItemEnvelope())
	if err != nil {
		return nil, err
	}
	rows, found := Get(response, listPath)
	if !found {
		return nil, nil
	}
	items, ok := rows.([]any)
	if !ok {
		return nil, fmt.Errorf("conventions.item_envelope_path names %q, which this response carries but "+
			"is not a list — check the path against the descriptor", listPath)
	}
	out := []ItemRefusal{}
	accounted := 0
	explicitOK := false
	unset := []int{}
	empty := []int{}
	for i, item := range items {
		v, found := Get(item, field)
		if !found || v == nil {
			if found || declaresUnsetVerdict(item, field) {
				accounted++
			}
			unset = append(unset, i)
			continue
		}
		accounted++
		code := stringify(v)
		switch code {
		case EnvelopeOK():
			explicitOK = true
		case "":
			empty = append(empty, i)
		default:
			line := fmt.Sprintf("%s.%d", listPath, i)
			out = append(out, ItemRefusal{Path: line + "." + field, Code: code, Line: line})
		}
	}
	missing := []int{}
	if explicitOK || len(out) > 0 {
		missing = append(missing, empty...)
	}
	if explicitOK {
		missing = append(missing, unset...)
	}
	for _, i := range missing {
		line := fmt.Sprintf("%s.%d", listPath, i)
		out = append(out, ItemRefusal{Path: line + "." + field, Code: NoItemVerdict, Line: line})
	}
	sort.SliceStable(out, func(a, b int) bool { return itemIndex(out[a].Line) < itemIndex(out[b].Line) })
	if len(items) > 0 && accounted == 0 {
		return nil, fmt.Errorf("conventions.item_envelope_path expects each %s[] to carry %q, and none of "+
			"the %d item(s) declares it at all. The per-item verdict is NOT being checked: a batch refusing "+
			"every line would report success. Check the path against the descriptor",
			listPath, field, len(items))
	}
	return out, nil
}

const NoItemVerdict = "(no verdict)"

type MisspeltItemVerdict struct {
	Refusal ItemRefusal
	Key     string
	Want    string
}

func MisspeltItemVerdicts(sent any, unknown []string) []MisspeltItemVerdict {
	listPath, field, err := splitItemEnvelope(ItemEnvelope())
	if err != nil {
		return nil
	}
	fieldSegs := SplitPath(field)
	var out []MisspeltItemVerdict
	seen := map[string]bool{}
	for _, u := range unknown {
		rest, ok := strings.CutPrefix(u, listPath+"[].")
		if !ok {
			continue
		}
		segs := strings.Split(rest, ".")
		depth := len(segs) - 1
		if depth >= len(fieldSegs) || !namecase.Equal(segs[depth], fieldSegs[depth]) || segs[depth] == fieldSegs[depth] {
			continue
		}
		if strings.Join(segs[:depth], ".") != strings.Join(fieldSegs[:depth], ".") {
			continue
		}
		rows, _ := Get(sent, listPath)
		items, _ := rows.([]any)
		for i, item := range items {
			parent, _ := Get(item, strings.Join(fieldSegs[:depth], "."))
			obj, ok := parent.(map[string]any)
			if !ok {
				continue
			}
			if _, exact := obj[fieldSegs[depth]]; exact {
				continue
			}
			if _, variant := obj[segs[depth]]; !variant {
				continue
			}
			line := fmt.Sprintf("%s.%d", listPath, i)
			if seen[line] {
				continue
			}
			seen[line] = true
			out = append(out, MisspeltItemVerdict{
				Refusal: ItemRefusal{Path: line + "." + field, Code: NoItemVerdict, Line: line},
				Key:     rest,
				Want:    strings.Join(fieldSegs[:depth+1], "."),
			})
		}
	}
	return out
}

func itemIndex(line string) int {
	n, _ := strconv.Atoi(line[strings.LastIndex(line, ".")+1:])
	return n
}

func declaresUnsetVerdict(item any, field string) bool {
	segs := SplitPath(field)
	for i := 1; i <= len(segs); i++ {
		v, found := Get(item, strings.Join(segs[:i], "."))
		if !found {
			return false
		}
		if v == nil {
			return true
		}
	}
	return false
}

func ItemEnvelopeDeclared(fields []*catalog.Field) bool {
	listPath, field, err := splitItemEnvelope(ItemEnvelope())
	return err == nil && listDeclares(fields, listPath, field)
}

func ValidateItemEnvelope(cat *catalog.Catalog) error {
	return ValidateItemEnvelopeIn(cat, ItemEnvelope())
}

func ValidateItemEnvelopeIn(cat *catalog.Catalog, path string) error {
	if path == "" {
		return nil
	}
	listPath, field, err := splitItemEnvelope(path)
	if err != nil {
		return err
	}
	for _, m := range cat.Methods() {
		if listDeclares(catalog.DescribeMessage(m.Output()).Fields, listPath, field) {
			return nil
		}
	}
	return fmt.Errorf("conventions.item_envelope_path is %q, and no response message in the descriptor "+
		"declares that list with that field — nothing would ever be checked, so a batch rpc refusing "+
		"every line would report success. Fix the path against the descriptor, or remove the key if "+
		"this backend has no per-item verdict", path)
}

func ValidateEnvelopeIn(cat *catalog.Catalog, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || cat == nil || len(cat.Methods()) == 0 {
		return nil
	}
	segs := SplitPath(path)
	for _, m := range cat.Methods() {
		if catalog.HasResponsePath(catalog.DescribeMessage(m.Output()).Fields, segs) {
			return nil
		}
	}
	hint := ""
	if found := catalog.DetectEnvelope(cat); len(found) > 0 {
		hint = fmt.Sprintf(" The response messages carry %q; if that is the verdict, set envelope_path: %s.", found[0].Path, found[0].Path)
	}
	return fmt.Errorf("conventions.envelope_path is %q, and no response message in the descriptor declares "+
		"that field, so nothing was sent: every step asserting the envelope would compare against a path "+
		"that is never present, and an in-band refusal would pass unseen.%s Fix the path against the "+
		"descriptor ('shrt doctor' checks it)", path, hint)
}

func joinDataPath(path string) string {
	return strings.Join(slices.DeleteFunc(SplitPath(path), isDigits), ".")
}

func IsVerdictPath(path string) bool {
	if IsEnvelopePath(path) {
		return true
	}
	listPath, field, err := splitItemEnvelope(ItemEnvelope())
	if err != nil {
		return false
	}
	return joinDataPath(path) == joinDataPath(listPath+"."+field)
}

func isScalarValue(v any) bool {
	switch v.(type) {
	case nil, []any, map[string]any:
		return false
	}
	return true
}

func VacuousNotEqual(path string, want any) bool {
	return IsVerdictPath(path) && stringify(want) != EnvelopeOK()
}

func VacuousNotEqualResult(path string, want, got any) bool {
	return VacuousNotEqual(path, want) || isScalarValue(want) && !isScalarValue(got)
}

func (e Expectation) PinsValue() bool {
	if e.Equals != nil || e.Contains != "" || e.HasComparison() {
		return true
	}
	return e.NotEqual != nil && !VacuousNotEqual(e.Path, e.NotEqual)
}

func DeclaresVerdict(expect []Expectation, path string) bool {
	want := strings.Join(SplitPath(path), ".")
	return slices.ContainsFunc(expect, func(e Expectation) bool { return e.PinsValue() && strings.Join(SplitPath(e.Path), ".") == want })
}

func DeclaresRefusal(expect []Expectation, r ItemRefusal) bool {
	if DeclaresVerdict(expect, r.Path) {
		return true
	}
	line := r.Line
	if _, field, err := splitItemEnvelope(ItemEnvelope()); line == "" && err == nil && strings.HasSuffix(r.Path, "."+field) {
		line = strings.TrimSuffix(r.Path, "."+field)
	}
	line = strings.Join(SplitPath(line), ".")
	if line == "" {
		return false
	}
	return slices.ContainsFunc(expect, func(e Expectation) bool {
		segs := SplitPath(e.Path)
		return namesCode(e) && strings.HasPrefix(strings.Join(segs, "."), line+".") && isCodeField(segs)
	})
}

func UndeclaredRefusals(refusals []ItemRefusal, expect []Expectation) []ItemRefusal {
	out := []ItemRefusal{}
	for _, r := range refusals {
		if !DeclaresRefusal(expect, r) {
			out = append(out, r)
		}
	}
	return out
}

func SetEnvelope(path, ok string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setEnvelopeLocked(path, ok)
}

func IsEnvelopePath(path string) bool {
	return path == EnvelopeField() || strings.HasPrefix(path, EnvelopeField()+".")
}

func CoversVerdict(path string) bool {
	verdict := SplitPath(EnvelopePath())
	segs := SplitPath(path)
	return len(segs) > 0 && len(segs) <= len(verdict) && slices.EqualFunc(segs, verdict[:len(segs)], namecase.Equal)
}

func namesCode(e Expectation) bool {
	return e.Contains != "" || (e.Equals != nil && stringify(e.Equals) != "")
}

func isCodeField(segs []string) bool {
	return len(segs) > 0 && slices.Contains(CodeFields(), segs[len(segs)-1])
}

func PinsVerdictCode(e Expectation) bool {
	if !namesCode(e) {
		return false
	}
	segs := SplitPath(e.Path)
	verdict := SplitPath(EnvelopePath())
	return len(segs) > 0 && len(segs) >= len(verdict) && slices.EqualFunc(segs[:len(verdict)-1], verdict[:len(verdict)-1], namecase.Equal) && isCodeField(segs)
}

func IsVerdictItself(path string) bool {
	return CoversVerdict(path) && len(SplitPath(path)) == len(SplitPath(EnvelopePath()))
}

func IsPagingFieldName(name string) bool {
	page, token := false, false
	for _, w := range namecase.Words(name) {
		switch w {
		case "cursor", "pagination":
			return true
		case "page":
			page = true
		case "token":
			token = true
		}
	}
	return page && token
}

func IsMetadataField(name string) bool {
	return name == EnvelopeField() || IsPagingFieldName(name)
}

func ApplyConventions(readOnlyPrefixes []string, envelopePath, envelopeOK string) {
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	setReadOnlyPrefixesLocked(readOnlyPrefixes)
	setEnvelopeLocked(envelopePath, envelopeOK)
}

func ApplyCodeFields(names []string) {
	out := []string{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		out = DefaultCodeFields()
	}
	conventionsMu.Lock()
	defer conventionsMu.Unlock()
	active.codes = out
}
