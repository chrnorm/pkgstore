package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blakesmith/ar"
	"github.com/chrnorm/pkgstore/internal/gpg"
	"github.com/chrnorm/pkgstore/internal/publish"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStaleCDN simulates the exact bug that broke the old Common Fate APT repo:
//
// 1. Publish v1 of a package → capture Packages.gz content
// 2. Publish v2 of a package → new Release points to new Packages.gz
// 3. Serve the repo with a twist: the canonical Packages.gz path returns the
//    OLD (v1) content (simulating a stale CDN edge), but by-hash paths serve
//    correct content
// 4. Run apt-get update in Docker → should succeed because APT follows
//    Acquire-By-Hash and fetches the correct content via the hash path
//
// Without Acquire-By-Hash, this would fail with:
//   "File has unexpected size (X != Y). Mirror sync in progress?"
func TestStaleCDN(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !hasDocker() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()
	storeDir := t.TempDir()
	store := &storage.FS{Root: storeDir}

	privKey, err := gpg.GenerateTestKey()
	require.NoError(t, err)

	// Publish v1.
	arch := debArch()
	debV1 := createStaleCDNTestDeb(t, "testpkg", "1.0.0", arch)
	_, err = publish.Publish(ctx, store, publish.Options{
		DebPath:       debV1,
		Distribution:  "stable",
		Component:     "main",
		Origin:        "Test",
		Label:         "Test",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	// Capture the v1 Packages.gz content (the "stale" version).
	stalePackagesGz, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/main/binary-"+arch+"/Packages.gz"))
	require.NoError(t, err)
	stalePackages, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/main/binary-"+arch+"/Packages"))
	require.NoError(t, err)

	t.Logf("v1 Packages.gz size: %d bytes", len(stalePackagesGz))
	t.Logf("v1 Packages size: %d bytes", len(stalePackages))

	// Publish v2 — this creates new Packages/Packages.gz with both v1 and v2,
	// and a new Release file pointing to the new hashes.
	debV2 := createStaleCDNTestDeb(t, "testpkg", "2.0.0", arch)
	_, err = publish.Publish(ctx, store, publish.Options{
		DebPath:       debV2,
		Distribution:  "stable",
		Component:     "main",
		Origin:        "Test",
		Label:         "Test",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	currentPackagesGz, err := os.ReadFile(filepath.Join(storeDir, "dists/stable/main/binary-"+arch+"/Packages.gz"))
	require.NoError(t, err)
	t.Logf("v2 Packages.gz size: %d bytes", len(currentPackagesGz))

	// Verify the sizes are different (the whole point of the bug).
	assert.NotEqual(t, len(stalePackagesGz), len(currentPackagesGz),
		"v1 and v2 Packages.gz should be different sizes")

	// Export public key.
	pubKey, err := gpg.ReadPublicKeyFromPrivate(privKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storeDir, "key.asc"), []byte(pubKey), 0o644))

	// Serve the repo with stale CDN simulation:
	// - Canonical Packages and Packages.gz → return v1 content (STALE!)
	// - by-hash paths → serve correct content from disk
	// - Everything else → serve from disk normally
	repoURL := serveStaleCDNRepo(t, storeDir, stalePackages, stalePackagesGz)

	// This is the critical test: apt-get update should succeed despite stale
	// canonical Packages.gz, because it follows Acquire-By-Hash.
	script := fmt.Sprintf(`
set -e
apt-get update -qq
apt-get install -y -qq gnupg curl >/dev/null 2>&1
curl -fsSL %s/key.asc | gpg --dearmor -o /usr/share/keyrings/test.gpg
echo "deb [signed-by=/usr/share/keyrings/test.gpg] %s stable main" > /etc/apt/sources.list.d/test.list
apt-get update 2>&1
echo "APT_UPDATE_SUCCESS"
apt-cache show testpkg | grep Version
`, repoURL, repoURL)

	output := dockerRun(t, "ubuntu:22.04", "bash", "-c", script)
	assert.Contains(t, output, "APT_UPDATE_SUCCESS",
		"apt-get update should succeed with Acquire-By-Hash despite stale canonical Packages.gz")
	assert.Contains(t, output, "Version: 2.0.0",
		"v2 should be available after update")
}

// serveStaleCDNRepo serves the repo but returns stale content for canonical
// Packages and Packages.gz paths (simulating a CDN edge that hasn't been
// invalidated yet).
func serveStaleCDNRepo(t *testing.T, root string, stalePackages, stalePackagesGz []byte) string {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// For canonical Packages paths, serve stale content.
		if strings.HasSuffix(path, "/Packages.gz") && !strings.Contains(path, "by-hash") {
			t.Logf("STALE CDN: serving stale Packages.gz for %s (size %d)", path, len(stalePackagesGz))
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(stalePackagesGz)
			return
		}
		if strings.HasSuffix(path, "/Packages") && !strings.Contains(path, "by-hash") {
			t.Logf("STALE CDN: serving stale Packages for %s (size %d)", path, len(stalePackages))
			w.Write(stalePackages)
			return
		}

		// For everything else (including by-hash), serve from disk.
		t.Logf("CDN: serving from disk: %s", path)
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
	})

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

// createStaleCDNTestDeb is the same as createTestDeb but in this package to avoid import cycles.
func createStaleCDNTestDeb(t *testing.T, pkg, version, arch string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, fmt.Sprintf("%s_%s_%s.deb", pkg, version, arch))

	control := fmt.Sprintf(`Package: %s
Version: %s
Architecture: %s
Maintainer: Test <test@test.com>
Description: Test package
 A test package for integration testing.
`, pkg, version, arch)

	var controlTarGz bytes.Buffer
	gz := gzip.NewWriter(&controlTarGz)
	tw := tar.NewWriter(gz)
	controlBytes := []byte(control)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Size: int64(len(controlBytes)), Mode: 0o644}))
	_, err := tw.Write(controlBytes)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	var dataTarGz bytes.Buffer
	gz2 := gzip.NewWriter(&dataTarGz)
	tw2 := tar.NewWriter(gz2)
	require.NoError(t, tw2.Close())
	require.NoError(t, gz2.Close())

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

	dataData := dataTarGz.Bytes()
	require.NoError(t, w.WriteHeader(&ar.Header{Name: "data.tar.gz", Size: int64(len(dataData)), Mode: 0o100644}))
	_, err = w.Write(dataData)
	require.NoError(t, err)

	return path
}
