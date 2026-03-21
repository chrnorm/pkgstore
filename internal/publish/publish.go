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
	"github.com/chrnorm/pkgstore/internal/validate"
)

// Options configures a publish operation.
type Options struct {
	// DebPaths is the list of .deb files to publish.
	DebPaths []string

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
	Packages []PackageResult
}

// PackageResult contains information about a single published package.
type PackageResult struct {
	Package      string
	Version      string
	Architecture string
	PoolPath     string
}

// Publish adds one or more .deb packages to the APT repository in storage.
func Publish(ctx context.Context, s storage.Storage, opts Options) (*Result, error) {
	if len(opts.DebPaths) == 0 {
		return nil, fmt.Errorf("no .deb files specified")
	}

	// Validate distribution and component names to prevent path traversal.
	if err := validate.Name(opts.Distribution, "distribution"); err != nil {
		return nil, err
	}
	if err := validate.Name(opts.Component, "component"); err != nil {
		return nil, err
	}

	// 1. Read all .deb files and group by architecture.
	type debInfo struct {
		path     string
		meta     *deb.PackageMetadata
		fi       *deb.FileInfo
		poolPath string
	}

	var debs []debInfo
	archSet := make(map[string]bool)

	for _, debPath := range opts.DebPaths {
		meta, fi, err := deb.ReadDeb(debPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", debPath, err)
		}
		poolPath := fmt.Sprintf("pool/%s/%s_%s_%s.deb", opts.Component, meta.Package, meta.Version, meta.Architecture)
		debs = append(debs, debInfo{path: debPath, meta: meta, fi: fi, poolPath: poolPath})
		archSet[meta.Architecture] = true
	}

	// 2. Load existing repo state for all affected architectures.
	r := repo.New(opts.Distribution, opts.Component)
	r.Origin = opts.Origin
	r.Label = opts.Label
	r.Description = opts.Description

	for arch := range archSet {
		if err := r.LoadArch(ctx, s, arch); err != nil {
			return nil, fmt.Errorf("loading existing packages for %s: %w", arch, err)
		}
	}

	// 3. Add all packages.
	for _, d := range debs {
		r.AddPackage(d.meta, d.fi, d.poolPath)
	}

	// 4. Build index files (Packages, Packages.gz, by-hash).
	indexFiles, releaseEntries, err := r.BuildIndexFiles()
	if err != nil {
		return nil, fmt.Errorf("building index files: %w", err)
	}

	// 5. Load existing Release to carry forward checksums for untouched architectures.
	existingReleaseEntries, err := loadExistingReleaseEntries(ctx, s, opts.Distribution, opts.Component, archSet)
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

	// Upload all .deb files.
	for _, d := range debs {
		debFile, err := os.Open(d.path)
		if err != nil {
			return nil, fmt.Errorf("opening %s for upload: %w", d.path, err)
		}
		err = s.Put(ctx, d.poolPath, debFile, "application/vnd.debian.binary-package")
		debFile.Close()
		if err != nil {
			return nil, fmt.Errorf("uploading %s: %w", d.poolPath, err)
		}
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

	// Upload Release files last, in order of dependency.
	// InRelease is uploaded last because modern apt clients check it first —
	// until InRelease is updated, clients see the old consistent state.
	// If the process fails partway, re-running publish will fix the state
	// (the operation is idempotent).
	distsPrefix := fmt.Sprintf("dists/%s", opts.Distribution)
	if releaseGPG != nil {
		if err := s.Put(ctx, distsPrefix+"/Release.gpg", bytes.NewReader(releaseGPG), ""); err != nil {
			return nil, fmt.Errorf("uploading Release.gpg: %w", err)
		}
	}
	if err := s.Put(ctx, distsPrefix+"/Release", bytes.NewReader(releaseContent), ""); err != nil {
		return nil, fmt.Errorf("uploading Release: %w", err)
	}
	if inRelease != nil {
		if err := s.Put(ctx, distsPrefix+"/InRelease", bytes.NewReader(inRelease), ""); err != nil {
			return nil, fmt.Errorf("uploading InRelease: %w", err)
		}
	}

	result := &Result{}
	for _, d := range debs {
		result.Packages = append(result.Packages, PackageResult{
			Package:      d.meta.Package,
			Version:      d.meta.Version,
			Architecture: d.meta.Architecture,
			PoolPath:     d.poolPath,
		})
	}
	return result, nil
}

// loadExistingReleaseEntries loads IndexFileEntries for architectures NOT in skipArchs.
func loadExistingReleaseEntries(ctx context.Context, s storage.Storage, distribution, component string, skipArchs map[string]bool) ([]index.IndexFileEntry, error) {
	prefix := fmt.Sprintf("dists/%s/%s/", distribution, component)
	keys, err := s.List(ctx, prefix)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}

	var result []index.IndexFileEntry
	for _, key := range keys {
		if strings.Contains(key, "by-hash") {
			continue
		}
		archDir := extractArchDir(key)
		if archDir == "" {
			continue
		}
		arch := strings.TrimPrefix(archDir, "binary-")
		if skipArchs[arch] {
			continue
		}
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

		relPath := strings.TrimPrefix(key, fmt.Sprintf("dists/%s/", distribution))
		result = append(result, index.IndexFileEntry{
			RelativePath: relPath,
			Content:      buf.Bytes(),
		})
	}

	return result, nil
}

func extractArchDir(key string) string {
	parts := strings.Split(key, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "binary-") {
			return p
		}
	}
	return ""
}
