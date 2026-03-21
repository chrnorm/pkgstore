package index

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// ReleaseConfig holds the configuration for generating a Release file.
type ReleaseConfig struct {
	Origin        string
	Label         string
	Suite         string
	Codename      string
	Architectures []string
	Components    []string
	Description   string
	Date          time.Time
}

// IndexFileEntry represents a file to be listed in the Release checksums.
type IndexFileEntry struct {
	// RelativePath is the path relative to dists/{suite}/, e.g. "main/binary-amd64/Packages".
	RelativePath string
	Content      []byte
}

// WriteRelease generates a Release file with Acquire-By-Hash enabled.
func WriteRelease(w io.Writer, cfg ReleaseConfig, files []IndexFileEntry) error {
	if cfg.Date.IsZero() {
		cfg.Date = time.Now().UTC()
	}

	fields := []struct {
		key   string
		value string
	}{
		{"Origin", cfg.Origin},
		{"Label", cfg.Label},
		{"Suite", cfg.Suite},
		{"Codename", cfg.Codename},
		{"Architectures", strings.Join(cfg.Architectures, " ")},
		{"Components", strings.Join(cfg.Components, " ")},
		{"Description", cfg.Description},
		{"Date", cfg.Date.UTC().Format(time.RFC1123Z)},
		{"Acquire-By-Hash", "yes"},
	}

	for _, f := range fields {
		if f.value == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", f.key, f.value); err != nil {
			return err
		}
	}

	// Sort files for deterministic output.
	sorted := make([]IndexFileEntry, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RelativePath < sorted[j].RelativePath
	})

	// MD5Sum section.
	if _, err := fmt.Fprintln(w, "MD5Sum:"); err != nil {
		return err
	}
	for _, f := range sorted {
		h := md5.Sum(f.Content)
		if _, err := fmt.Fprintf(w, " %s %16d %s\n", hex.EncodeToString(h[:]), len(f.Content), f.RelativePath); err != nil {
			return err
		}
	}

	// SHA1 section.
	if _, err := fmt.Fprintln(w, "SHA1:"); err != nil {
		return err
	}
	for _, f := range sorted {
		h := sha1.Sum(f.Content)
		if _, err := fmt.Fprintf(w, " %s %16d %s\n", hex.EncodeToString(h[:]), len(f.Content), f.RelativePath); err != nil {
			return err
		}
	}

	// SHA256 section.
	if _, err := fmt.Fprintln(w, "SHA256:"); err != nil {
		return err
	}
	for _, f := range sorted {
		h := sha256.Sum256(f.Content)
		if _, err := fmt.Fprintf(w, " %s %16d %s\n", hex.EncodeToString(h[:]), len(f.Content), f.RelativePath); err != nil {
			return err
		}
	}

	return nil
}

// ByHashPath returns the by-hash path for the given content.
// dir is the directory containing the file, e.g. "main/binary-amd64".
// The returned path is relative to dists/{suite}/.
func ByHashPath(dir string, content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%s/by-hash/SHA256/%s", dir, hex.EncodeToString(h[:]))
}
