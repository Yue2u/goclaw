// Package filesys abstracts tenant-scoped file storage for GoClaw.
// It supports a local disk backend and a filestore gRPC backend.
package filesys

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned when a requested path does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidPath is returned when a path is malformed or escapes the tenant root.
var ErrInvalidPath = errors.New("invalid path")

// Entry describes a single file or directory in storage.
type Entry struct {
	Path        string
	Name        string
	IsDir       bool
	Size        int64
	ModTime     time.Time
	HasChildren bool
}

// WalkFn is called for each entry visited by Walk.
// Returning filepath.SkipDir is not supported by the filestore backend;
// callers that need directory skipping should use List with depth limits.
type WalkFn func(path string, entry Entry) error

// Filesystem abstracts per-tenant file operations.
type Filesystem interface {
	// List returns directory entries under path up to maxDepth levels deep.
	// maxDepth 0 means immediate children only.
	List(ctx context.Context, path string, maxDepth int) ([]Entry, error)

	// Stat returns metadata for a single path.
	Stat(ctx context.Context, path string) (*Entry, error)

	// Get returns a reader for the file at path, its size, and content type.
	// Size may be -1 when the backend cannot determine it cheaply.
	Get(ctx context.Context, path string) (io.ReadCloser, int64, string, error)

	// Put writes the contents of r to path. It returns the canonical storage
	// key/path for the written object, which may be used by callbacks.
	Put(ctx context.Context, path string, r io.Reader, size int64, contentType string) (string, error)

	// Delete removes the file or directory at path.
	Delete(ctx context.Context, path string) error

	// Move renames/moves a file within the same tenant.
	Move(ctx context.Context, from, to string) error

	// Walk visits every entry under root.
	Walk(ctx context.Context, root string, fn WalkFn) error

	// TotalSize returns the total bytes used by the tenant.
	TotalSize(ctx context.Context) (int64, error)
}
