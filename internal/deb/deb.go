package deb

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blakesmith/ar"
	"github.com/chrnorm/pkgstore/internal/validate"
)

// maxControlSize is the maximum decompressed size of the control archive.
// Control files are typically a few KB; 10MB is a generous limit that
// prevents decompression bomb attacks.
const maxControlSize = 10 * 1024 * 1024

// PackageMetadata holds the control file metadata from a .deb package.
type PackageMetadata struct {
	Package       string
	Version       string
	Architecture  string
	Maintainer    string
	InstalledSize string
	Depends       string
	PreDepends    string
	Priority      string
	Section       string
	Homepage      string
	Description   string
}

// FileInfo holds file-level information about a .deb package.
type FileInfo struct {
	Size   int64
	MD5    string
	SHA1   string
	SHA256 string
}

// ReadDeb reads a .deb file and returns its control metadata and file info.
func ReadDeb(path string) (*PackageMetadata, *FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening deb: %w", err)
	}
	defer f.Close()

	// Compute hashes and size while reading.
	md5h := md5.New()
	sha1h := sha1.New()
	sha256h := sha256.New()
	hasher := io.MultiWriter(md5h, sha1h, sha256h)

	size, err := io.Copy(hasher, f)
	if err != nil {
		return nil, nil, fmt.Errorf("hashing deb: %w", err)
	}

	fi := &FileInfo{
		Size:   size,
		MD5:    hex.EncodeToString(md5h.Sum(nil)),
		SHA1:   hex.EncodeToString(sha1h.Sum(nil)),
		SHA256: hex.EncodeToString(sha256h.Sum(nil)),
	}

	// Seek back to start to read the ar archive.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("seeking deb: %w", err)
	}

	meta, err := readControl(f)
	if err != nil {
		return nil, nil, err
	}

	return meta, fi, nil
}

func readControl(r io.Reader) (*PackageMetadata, error) {
	reader := ar.NewReader(r)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading ar entry: %w", err)
		}

		name := strings.TrimRight(header.Name, "/")

		// The control archive can be control.tar.gz, control.tar.xz, or control.tar.zst.
		// We support control.tar.gz and uncompressed control.tar.
		if name == "control.tar.gz" {
			gz, err := gzip.NewReader(reader)
			if err != nil {
				return nil, fmt.Errorf("decompressing control.tar.gz: %w", err)
			}
			defer gz.Close()
			return readControlTar(io.LimitReader(gz, maxControlSize))
		}

		if name == "control.tar" {
			return readControlTar(io.LimitReader(reader, maxControlSize))
		}
	}

	return nil, fmt.Errorf("control archive not found in .deb")
}

func readControlTar(r io.Reader) (*PackageMetadata, error) {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading control tar: %w", err)
		}

		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "control" {
			return parseControl(tr)
		}
	}

	return nil, fmt.Errorf("control file not found in control archive")
}

func parseControl(r io.Reader) (*PackageMetadata, error) {
	meta := &PackageMetadata{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentField string
	for scanner.Scan() {
		line := scanner.Text()

		// Continuation line (starts with space or tab).
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if currentField == "Description" {
				meta.Description += "\n" + line
			}
			continue
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]
		currentField = key

		switch key {
		case "Package":
			meta.Package = value
		case "Version":
			meta.Version = value
		case "Architecture":
			meta.Architecture = value
		case "Maintainer":
			meta.Maintainer = value
		case "Installed-Size":
			meta.InstalledSize = value
		case "Depends":
			meta.Depends = value
		case "Pre-Depends":
			meta.PreDepends = value
		case "Priority":
			meta.Priority = value
		case "Section":
			meta.Section = value
		case "Homepage":
			meta.Homepage = value
		case "Description":
			meta.Description = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning control file: %w", err)
	}

	if meta.Package == "" {
		return nil, fmt.Errorf("control file missing Package field")
	}
	if meta.Version == "" {
		return nil, fmt.Errorf("control file missing Version field")
	}
	if meta.Architecture == "" {
		return nil, fmt.Errorf("control file missing Architecture field")
	}

	// Validate fields used in path construction to prevent path traversal.
	if err := validate.Name(meta.Package, "Package"); err != nil {
		return nil, err
	}
	if err := validate.Name(meta.Version, "Version"); err != nil {
		return nil, err
	}
	if err := validate.Name(meta.Architecture, "Architecture"); err != nil {
		return nil, err
	}

	return meta, nil
}
