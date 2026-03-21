package publish

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blakesmith/ar"
	"github.com/chrnorm/pkgstore/internal/gpg"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDeb(t *testing.T, pkg, version, arch string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.deb")

	control := "Package: " + pkg + "\nVersion: " + version + "\nArchitecture: " + arch + "\nMaintainer: Test <test@test.com>\nDescription: Test package\n"

	var controlTarGz bytes.Buffer
	gz := gzip.NewWriter(&controlTarGz)
	tw := tar.NewWriter(gz)
	controlBytes := []byte(control)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Size: int64(len(controlBytes)), Mode: 0o644}))
	_, err := tw.Write(controlBytes)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	w := ar.NewWriter(f)
	require.NoError(t, w.WriteGlobalHeader())

	debBinary := []byte("2.0\n")
	require.NoError(t, w.WriteHeader(&ar.Header{Name: "debian-binary", Size: int64(len(debBinary)), Mode: 0o100644}))
	_, err = w.Write(debBinary)
	require.NoError(t, err)

	ctrlData := controlTarGz.Bytes()
	require.NoError(t, w.WriteHeader(&ar.Header{Name: "control.tar.gz", Size: int64(len(ctrlData)), Mode: 0o100644}))
	_, err = w.Write(ctrlData)
	require.NoError(t, err)

	return path
}

func TestPublish_Basic(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")

	result, err := Publish(ctx, store, Options{
		DebPath:      debPath,
		Distribution: "stable",
		Component:    "main",
		Origin:       "Test",
		Label:        "Test",
	})
	require.NoError(t, err)

	assert.Equal(t, "testpkg", result.Package)
	assert.Equal(t, "1.0.0", result.Version)
	assert.Equal(t, "amd64", result.Architecture)
	assert.Equal(t, "pool/main/testpkg_1.0.0_amd64.deb", result.PoolPath)

	// Verify files exist in storage.
	assertFileExists(t, storeDir, "pool/main/testpkg_1.0.0_amd64.deb")
	assertFileExists(t, storeDir, "dists/stable/main/binary-amd64/Packages")
	assertFileExists(t, storeDir, "dists/stable/main/binary-amd64/Packages.gz")
	assertFileExists(t, storeDir, "dists/stable/Release")

	// Verify Packages content.
	packagesContent := readFile(t, storeDir, "dists/stable/main/binary-amd64/Packages")
	assert.Contains(t, packagesContent, "Package: testpkg")
	assert.Contains(t, packagesContent, "Version: 1.0.0")
	assert.Contains(t, packagesContent, "Filename: pool/main/testpkg_1.0.0_amd64.deb")

	// Verify Release content.
	releaseContent := readFile(t, storeDir, "dists/stable/Release")
	assert.Contains(t, releaseContent, "Origin: Test")
	assert.Contains(t, releaseContent, "Acquire-By-Hash: yes")
	assert.Contains(t, releaseContent, "main/binary-amd64/Packages")

	// Verify by-hash files exist.
	byHashDir := filepath.Join(storeDir, "dists/stable/main/binary-amd64/by-hash/SHA256")
	entries, err := os.ReadDir(byHashDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2) // One for Packages, one for Packages.gz

	// Verify by-hash filename matches SHA256 of content.
	packagesBytes, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/main/binary-amd64/Packages"))
	require.NoError(t, err)
	h := sha256.Sum256(packagesBytes)
	expectedHash := hex.EncodeToString(h[:])
	assertFileExists(t, storeDir, "dists/stable/main/binary-amd64/by-hash/SHA256/"+expectedHash)
}

