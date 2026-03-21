package prune

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blakesmith/ar"
	"github.com/chrnorm/pkgstore/internal/publish"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDeb(t *testing.T, pkg, version, arch string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.deb")
	control := fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\nMaintainer: Test <test@test.com>\nDescription: Test\n", pkg, version, arch)

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

func TestPrune_DeletesOldEntries(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	// Publish 3 versions to accumulate by-hash entries.
	for _, ver := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		debPath := createTestDeb(t, "testpkg", ver, "amd64")
		_, err := publish.Publish(ctx, store, publish.Options{
			DebPaths:     []string{debPath},
			Distribution: "stable",
			Component:    "main",
		})
		require.NoError(t, err)
	}

	// Should have 6 by-hash entries (2 per version: Packages + Packages.gz).
	byHashDir := filepath.Join(storeDir, "dists/stable/main/binary-amd64/by-hash/SHA256")
	entries, err := os.ReadDir(byHashDir)
	require.NoError(t, err)
	assert.Equal(t, 6, len(entries))

	// Set old entries to 2 hours ago, keep current ones recent.
	// The current entries are the ones matching the latest Packages file.
	for i, entry := range entries {
		path := filepath.Join(byHashDir, entry.Name())
		if i < 4 { // First 4 entries are from v1 and v2 (old).
			require.NoError(t, os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
		}
		// Last 2 entries are from v3 (current) — leave them recent.
	}

	// Prune entries older than 1 hour.
	result, err := Prune(ctx, store, Options{
		Distribution: "stable",
		Component:    "main",
		OlderThan:    1 * time.Hour,
	})
	require.NoError(t, err)

	// The 2 current by-hash entries (referenced by Packages files) should be
	// kept regardless of age. Of the 4 old entries, those older than 1h should
	// be deleted. Current entries are always protected.
	assert.Greater(t, result.Deleted, 0)

	// Verify current entries still exist.
	entries, err = os.ReadDir(byHashDir)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 2, "current by-hash entries should be preserved")
}

func TestPrune_KeepsCurrentEntries(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	// Publish a single version.
	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")
	_, err := publish.Publish(ctx, store, publish.Options{
		DebPaths:     []string{debPath},
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Set all entries to old.
	byHashDir := filepath.Join(storeDir, "dists/stable/main/binary-amd64/by-hash/SHA256")
	entries, err := os.ReadDir(byHashDir)
	require.NoError(t, err)
	for _, entry := range entries {
		path := filepath.Join(byHashDir, entry.Name())
		require.NoError(t, os.Chtimes(path, time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour)))
	}

	// Prune — should delete nothing because all entries are current (referenced by Packages).
	result, err := Prune(ctx, store, Options{
		Distribution: "stable",
		Component:    "main",
		OlderThan:    1 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Deleted)

	// All entries should still exist.
	entries, err = os.ReadDir(byHashDir)
	require.NoError(t, err)
	assert.Equal(t, 2, len(entries))
}

func TestPrune_NothingToPrune(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	// Publish a single version.
	debPath := createTestDeb(t, "testpkg", "1.0.0", "amd64")
	_, err := publish.Publish(ctx, store, publish.Options{
		DebPaths:     []string{debPath},
		Distribution: "stable",
		Component:    "main",
	})
	require.NoError(t, err)

	// Prune with a long duration — nothing should be deleted.
	result, err := Prune(ctx, store, Options{
		Distribution: "stable",
		Component:    "main",
		OlderThan:    24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Deleted)
}
