package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestName_Valid(t *testing.T) {
	valid := []string{
		"granted",
		"granted-cli",
		"0.39.0",
		"1.0.0~beta1",
		"amd64",
		"arm64",
		"i386",
		"stable",
		"main",
		"1:2.3.4+dfsg-1",
	}
	for _, v := range valid {
		assert.NoError(t, Name(v, "test"), "should be valid: %q", v)
	}
}

func TestName_Invalid(t *testing.T) {
	cases := []struct {
		value string
		desc  string
	}{
		{"", "empty"},
		{"../etc/passwd", "path traversal with ../"},
		{"foo/bar", "forward slash"},
		{"foo\\bar", "backslash"},
		{"foo\x00bar", "null byte"},
		{".hidden", "starts with dot"},
		{"../../dists/stable/Release", "deep traversal"},
	}
	for _, tc := range cases {
		assert.Error(t, Name(tc.value, "test"), "should be invalid (%s): %q", tc.desc, tc.value)
	}
}
