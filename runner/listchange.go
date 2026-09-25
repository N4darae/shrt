package runner

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/N4darae/shrt/chain"
)

type listPosition struct {
	list  string
	index int
	rest  string
	items []any
}

func listPositionOf(root any, path string) (listPosition, bool) {
	segs := chain.SplitPath(path)
	for k := len(segs) - 1; k >= 1; k-- {
		i, err := strconv.Atoi(segs[k])
		if err != nil || i < 0 {
			continue
		}
		prefix := strings.Join(segs[:k], ".")
		v, ok := chain.Get(root, prefix)
		items, isList := v.([]any)
		if !ok || !isList {
			continue
		}
		return listPosition{list: prefix, index: i, rest: strings.Join(segs[k+1:], "."), items: items}, true
	}
	return listPosition{}, false
}

func identityKey(path string) bool {
	segs := chain.SplitPath(path)
	if len(segs) == 0 {
		return false
	}
	key := segs[len(segs)-1]
	lower := strings.ToLower(key)
	return lower == "id" || strings.HasSuffix(lower, "_id") || strings.HasPrefix(lower, "id_") ||
		(strings.HasSuffix(key, "Id") && len(key) > 2)
}

func listChangeKinds(sr *StepRecord) map[int]string {
	out := map[int]string{}
	var root any
	if len(sr.Response) == 0 || json.Unmarshal(sr.Response, &root) != nil {
		return out
	}
	itemKind := map[string]string{}
	for i, ex := range sr.Expect {
		if ex.Passed || ex.Rule != "equals" || !identityKey(ex.Path) {
			continue
		}
		pos, ok := listPositionOf(root, ex.Path)
		if !ok || pos.rest == "" {
			continue
		}
		kind := fmt.Sprintf("item missing: no item of %s has %s=%s", pos.list, pos.rest, gotText(ex.Want))
		for j, item := range pos.items {
			if v, ok := chain.Get(item, pos.rest); ok && j != pos.index && gotText(v) == gotText(ex.Want) {
				kind = fmt.Sprintf("reordered: %s is at %s.%d", gotText(ex.Want), pos.list, j)
				break
			}
		}
		out[i] = kind
		itemKind[pos.list+"."+strconv.Itoa(pos.index)] = pos.list + "." + strconv.Itoa(pos.index) + "." + pos.rest
	}
	for i, ex := range sr.Expect {
		if ex.Passed || out[i] != "" {
			continue
		}
		pos, ok := listPositionOf(root, ex.Path)
		if !ok {
			continue
		}
		at := pos.list + "." + strconv.Itoa(pos.index)
		switch {
		case pos.rest == "" && ex.Rule == "exists" && gotText(ex.Want) == "false":
			out[i] = fmt.Sprintf("item added: %s holds %d item(s)", pos.list, len(pos.items))
		case pos.rest == "" && ex.Rule == "exists":
			out[i] = fmt.Sprintf("item missing: %s holds %d item(s)", pos.list, len(pos.items))
		case itemKind[at] != "":
			out[i] = "another item at " + at + ", see " + itemKind[at]
		case ex.Rule == "equals":
			out[i] = "value changed: the item at " + at + " holds another " + pos.rest
		}
	}
	return out
}

var listChangeWords = []string{"reordered", "item missing", "item added", "another item", "value changed"}

func ListChangeKind(item string) string {
	for _, word := range listChangeWords {
		if strings.Contains(item, " ("+word) {
			if word == "another item" {
				return ""
			}
			return word
		}
	}
	return ""
}
