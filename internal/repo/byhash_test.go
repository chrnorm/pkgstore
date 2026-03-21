package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestByHashPath(t *testing.T) {
	content := []byte("Package: test\nVersion: 1.0\n")
	h := sha256.Sum256(content)

	result := ByHashPath("main/binary-amd64", content)
	expected := "main/binary-amd64/by-hash/SHA256/" + hex.EncodeToString(h[:])
	assert.Equal(t, expected, result)
}

func TestByHashPath_DifferentContent(t *testing.T) {
	a := ByHashPath("main/binary-amd64", []byte("content a"))
	b := ByHashPath("main/binary-amd64", []byte("content b"))
	assert.NotEqual(t, a, b, "different content should produce different by-hash paths")
}
