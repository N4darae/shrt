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
	wanted := map[string]int{}
	for _, ex := range sr.Expect {
		if pos, ok := listPositionOf(root, ex.Path); ok && ex.Rule == "equals" && identityKey(ex.Path) && pos.rest != "" {
			wanted[pos.list+"\x00"+pos.rest+"\x00"+gotText(ex.Want)]++
		}
	}
	for i, ex := range sr.Expect {
		if ex.Passed || ex.Rule != "equals" || !identityKey(ex.Path) {
			continue
		}
		pos, ok := listPositionOf(root, ex.Path)
		if !ok || pos.rest == "" {
			continue
		}
		kind := fmt.Sprintf("item missing: no item of %s has %s=%s", pos.list, pos.rest, gotText(ex.Want))
		if n := wanted[pos.list+"\x00"+pos.rest+"\x00"+gotText(ex.Want)]; n > 1 {
			have := 0
			for _, item := range pos.items {
				if v, ok := chain.Get(item, pos.rest); ok && gotText(v) == gotText(ex.Want) {
					have++
				}
			}
			if have >= n {
				continue
			}
			kind = fmt.Sprintf("item missing: %s holds %d of the %d item(s) with %s=%s", pos.list, have, n, pos.rest, gotText(ex.Want))
			pos.items = nil
		}
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
		case ex.Rule == "equals" && sentReordered(sr.Request, pos):
			out[i] = "reordered: " + pos.list + " holds the items sent, in another order"
		case ex.Rule == "equals":
			out[i] = "value changed: the item at " + at + " holds another " + pos.rest
		}
	}
	return out
}

func sentReordered(request json.RawMessage, pos listPosition) bool {
	var req any
	if len(pos.items) < 2 || json.Unmarshal(request, &req) != nil {
		return false
	}
	segs := chain.SplitPath(pos.list)
	sent, _ := findList(req, segs[len(segs)-1]).([]any)
	if len(sent) != len(pos.items) {
		return false
	}
	count, same := map[string]int{}, true
	for j, item := range pos.items {
		want, ok := sent[j].(map[string]any)
		if !ok || len(want) == 0 {
			return false
		}
		got := map[string]any{}
		for k := range want {
			got[k], _ = chain.Get(item, k)
		}
		a, b := fmt.Sprint(want), fmt.Sprint(got)
		count[a]++
		count[b]--
		same = same && a == b
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return !same
}

func findList(v any, key string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if l, ok := m[key].([]any); ok {
		return l
	}
	for _, child := range m {
		if l := findList(child, key); l != nil {
			return l
		}
	}
	return nil
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

func ReorderedPaths(sr *StepRecord) map[string]bool {
	out := map[string]bool{}
	kinds := listChangeKinds(sr)
	var root any
	_ = json.Unmarshal(sr.Response, &root)
	changedSet := map[string]bool{}
	for i, k := range kinds {
		if pos, ok := listPositionOf(root, sr.Expect[i].Path); ok && !strings.HasPrefix(k, "reordered") && !strings.HasPrefix(k, "another item at ") {
			changedSet[pos.list] = true
		}
	}
	setChanged := func(list string) bool {
		for l := range changedSet {
			if list == l || strings.HasPrefix(list, l+".") {
				return true
			}
		}
		return false
	}
	for i, k := range kinds {
		if pos, _ := listPositionOf(root, sr.Expect[i].Path); strings.HasPrefix(k, "reordered") && !setChanged(pos.list) {
			out[sr.Expect[i].Path] = true
		}
	}
	for i, k := range kinds {
		if _, see, ok := strings.Cut(k, ", see "); ok && strings.HasPrefix(k, "another item at ") && out[see] {
			out[sr.Expect[i].Path] = true
		}
	}
	return out
}
