package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/chrnorm/pkgstore/internal/deb"
	"github.com/chrnorm/pkgstore/internal/gpg"
	"github.com/chrnorm/pkgstore/internal/index"
	"github.com/chrnorm/pkgstore/internal/repo"
	"github.com/chrnorm/pkgstore/internal/storage"
)

// Options configures a publish operation.
type Options struct {
	// DebPath is the path to the .deb file to publish.
	DebPath string

	// Distribution is the APT suite, e.g. "stable".
	Distribution string

	// Component is the APT component, e.g. "main".
	Component string

	// Origin, Label, Description are optional Release file metadata.
	Origin      string
	Label       string
	Description string

	// GPGPrivateKey is the ASCII-armored GPG private key for signing.
	// If empty, signing is skipped.
	GPGPrivateKey string

	// GPGPassphrase is the passphrase for the GPG key, if encrypted.
	GPGPassphrase string
}

// Result contains information about what was published.
type Result struct {
	Package      string
	Version      string
	Architecture string
	PoolPath     string
}

// Publish adds a .deb package to the APT repository in storage.
func Publish(ctx context.Context, s storage.Storage, opts Options) (*Result, error) {
	// 1. Read the .deb file.
	meta, fi, err := deb.ReadDeb(opts.DebPath)
	if err != nil {
		return nil, fmt.Errorf("reading deb: %w", err)
	}

	poolPath := fmt.Sprintf("pool/%s/%s_%s_%s.deb", opts.Component, meta.Package, meta.Version, meta.Architecture)

	// 2. Load existing repo state for this architecture.
	r := repo.New(opts.Distribution, opts.Component)
	r.Origin = opts.Origin
	r.Label = opts.Label
	r.Description = opts.Description

	if err := r.LoadArch(ctx, s, meta.Architecture); err != nil {
		return nil, fmt.Errorf("loading existing packages: %w", err)
	}

	// 3. Add the new package.
	r.AddPackage(meta, fi, poolPath)

	// 4. Build index files (Packages, Packages.gz, by-hash).
	indexFiles, releaseEntries, err := r.BuildIndexFiles()
	if err != nil {
		return nil, fmt.Errorf("building index files: %w", err)
	}

	// 5. Load existing Release to carry forward checksums for other architectures.
	existingReleaseEntries, err := loadExistingReleaseEntries(ctx, s, opts.Distribution, opts.Component, meta.Architecture)
	if err != nil {
		return nil, fmt.Errorf("loading existing release entries: %w", err)
	}

	// 6. Build Release file.
	releaseContent, err := r.BuildRelease(releaseEntries, existingReleaseEntries)
	if err != nil {
		return nil, fmt.Errorf("building release: %w", err)
	}

	// 7. Sign Release file (if GPG key provided).
	var releaseGPG, inRelease []byte
	if opts.GPGPrivateKey != "" {
		signer, err := gpg.NewSigner(opts.GPGPrivateKey, opts.GPGPassphrase)
		if err != nil {
			return nil, fmt.Errorf("creating GPG signer: %w", err)
		}

		releaseGPG, err = signer.DetachedSign(releaseContent)
		if err != nil {
			return nil, fmt.Errorf("signing Release: %w", err)
		}

		inRelease, err = signer.ClearSign(releaseContent)
		if err != nil {
			return nil, fmt.Errorf("clearsigning Release: %w", err)
		}
	}

	// 8. Upload in order: .deb + by-hash first, then Packages, then Release last.

	// Upload the .deb file.
	debFile, err := os.Open(opts.DebPath)
	if err != nil {
		return nil, fmt.Errorf("opening deb for upload: %w", err)
	}
	defer debFile.Close()
	if err := s.Put(ctx, poolPath, debFile, "application/vnd.debian.binary-package"); err != nil {
		return nil, fmt.Errorf("uploading deb: %w", err)
	}

	// Upload by-hash files first (content-addressed, safe to upload anytime).
	for filePath, content := range indexFiles {
		if strings.Contains(filePath, "by-hash") {
			if err := s.Put(ctx, filePath, bytes.NewReader(content), ""); err != nil {
				return nil, fmt.Errorf("uploading %s: %w", filePath, err)
			}
		}
	}

	// Upload Packages files at canonical paths.
	for filePath, content := range indexFiles {
		if !strings.Contains(filePath, "by-hash") {
			if err := s.Put(ctx, filePath, bytes.NewReader(content), ""); err != nil {
				return nil, fmt.Errorf("uploading %s: %w", filePath, err)
			}
		}
	}

	// Upload Release, Release.gpg, InRelease last.
	distsPrefix := fmt.Sprintf("dists/%s", opts.Distribution)
	if err := s.Put(ctx, distsPrefix+"/Release", bytes.NewReader(releaseContent), ""); err != nil {
		return nil, fmt.Errorf("uploading Release: %w", err)
	}
	if releaseGPG != nil {
		if err := s.Put(ctx, distsPrefix+"/Release.gpg", bytes.NewReader(releaseGPG), ""); err != nil {
			return nil, fmt.Errorf("uploading Release.gpg: %w", err)
		}
	}
	if inRelease != nil {
		if err := s.Put(ctx, distsPrefix+"/InRelease", bytes.NewReader(inRelease), ""); err != nil {
			return nil, fmt.Errorf("uploading InRelease: %w", err)
		}
	}

	return &Result{
		Package:      meta.Package,
		Version:      meta.Version,
		Architecture: meta.Architecture,
		PoolPath:     poolPath,
	}, nil
}

// loadExistingReleaseEntries loads the existing Release file and returns IndexFileEntries
// for architectures other than the one being updated. This allows the new Release file
// to include checksums for all architectures without re-downloading their Packages files.
func loadExistingReleaseEntries(ctx context.Context, s storage.Storage, distribution, component, skipArch string) ([]index.IndexFileEntry, error) {
	releaseKey := fmt.Sprintf("dists/%s/Release", distribution)
	rc, err := s.Get(ctx, releaseKey)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			return nil, nil // First publish, no existing Release.
		}
		return nil, err
	}
	defer rc.Close()

	// Parse the existing Release file to find entries for other architectures.
	// We need to re-download the actual Packages files for those architectures
	// to get their content for checksum computation.
	// This is simpler than parsing checksums from the Release file.
	entries, err := index.ReadPackages(rc)
	_ = entries // We don't parse Release as Packages; we need a different approach.

	// Instead, list the existing Packages files for other architectures.
	prefix := fmt.Sprintf("dists/%s/%s/", distribution, component)
	keys, err := s.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	var result []index.IndexFileEntry
	for _, key := range keys {
		// Skip by-hash entries and the architecture we're updating.
		if strings.Contains(key, "by-hash") {
			continue
		}
		archDir := extractArchDir(key)
		if archDir == "" || archDir == "binary-"+skipArch {
			continue
		}
		// Only include Packages and Packages.gz files.
		base := path.Base(key)
		if base != "Packages" && base != "Packages.gz" {
			continue
		}

		rc, err := s.Get(ctx, key)
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()

		// Make the path relative to dists/{distribution}/.
		relPath := strings.TrimPrefix(key, fmt.Sprintf("dists/%s/", distribution))
		result = append(result, index.IndexFileEntry{
			RelativePath: relPath,
			Content:      buf.Bytes(),
		})
	}

	return result, nil
}

// extractArchDir extracts the "binary-{arch}" directory from a path like
// "dists/stable/main/binary-amd64/Packages".
func extractArchDir(key string) string {
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "binary-") {
			return p
		}
	}
	return ""
}
