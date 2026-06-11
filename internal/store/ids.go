package store

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID 生成 prefix_<16hex> 形式的实体 ID(sess_/turn_/msg_)。
func NewID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
