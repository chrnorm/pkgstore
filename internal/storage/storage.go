package storage

import (
	"context"
	"io"
	"time"
)

// Storage is the interface for reading and writing repository files.
type Storage interface {
	// Get retrieves an object by key. Returns ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Put writes an object at the given key.
	Put(ctx context.Context, key string, body io.Reader, contentType string) error

	// List returns all keys with the given prefix.
	List(ctx context.Context, prefix string) ([]string, error)

	// Delete removes the given keys.
	Delete(ctx context.Context, keys []string) error
}

// ObjectWithModTime represents a storage object with its modification time.
type ObjectWithModTime struct {
	Key     string
	ModTime time.Time
}

// ErrNotFound is returned by Get when the key does not exist.
type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return "not found: " + e.Key
}
