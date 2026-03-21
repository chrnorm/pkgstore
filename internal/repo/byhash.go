package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ByHashPath returns the by-hash path for the given content within a directory.
// dir is relative to dists/{suite}/, e.g. "main/binary-amd64".
func ByHashPath(dir string, content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%s/by-hash/SHA256/%s", dir, hex.EncodeToString(h[:]))
}
