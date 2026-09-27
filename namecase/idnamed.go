package namecase

import "strings"

func IDNamed(key string) bool {
	lower := strings.ToLower(key)
	switch {
	case lower == "id", lower == "ids", lower == "idempotency_key",
		strings.HasSuffix(lower, "_id"), strings.HasSuffix(lower, "_ids"), strings.HasPrefix(lower, "id_"),
		camelTail(key, "Id"), camelTail(key, "Ids"), len(key) > 2 && key[:2] == "id" && key[2] >= 'A' && key[2] <= 'Z':
		return true
	}
	return false
}

func camelTail(key, suffix string) bool {
	if len(key) <= len(suffix) || !strings.HasSuffix(key, suffix) {
		return false
	}
	prev := key[len(key)-len(suffix)-1]
	return prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
}
