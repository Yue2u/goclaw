package filesys

import (
	"context"
	"io"
	"mime"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/filestoreclient"
)

const maxListDepth = int(^uint(0) >> 1) // MaxInt

// FilestoreFilesystem implements Filesystem over the filestore gRPC service.
type FilestoreFilesystem struct {
	client     *filestoreclient.Client
	tenantSlug string
}

// NewFilestoreFilesystem creates a filestore-backed filesystem for tenantSlug.
func NewFilestoreFilesystem(client *filestoreclient.Client, tenantSlug string) *FilestoreFilesystem {
	return &FilestoreFilesystem{client: client, tenantSlug: tenantSlug}
}

func (fs *FilestoreFilesystem) toEntry(e filestoreclient.Entry) Entry {
	return Entry{
		Path:        e.Path,
		Name:        e.Name,
		IsDir:       e.IsDir,
		Size:        e.Size,
		ModTime:     time.Unix(e.ModTimeUnix, 0),
		HasChildren: e.HasChildren,
	}
}

// List returns directory entries under path up to maxDepth levels deep.
func (fs *FilestoreFilesystem) List(ctx context.Context, path string, maxDepth int) ([]Entry, error) {
	resp, err := fs.client.List(ctx, fs.tenantSlug, path, maxDepth)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(resp))
	for i, e := range resp {
		entries[i] = fs.toEntry(e)
	}
	return entries, nil
}

// Stat returns metadata for path.
func (fs *FilestoreFilesystem) Stat(ctx context.Context, path string) (*Entry, error) {
	e, err := fs.client.Stat(ctx, fs.tenantSlug, path)
	if err != nil {
		return nil, err
	}
	entry := fs.toEntry(*e)
	return &entry, nil
}

// Get returns a reader for the file at path plus its size and content type.
func (fs *FilestoreFilesystem) Get(ctx context.Context, path string) (io.ReadCloser, int64, string, error) {
	stat, err := fs.client.Stat(ctx, fs.tenantSlug, path)
	if err != nil {
		return nil, 0, "", err
	}
	rc, err := fs.client.Get(ctx, fs.tenantSlug, path)
	if err != nil {
		return nil, 0, "", err
	}
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return rc, stat.Size, ct, nil
}

// Put writes r to path in the filestore.
func (fs *FilestoreFilesystem) Put(ctx context.Context, path string, r io.Reader, size int64, contentType string) (string, error) {
	return fs.client.Put(ctx, fs.tenantSlug, path, r, size, contentType)
}

// Delete removes the file or directory at path.
func (fs *FilestoreFilesystem) Delete(ctx context.Context, path string) error {
	stat, err := fs.client.Stat(ctx, fs.tenantSlug, path)
	if err != nil {
		return err
	}
	return fs.client.Delete(ctx, fs.tenantSlug, path, stat.IsDir)
}

// Move renames from to to within the same tenant.
func (fs *FilestoreFilesystem) Move(ctx context.Context, from, to string) error {
	return fs.client.Move(ctx, fs.tenantSlug, from, to)
}

// Walk visits every entry under root.
func (fs *FilestoreFilesystem) Walk(ctx context.Context, root string, fn WalkFn) error {
	entries, err := fs.client.List(ctx, fs.tenantSlug, root, maxListDepth)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := fn(e.Path, fs.toEntry(e)); err != nil {
			return err
		}
	}
	return nil
}

// TotalSize returns the total bytes used by the tenant.
func (fs *FilestoreFilesystem) TotalSize(ctx context.Context) (int64, error) {
	return fs.client.TotalSize(ctx, fs.tenantSlug)
}

// compile-time interface check.
var _ Filesystem = (*FilestoreFilesystem)(nil)
