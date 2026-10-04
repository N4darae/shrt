package runner

import (
	"crypto/rand"
	"encoding/hex"
)

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
