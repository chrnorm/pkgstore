package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FS implements Storage using the local filesystem.
type FS struct {
	Root string
}

func (f *FS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(f.Root, key)
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
	path := filepath.Join(f.Root, key)
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
	var keys []string
	root := filepath.Join(f.Root, prefix)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(f.Root, path)
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

func (f *FS) Delete(_ context.Context, keys []string) error {
	for _, key := range keys {
		path := filepath.Join(f.Root, key)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
