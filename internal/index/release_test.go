package index

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRelease(t *testing.T) {
	content := []byte("Package: test\nVersion: 1.0\n")

	cfg := ReleaseConfig{
		Origin:        "Test",
		Label:         "Test",
		Suite:         "stable",
		Codename:      "stable",
		Architectures: []string{"amd64"},
		Components:    []string{"main"},
		Date:          time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
	}

	files := []IndexFileEntry{
		{RelativePath: "main/binary-amd64/Packages", Content: content},
	}

	var buf bytes.Buffer
	err := WriteRelease(&buf, cfg, files)
	require.NoError(t, err)

	output := buf.String()

	assert.Contains(t, output, "Origin: Test")
	assert.Contains(t, output, "Suite: stable")
	assert.Contains(t, output, "Acquire-By-Hash: yes")
	assert.Contains(t, output, "Architectures: amd64")
	assert.Contains(t, output, "Components: main")

	// Verify checksums match actual content.
	md5sum := md5.Sum(content)
	sha1sum := sha1.Sum(content)
	sha256sum := sha256.Sum256(content)

	assert.Contains(t, output, hex.EncodeToString(md5sum[:]))
	assert.Contains(t, output, hex.EncodeToString(sha1sum[:]))
	assert.Contains(t, output, hex.EncodeToString(sha256sum[:]))

	// Verify file size is correct.
	assert.Contains(t, output, fmt.Sprintf("%16d main/binary-amd64/Packages", len(content)))
}

func TestWriteRelease_MultipleFiles(t *testing.T) {
	packages := []byte("Package: test\n")
	packagesGz := []byte{0x1f, 0x8b, 0x08} // fake gzip

	cfg := ReleaseConfig{
		Suite:         "stable",
		Architectures: []string{"amd64"},
		Components:    []string{"main"},
		Date:          time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC),
	}

	files := []IndexFileEntry{
		{RelativePath: "main/binary-amd64/Packages", Content: packages},
		{RelativePath: "main/binary-amd64/Packages.gz", Content: packagesGz},
	}

	var buf bytes.Buffer
	err := WriteRelease(&buf, cfg, files)
	require.NoError(t, err)

	output := buf.String()

	// Both files should appear in each checksum section.
	assert.Equal(t, 3, strings.Count(output, "main/binary-amd64/Packages.gz"))  // MD5, SHA1, SHA256
	assert.Equal(t, 6, strings.Count(output, "main/binary-amd64/Packages"))     // 3 for Packages + 3 for Packages.gz (substring)
}

func TestByHashPath(t *testing.T) {
	content := []byte("hello world")
	h := sha256.Sum256(content)
	expected := "main/binary-amd64/by-hash/SHA256/" + hex.EncodeToString(h[:])

	result := ByHashPath("main/binary-amd64", content)
	assert.Equal(t, expected, result)
}
