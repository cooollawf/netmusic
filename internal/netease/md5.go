package netease

import (
	"crypto/md5"
	"encoding/hex"
)

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
