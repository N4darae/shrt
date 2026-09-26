package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (c *Chain) Digest() string {
	if c == nil {
		return ""
	}
	copied := *c
	copied.Name, copied.SourcePath = "", ""
	if copied.APIVersion == "" {
		copied.APIVersion = APIVersion
	}
	raw, err := json.Marshal(&copied)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}
