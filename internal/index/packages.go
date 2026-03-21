package index

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sort"
	"strings"
)

// PackageEntry represents a single package stanza in a Packages file.
type PackageEntry struct {
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
	Filename      string
	Size          int64
	MD5sum        string
	SHA1          string
	SHA256        string
}

// WritePackages writes a Packages file from the given entries.
// Entries are sorted by (Package, Version, Architecture) for deterministic output.
func WritePackages(w io.Writer, entries []PackageEntry) error {
	sorted := make([]PackageEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Package != sorted[j].Package {
			return sorted[i].Package < sorted[j].Package
		}
		if sorted[i].Version != sorted[j].Version {
			return sorted[i].Version < sorted[j].Version
		}
		return sorted[i].Architecture < sorted[j].Architecture
	})

	for i, e := range sorted {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := writeEntry(w, e); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(w io.Writer, e PackageEntry) error {
	fields := []struct {
		key   string
		value string
	}{
		{"Package", e.Package},
		{"Version", e.Version},
		{"Architecture", e.Architecture},
		{"Maintainer", e.Maintainer},
		{"Installed-Size", e.InstalledSize},
		{"Depends", e.Depends},
		{"Pre-Depends", e.PreDepends},
		{"Priority", e.Priority},
		{"Section", e.Section},
		{"Homepage", e.Homepage},
		{"Description", e.Description},
		{"Filename", e.Filename},
		{"Size", fmt.Sprintf("%d", e.Size)},
		{"MD5sum", e.MD5sum},
		{"SHA1", e.SHA1},
		{"SHA256", e.SHA256},
	}

	for _, f := range fields {
		if f.value == "" || (f.key == "Size" && f.value == "0") {
			continue
		}
		// Description can be multi-line; the first line goes on the same line,
		// continuation lines are already prefixed with space from the control file.
		if _, err := fmt.Fprintf(w, "%s: %s\n", f.key, f.value); err != nil {
			return err
		}
	}
	return nil
}

// ReadPackages parses a Packages file and returns the entries.
func ReadPackages(r io.Reader) ([]PackageEntry, error) {
	var entries []PackageEntry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var current *PackageEntry
	var currentField string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line separates entries.
		if line == "" {
			if current != nil {
				entries = append(entries, *current)
				current = nil
			}
			currentField = ""
			continue
		}

		// Continuation line.
		if line[0] == ' ' || line[0] == '\t' {
			if current != nil && currentField == "Description" {
				current.Description += "\n" + line
			}
			continue
		}

		if current == nil {
			current = &PackageEntry{}
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
			current.Package = value
		case "Version":
			current.Version = value
		case "Architecture":
			current.Architecture = value
		case "Maintainer":
			current.Maintainer = value
		case "Installed-Size":
			current.InstalledSize = value
		case "Depends":
			current.Depends = value
		case "Pre-Depends":
			current.PreDepends = value
		case "Priority":
			current.Priority = value
		case "Section":
			current.Section = value
		case "Homepage":
			current.Homepage = value
		case "Description":
			current.Description = value
		case "Filename":
			current.Filename = value
		case "Size":
			fmt.Sscanf(value, "%d", &current.Size)
		case "MD5sum":
			current.MD5sum = value
		case "SHA1":
			current.SHA1 = value
		case "SHA256":
			current.SHA256 = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Don't forget the last entry if file doesn't end with a blank line.
	if current != nil {
		entries = append(entries, *current)
	}

	return entries, nil
}

// CompressPackages gzips the given Packages content.
func CompressPackages(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := gz.Write(data); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
