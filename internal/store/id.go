package store

import (
	"crypto/rand"
	"encoding/hex"
)

// newID 生成随机 16 字节十六进制 ID。
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
