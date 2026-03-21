package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chrnorm/pkgstore/internal/deb"
	"github.com/chrnorm/pkgstore/internal/index"
	"github.com/chrnorm/pkgstore/internal/storage"
)

// Repository represents the state of an APT repository for a single distribution and component.
type Repository struct {
	Distribution string
	Component    string
	Origin       string
	Label        string
	Description  string

	// packages is keyed by architecture.
	packages map[string][]index.PackageEntry
}

// New creates a new empty Repository.
func New(distribution, component string) *Repository {
	return &Repository{
		Distribution: distribution,
		Component:    component,
		packages:     make(map[string][]index.PackageEntry),
	}
}

// LoadArch loads existing package entries for a specific architecture from storage.
// If the Packages file doesn't exist, the architecture starts empty.
func (r *Repository) LoadArch(ctx context.Context, s storage.Storage, arch string) error {
	key := fmt.Sprintf("dists/%s/%s/binary-%s/Packages", r.Distribution, r.Component, arch)
	rc, err := s.Get(ctx, key)
	if err != nil {
		var notFound *storage.ErrNotFound
		if errors.As(err, &notFound) {
			r.packages[arch] = nil
			return nil
		}
		return fmt.Errorf("loading packages for %s: %w", arch, err)
	}
	defer rc.Close()

	entries, err := index.ReadPackages(rc)
	if err != nil {
		return fmt.Errorf("parsing packages for %s: %w", arch, err)
	}

	r.packages[arch] = entries
	return nil
}

// AddPackage adds or replaces a package in the repository.
// The architecture is taken from the package metadata.
func (r *Repository) AddPackage(meta *deb.PackageMetadata, fi *deb.FileInfo, poolPath string) {
	arch := meta.Architecture

	entry := index.PackageEntry{
		Package:       meta.Package,
		Version:       meta.Version,
		Architecture:  meta.Architecture,
		Maintainer:    meta.Maintainer,
		InstalledSize: meta.InstalledSize,
		Depends:       meta.Depends,
		PreDepends:    meta.PreDepends,
		Priority:      meta.Priority,
		Section:       meta.Section,
		Homepage:      meta.Homepage,
		Description:   meta.Description,
		Filename:      poolPath,
		Size:          fi.Size,
		MD5sum:        fi.MD5,
		SHA1:          fi.SHA1,
		SHA256:        fi.SHA256,
	}

	// Replace existing entry with same (Package, Version, Architecture) or append.
	entries := r.packages[arch]
	found := false
	for i, e := range entries {
		if e.Package == entry.Package && e.Version == entry.Version && e.Architecture == entry.Architecture {
			entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	r.packages[arch] = entries
}

// Architectures returns the architectures that have packages.
func (r *Repository) Architectures() []string {
	var archs []string
	for arch, entries := range r.packages {
		if len(entries) > 0 {
			archs = append(archs, arch)
		}
	}
	return archs
}

// FileMap represents all files that need to be written, keyed by their storage path.
type FileMap map[string][]byte

// BuildIndexFiles generates all index files for the repository.
// Returns a map of storage path -> content for all files that need to be uploaded.
// This includes Packages, Packages.gz, and by-hash entries for each architecture.
// It does NOT include the Release file (use BuildRelease for that).
func (r *Repository) BuildIndexFiles() (FileMap, []index.IndexFileEntry, error) {
	files := make(FileMap)
	var releaseEntries []index.IndexFileEntry

	for arch, entries := range r.packages {
		dir := fmt.Sprintf("%s/binary-%s", r.Component, arch)
		distsDir := fmt.Sprintf("dists/%s/%s", r.Distribution, dir)

		// Generate Packages file.
		var buf bytes.Buffer
		if err := index.WritePackages(&buf, entries); err != nil {
			return nil, nil, fmt.Errorf("writing packages for %s: %w", arch, err)
		}
		packagesContent := buf.Bytes()

		// Generate Packages.gz.
		packagesGz, err := index.CompressPackages(packagesContent)
		if err != nil {
			return nil, nil, fmt.Errorf("compressing packages for %s: %w", arch, err)
		}

		// Canonical paths.
		files[distsDir+"/Packages"] = packagesContent
		files[distsDir+"/Packages.gz"] = packagesGz

		// By-hash paths.
		files[fmt.Sprintf("dists/%s/%s", r.Distribution, ByHashPath(dir, packagesContent))] = packagesContent
		files[fmt.Sprintf("dists/%s/%s", r.Distribution, ByHashPath(dir, packagesGz))] = packagesGz

		// Entries for the Release file (paths relative to dists/{suite}/).
		releaseEntries = append(releaseEntries,
			index.IndexFileEntry{RelativePath: dir + "/Packages", Content: packagesContent},
			index.IndexFileEntry{RelativePath: dir + "/Packages.gz", Content: packagesGz},
		)
	}

	return files, releaseEntries, nil
}

// BuildRelease generates the Release file content.
// existingReleaseEntries can include entries for architectures not being updated,
// so the Release file covers all architectures.
func (r *Repository) BuildRelease(indexEntries []index.IndexFileEntry, extraEntries []index.IndexFileEntry) ([]byte, error) {
	allEntries := append(indexEntries, extraEntries...)

	// Deduplicate by RelativePath, preferring the first occurrence (from indexEntries).
	seen := make(map[string]bool)
	var deduped []index.IndexFileEntry
	for _, e := range allEntries {
		if !seen[e.RelativePath] {
			seen[e.RelativePath] = true
			deduped = append(deduped, e)
		}
	}

	// Collect all architectures from the entries.
	archSet := make(map[string]bool)
	for _, e := range deduped {
		// Extract arch from path like "main/binary-amd64/Packages".
		parts := strings.Split(e.RelativePath, "/")
		for _, p := range parts {
			if strings.HasPrefix(p, "binary-") {
				archSet[strings.TrimPrefix(p, "binary-")] = true
			}
		}
	}
	var archs []string
	for a := range archSet {
		archs = append(archs, a)
	}

	cfg := index.ReleaseConfig{
		Origin:        r.Origin,
		Label:         r.Label,
		Suite:         r.Distribution,
		Codename:      r.Distribution,
		Architectures: archs,
		Components:    []string{r.Component},
		Description:   r.Description,
	}

	var buf bytes.Buffer
	if err := index.WriteRelease(&buf, cfg, deduped); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
