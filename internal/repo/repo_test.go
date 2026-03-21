package repo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chrnorm/pkgstore/internal/deb"
	"github.com/chrnorm/pkgstore/internal/index"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepository(t *testing.T) {
	r := New("stable", "main")
	assert.Equal(t, "stable", r.Distribution)
	assert.Equal(t, "main", r.Component)
	assert.Empty(t, r.Architectures())
}

func TestAddPackage(t *testing.T) {
	r := New("stable", "main")

	meta := &deb.PackageMetadata{
		Package:      "granted",
		Version:      "0.39.0",
		Architecture: "amd64",
		Maintainer:   "Chris Norman <chris@granted.dev>",
	}
	fi := &deb.FileInfo{
		Size:   12345,
		MD5:    "abc123",
		SHA1:   "def456",
		SHA256: "789xyz",
	}

	r.AddPackage(meta, fi, "pool/main/granted_0.39.0_amd64.deb")

	archs := r.Architectures()
	require.Len(t, archs, 1)
	assert.Equal(t, "amd64", archs[0])
}

func TestAddPackage_Idempotent(t *testing.T) {
	r := New("stable", "main")

	meta := &deb.PackageMetadata{
		Package:      "granted",
		Version:      "0.39.0",
		Architecture: "amd64",
	}
	fi1 := &deb.FileInfo{Size: 100, MD5: "a", SHA1: "b", SHA256: "c"}
	fi2 := &deb.FileInfo{Size: 200, MD5: "d", SHA1: "e", SHA256: "f"}

	r.AddPackage(meta, fi1, "pool/main/granted_0.39.0_amd64.deb")
	r.AddPackage(meta, fi2, "pool/main/granted_0.39.0_amd64.deb")

	// Should have only one entry, with the second file info.
	files, entries, err := r.BuildIndexFiles()
	require.NoError(t, err)

	// Should have Packages, Packages.gz, and two by-hash entries for amd64.
	assert.Len(t, files, 4)

	// The Packages file should contain only one entry.
	packagesKey := "dists/stable/main/binary-amd64/Packages"
	packagesContent := files[packagesKey]
	parsed, err := index.ReadPackages(bytes.NewReader(packagesContent))
	require.NoError(t, err)
	require.Len(t, parsed, 1)
	assert.Equal(t, int64(200), parsed[0].Size)

	// Release entries should have 2 entries (Packages + Packages.gz).
	assert.Len(t, entries, 2)
}

func TestAddPackage_MultipleArchitectures(t *testing.T) {
	r := New("stable", "main")

	metaAmd64 := &deb.PackageMetadata{Package: "granted", Version: "0.39.0", Architecture: "amd64"}
	metaArm64 := &deb.PackageMetadata{Package: "granted", Version: "0.39.0", Architecture: "arm64"}
	fi := &deb.FileInfo{Size: 100, MD5: "a", SHA1: "b", SHA256: "c"}

	r.AddPackage(metaAmd64, fi, "pool/main/granted_0.39.0_amd64.deb")
	r.AddPackage(metaArm64, fi, "pool/main/granted_0.39.0_arm64.deb")

	archs := r.Architectures()
	assert.Len(t, archs, 2)

	files, entries, err := r.BuildIndexFiles()
	require.NoError(t, err)

	// 4 files per arch (Packages, Packages.gz, 2 by-hash) = 8 total.
	assert.Len(t, files, 8)
	// 2 release entries per arch = 4 total.
	assert.Len(t, entries, 4)
}

func TestLoadArch_NotFound(t *testing.T) {
	store := &storage.FS{Root: t.TempDir()}
	r := New("stable", "main")

	err := r.LoadArch(context.Background(), store, "amd64")
	require.NoError(t, err)
	// Should be empty, not an error.
	assert.Empty(t, r.Architectures())
}

func TestLoadArch_ExistingPackages(t *testing.T) {
	dir := t.TempDir()
	store := &storage.FS{Root: dir}
	ctx := context.Background()

	// Write a Packages file to storage.
	packagesContent := "Package: granted\nVersion: 0.38.0\nArchitecture: amd64\nFilename: pool/main/granted_0.38.0_amd64.deb\nSize: 100\nSHA256: abc\n"
	err := store.Put(ctx, "dists/stable/main/binary-amd64/Packages", strings.NewReader(packagesContent), "")
	require.NoError(t, err)

	r := New("stable", "main")
	err = r.LoadArch(ctx, store, "amd64")
	require.NoError(t, err)

	// Now add a new version.
	meta := &deb.PackageMetadata{Package: "granted", Version: "0.39.0", Architecture: "amd64"}
	fi := &deb.FileInfo{Size: 200, MD5: "d", SHA1: "e", SHA256: "f"}
	r.AddPackage(meta, fi, "pool/main/granted_0.39.0_amd64.deb")

	files, _, err := r.BuildIndexFiles()
	require.NoError(t, err)

	// The Packages file should contain both versions.
	packagesKey := "dists/stable/main/binary-amd64/Packages"
	parsed, err := index.ReadPackages(bytes.NewReader(files[packagesKey]))
	require.NoError(t, err)
	require.Len(t, parsed, 2)
}

func TestBuildRelease(t *testing.T) {
	r := New("stable", "main")
	r.Origin = "Test"
	r.Label = "Test"

	meta := &deb.PackageMetadata{Package: "granted", Version: "0.39.0", Architecture: "amd64"}
	fi := &deb.FileInfo{Size: 100, MD5: "a", SHA1: "b", SHA256: "c"}
	r.AddPackage(meta, fi, "pool/main/granted_0.39.0_amd64.deb")

	_, entries, err := r.BuildIndexFiles()
	require.NoError(t, err)

	releaseContent, err := r.BuildRelease(entries, nil)
	require.NoError(t, err)

	release := string(releaseContent)
	assert.Contains(t, release, "Origin: Test")
	assert.Contains(t, release, "Suite: stable")
	assert.Contains(t, release, "Acquire-By-Hash: yes")
	assert.Contains(t, release, "Architectures: amd64")
	assert.Contains(t, release, "Components: main")
	assert.Contains(t, release, "SHA256:")
	assert.Contains(t, release, "main/binary-amd64/Packages")
}
