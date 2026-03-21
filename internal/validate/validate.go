package validate

import (
	"fmt"
	"regexp"
	"strings"
)

// safeName matches valid Debian package names, versions, architectures,
// distribution names, and component names.
// Allowed: alphanumeric, hyphens, dots, tildes, plus signs, colons, underscores.
var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+:~-]*$`)

// Name validates that a string is safe for use in file paths.
// It rejects empty strings, path traversal sequences, slashes, and null bytes.
func Name(value, field string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s contains null byte", field)
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("%s contains path separator: %q", field, value)
	}
	if strings.Contains(value, "..") {
		return fmt.Errorf("%s contains path traversal: %q", field, value)
	}
	if !safeName.MatchString(value) {
		return fmt.Errorf("%s contains invalid characters: %q", field, value)
	}
	return nil
}
