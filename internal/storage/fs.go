package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FS implements Storage using the local filesystem.
type FS struct {
	Root string
}

// safePath resolves a key to an absolute path under Root and validates
// that it doesn't escape via path traversal.
func (f *FS) safePath(key string) (string, error) {
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return "", fmt.Errorf("resolving root: %w", err)
	}
	resolved := filepath.Clean(filepath.Join(root, key))
	if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
		return "", fmt.Errorf("path traversal detected: %q resolves outside root", key)
	}
	return resolved, nil
}

func (f *FS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := f.safePath(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ErrNotFound{Key: key}
		}
		return nil, err
	}
	return file, nil
}

func (f *FS) Put(_ context.Context, key string, body io.Reader, _ string) error {
	path, err := f.safePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, body)
	return err
}

func (f *FS) List(_ context.Context, prefix string) ([]string, error) {
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return nil, err
	}
	searchRoot := filepath.Join(root, prefix)

	var keys []string
	err = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			// Normalize to forward slashes for consistency with S3 keys.
			keys = append(keys, strings.ReplaceAll(rel, string(filepath.Separator), "/"))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// ListWithModTime returns objects with modification times from the filesystem.
func (f *FS) ListWithModTime(_ context.Context, prefix string) ([]ObjectWithModTime, error) {
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return nil, err
	}
	searchRoot := filepath.Join(root, prefix)

	var objects []ObjectWithModTime
	_ = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			objects = append(objects, ObjectWithModTime{
				Key:     strings.ReplaceAll(rel, string(filepath.Separator), "/"),
				ModTime: info.ModTime(),
			})
		}
		return nil
	})
	return objects, nil
}

// SetModTime is a test helper to set the modification time of a file.
func (f *FS) SetModTime(key string, t time.Time) error {
	path := filepath.Join(f.Root, key)
	return os.Chtimes(path, t, t)
}

func (f *FS) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		path, err := f.safePath(key)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