func TestPublish_WithGPG(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	privKey, err := gpg.GenerateTestKey()
	require.NoError(t, err)

	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")

	_, err = Publish(ctx, store, Options{
		DebPath:       debPath,
		Distribution:  "stable",
		Component:     "main",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	// Verify signed files exist.
	assertFileExists(t, storeDir, "dists/stable/Release.gpg")
	assertFileExists(t, storeDir, "dists/stable/InRelease")

	releaseGPG := readFile(t, storeDir, "dists/stable/Release.gpg")
	assert.Contains(t, releaseGPG, "BEGIN PGP SIGNATURE")

	inRelease := readFile(t, storeDir, "dists/stable/InRelease")
	assert.Contains(t, inRelease, "BEGIN PGP SIGNED MESSAGE")

	// Verify signatures are valid.
	pubKey, err := gpg.ReadPublicKeyFromPrivate(privKey)
	require.NoError(t, err)

	releaseContent, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/Release"))
	require.NoError(t, err)
	releaseGPGBytes, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/Release.gpg"))
	require.NoError(t, err)

	err = gpg.VerifyDetachedSignature(pubKey, releaseContent, releaseGPGBytes)
	assert.NoError(t, err)

	inReleaseBytes, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/InRelease"))
	require.NoError(t, err)
	_, err = gpg.VerifyClearSignature(pubKey, inReleaseBytes)
	assert.NoError(t, err)
}

func TestPublish_UpdateExisting(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	// Publish v1.
	debV1 := createTestDeb(t, "testpkg", "1.0.0", "amd64")
	_, err := Publish(ctx, store, Options{
		DebPath:      debV1,
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Publish v2.
	debV2 := createTestDeb(t, "testpkg", "2.0.0", "amd64")
	_, err = Publish(ctx, store, Options{
		DebPath:      debV2,
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Verify both versions in Packages.
	packagesContent := readFile(t, storeDir, "dists/stable/main/binary-amd64/Packages")
	assert.Contains(t, packagesContent, "Version: 1.0.0")
	assert.Contains(t, packagesContent, "Version: 2.0.0")

	// Verify both .deb files exist.
	assertFileExists(t, storeDir, "pool/main/testpkg_1.0.0_amd64.deb")
	assertFileExists(t, storeDir, "pool/main/testpkg_2.0.0_amd64.deb")

	// Verify by-hash directory has entries for both the old and new Packages content.
	byHashDir := filepath.Join(storeDir, "dists/stable/main/binary-amd64/by-hash/SHA256")
	entries, err := os.ReadDir(byHashDir)
	require.NoError(t, err)
	// After v1: 2 hashes (Packages + Packages.gz)
	// After v2: 4 hashes (old + new for each)
	assert.Len(t, entries, 4)
}

func TestPublish_MultipleArchitectures(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	// Publish amd64.
	debAmd64 := createTestDeb(t, "testpkg", "1.0.0", "amd64")
	_, err := Publish(ctx, store, Options{
		DebPath:      debAmd64,
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Publish arm64.
	debArm64 := createTestDeb(t, "testpkg", "1.0.0", "arm64")
	_, err = Publish(ctx, store, Options{
		DebPath:      debArm64,
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Verify both architectures have Packages files.
	assertFileExists(t, storeDir, "dists/stable/main/binary-amd64/Packages")
	assertFileExists(t, storeDir, "dists/stable/main/binary-arm64/Packages")

	// Verify Release contains both architectures.
	releaseContent := readFile(t, storeDir, "dists/stable/Release")
	assert.Contains(t, releaseContent, "main/binary-amd64/Packages")
	assert.Contains(t, releaseContent, "main/binary-arm64/Packages")
}

func TestPublish_ReleaseChecksums(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")
	_, err := Publish(ctx, store, Options{
		DebPath:      debPath,
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Read the actual Packages file and compute its SHA256.
	packagesBytes, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/main/binary-amd64/Packages"))
	require.NoError(t, err)
	h := sha256.Sum256(packagesBytes)
	expectedHash := hex.EncodeToString(h[:])

	// Verify the Release file contains this hash.
	releaseContent := readFile(t, storeDir, "dists/stable/Release")
	assert.Contains(t, releaseContent, expectedHash)
}

func TestPublish_IdempotentRepublish(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")

	// Publish twice.
	_, err := Publish(ctx, store, Options{DebPath: debPath, Distribution: "stable", Component: "main"})
	require.NoError(t, err)
	_, err = Publish(ctx, store, Options{DebPath: debPath, Distribution: "stable", Component: "main"})
	require.NoError(t, err)

	// Should still have only one entry.
	packagesContent := readFile(t, storeDir, "dists/stable/main/binary-amd64/Packages")
	assert.Equal(t, 1, strings.Count(packagesContent, "Package: testpkg"))
}

func assertFileExists(t *testing.T, root, relPath string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, relPath))
	assert.NoError(t, err, "expected file to exist: %s", relPath)
}

func readFile(t *testing.T, root, relPath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, relPath))
	require.NoError(t, err)
	return string(content)
}
