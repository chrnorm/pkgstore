package prune

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/chrnorm/pkgstore/internal/storage"
	"github.com/chrnorm/pkgstore/internal/validate"
)

// StorageWithModTime extends the base Storage interface with the ability
// to list objects with their modification times, needed for time-based pruning.
type StorageWithModTime interface {
	storage.Storage
	ListWithModTime(ctx context.Context, prefix string) ([]storage.ObjectWithModTime, error)
}

// Options configures a prune operation.
type Options struct {
	// Distribution is the APT suite, e.g. "stable".
	Distribution string

	// Component is the APT component, e.g. "main".
	Component string

	// OlderThan prunes by-hash entries older than this duration.
	OlderThan time.Duration
}

// Result contains information about what was pruned.
type Result struct {
	Deleted int
}

// Prune removes old by-hash entries from the repository.
func Prune(ctx context.Context, s StorageWithModTime, opts Options) (*Result, error) {
	if err := validate.Name(opts.Distribution, "distribution"); err != nil {
		return nil, err
	}
	if err := validate.Name(opts.Component, "component"); err != nil {
		return nil, err
	}

	// Find the current by-hash entries referenced by the Packages files.
	currentHashes, err := findCurrentHashes(ctx, s, opts.Distribution, opts.Component)
	if err != nil {
		return nil, fmt.Errorf("finding current hashes: %w", err)
	}

	// List all by-hash entries with modification times.
	prefix := fmt.Sprintf("dists/%s/%s/", opts.Distribution, opts.Component)
	objects, err := s.ListWithModTime(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("listing objects: %w", err)
	}

	cutoff := time.Now().Add(-opts.OlderThan)

	var toDelete []string
	for _, obj := range objects {
		if !strings.Contains(obj.Key, "by-hash/") {
			continue
		}
		// Never delete entries referenced by current Packages files.
		if currentHashes[obj.Key] {
			continue
		}
		// Only delete entries older than the cutoff.
		if obj.ModTime.Before(cutoff) {
			toDelete = append(toDelete, obj.Key)
		}
	}

	if len(toDelete) > 0 {
		if err := s.Delete(ctx, toDelete); err != nil {
			return nil, fmt.Errorf("deleting old by-hash entries: %w", err)
		}
	}

	return &Result{Deleted: len(toDelete)}, nil
}

// findCurrentHashes reads the current Packages and Packages.gz files and
// computes their by-hash paths so we never delete entries that are in use.
func findCurrentHashes(ctx context.Context, s storage.Storage, distribution, component string) (map[string]bool, error) {
	prefix := fmt.Sprintf("dists/%s/%s/", distribution, component)
	keys, err := s.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	current := make(map[string]bool)

	for _, key := range keys {
		if strings.Contains(key, "by-hash") {
			continue
		}
		base := key[strings.LastIndex(key, "/")+1:]
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

		h := sha256.Sum256(buf.Bytes())
		dir := key[:strings.LastIndex(key, "/")]
		byHashKey := fmt.Sprintf("%s/by-hash/SHA256/%s", dir, hex.EncodeToString(h[:]))
		current[byHashKey] = true
	}

	return current, nil
}
