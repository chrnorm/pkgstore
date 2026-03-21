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
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/blakesmith/ar"
	"github.com/chrnorm/pkgstore/internal/gpg"
	"github.com/chrnorm/pkgstore/internal/publish"
	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// debArch returns the Debian architecture name for the current platform.
func debArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "i386"
	default:
		return runtime.GOARCH
	}
}

func hasDocker() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

func createTestDeb(t *testing.T, pkg, version, arch string) string {
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

// serveRepo starts an HTTP server on a random port and returns the base URL
// accessible from Docker via host.docker.internal.
func serveRepo(t *testing.T, root string) string {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{Handler: http.FileServer(http.Dir(root))}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })

	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

func dockerRun(t *testing.T, image string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{
		"run", "--rm",
		"--add-host=host.docker.internal:host-gateway",
		image,
	}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	// Combine stdout and stderr so we can see all output.
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	err := cmd.Run()
	output := combined.String()
	if err != nil {
		t.Fatalf("docker run failed: %v\noutput:\n%s", err, output)
	}
	return output
}

func TestAPTInstall(t *testing.T) {
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

	arch := debArch()
	debPath := createTestDeb(t, "testpkg", "1.0.0", arch)
	_, err = publish.Publish(ctx, store, publish.Options{
		DebPaths: []string{debPath},
		Distribution:  "stable",
		Component:     "main",
		Origin:        "Test",
		Label:         "Test",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	pubKey, err := gpg.ReadPublicKeyFromPrivate(privKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storeDir, "key.asc"), []byte(pubKey), 0o644))

	repoURL := serveRepo(t, storeDir)

	script := fmt.Sprintf(`
set -e
apt-get update -qq
apt-get install -y -qq gnupg curl >/dev/null 2>&1
curl -fsSL %s/key.asc | gpg --dearmor -o /usr/share/keyrings/test.gpg
echo "deb [signed-by=/usr/share/keyrings/test.gpg] %s stable main" > /etc/apt/sources.list.d/test.list
apt-get update
apt-get install -y testpkg
dpkg -s testpkg | grep Version
`, repoURL, repoURL)

	output := dockerRun(t, "ubuntu:22.04", "bash", "-c", script)
	assert.Contains(t, output, "Version: 1.0.0")
}

func TestAPTUpgrade(t *testing.T) {
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

	arch := debArch()
	debV1 := createTestDeb(t, "testpkg", "1.0.0", arch)
	_, err = publish.Publish(ctx, store, publish.Options{
		DebPaths: []string{debV1},
		Distribution:  "stable",
		Component:     "main",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	debV2 := createTestDeb(t, "testpkg", "2.0.0", arch)
	_, err = publish.Publish(ctx, store, publish.Options{
		DebPaths: []string{debV2},
		Distribution:  "stable",
		Component:     "main",
		GPGPrivateKey: privKey,
	})
	require.NoError(t, err)

	pubKey, err := gpg.ReadPublicKeyFromPrivate(privKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storeDir, "key.asc"), []byte(pubKey), 0o644))

	repoURL := serveRepo(t, storeDir)

	script := fmt.Sprintf(`
set -e
apt-get update -qq
apt-get install -y -qq gnupg curl >/dev/null 2>&1
curl -fsSL %s/key.asc | gpg --dearmor -o /usr/share/keyrings/test.gpg
echo "deb [signed-by=/usr/share/keyrings/test.gpg] %s stable main" > /etc/apt/sources.list.d/test.list
apt-get update -qq
apt-get install -y testpkg
dpkg -s testpkg | grep Version
`, repoURL, repoURL)

	output := dockerRun(t, "ubuntu:22.04", "bash", "-c", script)
	assert.Contains(t, output, "Version: 2.0.0")
}
