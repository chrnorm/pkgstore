package deb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/blakesmith/ar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDeb(t *testing.T, control string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.deb")

	// Build control.tar.gz in memory.
	var controlTarGz bytes.Buffer
	gz := gzip.NewWriter(&controlTarGz)
	tw := tar.NewWriter(gz)

	controlBytes := []byte(control)
	err := tw.WriteHeader(&tar.Header{
		Name: "./control",
		Size: int64(len(controlBytes)),
		Mode: 0o644,
	})
	require.NoError(t, err)
	_, err = tw.Write(controlBytes)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	// Build the ar archive (.deb).
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	w := ar.NewWriter(f)
	require.NoError(t, w.WriteGlobalHeader())

	// debian-binary member.
	debBinary := []byte("2.0\n")
	err = w.WriteHeader(&ar.Header{
		Name: "debian-binary",
		Size: int64(len(debBinary)),
		Mode: 0o100644,
	})
	require.NoError(t, err)
	_, err = w.Write(debBinary)
	require.NoError(t, err)

	// control.tar.gz member.
	ctrlData := controlTarGz.Bytes()
	err = w.WriteHeader(&ar.Header{
		Name: "control.tar.gz",
		Size: int64(len(ctrlData)),
		Mode: 0o100644,
	})
	require.NoError(t, err)
	_, err = w.Write(ctrlData)
	require.NoError(t, err)

	return path
}

func TestReadDeb(t *testing.T) {
	control := `Package: granted
Version: 0.39.0
Architecture: amd64
Maintainer: Chris Norman <chris@granted.dev>
Installed-Size: 12345
Depends: libc6
Priority: optional
Section: utils
Homepage: https://granted.dev
Description: Granted CLI
 A tool for managing AWS credentials.
`

	path := createTestDeb(t, control)

	meta, fi, err := ReadDeb(path)
	require.NoError(t, err)

	assert.Equal(t, "granted", meta.Package)
	assert.Equal(t, "0.39.0", meta.Version)
	assert.Equal(t, "amd64", meta.Architecture)
	assert.Equal(t, "Chris Norman <chris@granted.dev>", meta.Maintainer)
	assert.Equal(t, "12345", meta.InstalledSize)
	assert.Equal(t, "libc6", meta.Depends)
	assert.Equal(t, "optional", meta.Priority)
	assert.Equal(t, "utils", meta.Section)
	assert.Equal(t, "https://granted.dev", meta.Homepage)
	assert.Equal(t, "Granted CLI\n A tool for managing AWS credentials.", meta.Description)

	// File info should be populated.
	assert.Greater(t, fi.Size, int64(0))
	assert.NotEmpty(t, fi.MD5)
	assert.NotEmpty(t, fi.SHA1)
	assert.NotEmpty(t, fi.SHA256)
	assert.Len(t, fi.SHA256, 64) // hex-encoded SHA256
}

func TestReadDeb_i386Architecture(t *testing.T) {
	// NFPM generates debs with Architecture: i386 for 32-bit builds.
	// Verify we read the architecture from the control file correctly.
	control := `Package: granted
Version: 0.39.0
Architecture: i386
Maintainer: Test <test@test.com>
Description: Test
`
	path := createTestDeb(t, control)
	meta, _, err := ReadDeb(path)
	require.NoError(t, err)
	assert.Equal(t, "i386", meta.Architecture)
}

func TestReadDeb_MissingPackage(t *testing.T) {
	control := `Version: 1.0.0
Architecture: amd64
`
	path := createTestDeb(t, control)
	_, _, err := ReadDeb(path)
	assert.ErrorContains(t, err, "missing Package field")
}

func TestReadDeb_MissingVersion(t *testing.T) {
	control := `Package: test
Architecture: amd64
`
	path := createTestDeb(t, control)
	_, _, err := ReadDeb(path)
	assert.ErrorContains(t, err, "missing Version field")
}
