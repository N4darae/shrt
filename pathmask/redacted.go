package pathmask

import (
	"bytes"
	"encoding/json"
	"sort"
)

func RedactedPaths(raw json.RawMessage) []string {
	if len(raw) == 0 || !bytes.Contains(raw, []byte(MaskRedacted)) && bytes.IndexByte(raw, '\\') < 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	out := []string{}
	collectRedacted(v, "", &out)
	sort.Strings(out)
	return out
}

func collectRedacted(v any, path string, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			collectRedacted(item, Join(path, k), out)
		}
	case []any:
		for i, item := range t {
			collectRedacted(item, Join(path, IndexKey(i)), out)
		}
	case string:
		if t == MaskRedacted && path != "" {
			*out = append(*out, path)
		}
	}
}
