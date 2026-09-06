package runner

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func randSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("000000")
	}
	return hex.EncodeToString(b)
}
