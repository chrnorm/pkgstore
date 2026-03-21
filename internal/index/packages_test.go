package index

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndReadPackages(t *testing.T) {
	entries := []PackageEntry{
		{
			Package:      "granted",
			Version:      "0.39.0",
			Architecture: "amd64",
			Maintainer:   "Chris Norman <chris@granted.dev>",
			Priority:     "optional",
			Section:      "utils",
			Homepage:     "https://granted.dev",
			Description:  "Granted CLI",
			Filename:     "pool/main/granted_0.39.0_amd64.deb",
			Size:         12345,
			MD5sum:       "d41d8cd98f00b204e9800998ecf8427e",
			SHA1:         "da39a3ee5e6b4b0d3255bfef95601890afd80709",
			SHA256:       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
	}

	var buf bytes.Buffer
	err := WritePackages(&buf, entries)
	require.NoError(t, err)

	output := buf.String()

	// Verify key fields are present.
	assert.Contains(t, output, "Package: granted")
	assert.Contains(t, output, "Version: 0.39.0")
	assert.Contains(t, output, "Architecture: amd64")
	assert.Contains(t, output, "Filename: pool/main/granted_0.39.0_amd64.deb")
	assert.Contains(t, output, "Size: 12345")
	assert.Contains(t, output, "SHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	// Roundtrip: read it back.
	parsed, err := ReadPackages(strings.NewReader(output))
	require.NoError(t, err)
	require.Len(t, parsed, 1)

	assert.Equal(t, entries[0].Package, parsed[0].Package)
	assert.Equal(t, entries[0].Version, parsed[0].Version)
	assert.Equal(t, entries[0].Architecture, parsed[0].Architecture)
	assert.Equal(t, entries[0].Filename, parsed[0].Filename)
	assert.Equal(t, entries[0].Size, parsed[0].Size)
	assert.Equal(t, entries[0].SHA256, parsed[0].SHA256)
}

func TestWritePackages_MultipleEntries(t *testing.T) {
	entries := []PackageEntry{
		{Package: "beta", Version: "1.0.0", Architecture: "amd64", Filename: "pool/main/beta_1.0.0_amd64.deb", Size: 100},
		{Package: "alpha", Version: "2.0.0", Architecture: "amd64", Filename: "pool/main/alpha_2.0.0_amd64.deb", Size: 200},
	}

	var buf bytes.Buffer
	err := WritePackages(&buf, entries)
	require.NoError(t, err)

	output := buf.String()

	// Should be sorted: alpha before beta.
	alphaIdx := strings.Index(output, "Package: alpha")
	betaIdx := strings.Index(output, "Package: beta")
	assert.Less(t, alphaIdx, betaIdx)

	// Roundtrip.
	parsed, err := ReadPackages(strings.NewReader(output))
	require.NoError(t, err)
	require.Len(t, parsed, 2)
	assert.Equal(t, "alpha", parsed[0].Package)
	assert.Equal(t, "beta", parsed[1].Package)
}

func TestReadPackages_EmptyInput(t *testing.T) {
	parsed, err := ReadPackages(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, parsed)
}

func TestCompressPackages(t *testing.T) {
	data := []byte("Package: test\nVersion: 1.0\n")
	compressed, err := CompressPackages(data)
	require.NoError(t, err)
	assert.Greater(t, len(compressed), 0)

	// Verify it's valid gzip by checking magic bytes.
	assert.Equal(t, byte(0x1f), compressed[0])
	assert.Equal(t, byte(0x8b), compressed[1])
}
