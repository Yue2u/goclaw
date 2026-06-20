package filestoreclient

import (
	"context"
	"io"

	filestorev1 "github.com/nextlevelbuilder/goclaw/filestore/gen/filestore/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Entry mirrors the proto Entry for callers that don't want to import proto directly.
type Entry struct {
	Path        string
	Name        string
	IsDir       bool
	Size        int64
	ModTimeUnix int64
	HasChildren bool
}

// Client is a thin gRPC wrapper around the filestore service.
type Client struct {
	conn filestorev1.FileStoreClient
}

// NewClient connects to addr (e.g. "filestore:5300") and returns a Client.
func NewClient(addr string) (*Client, error) {
	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{conn: filestorev1.NewFileStoreClient(cc)}, nil
}

// Put streams r to filestore at tenantSlug/path.
func (c *Client) Put(ctx context.Context, tenantSlug, path string, r io.Reader, size int64, ct string) (string, error) {
	stream, err := c.conn.Put(ctx)
	if err != nil {
		return "", err
	}
	if err := stream.Send(&filestorev1.PutChunk{
		Data: &filestorev1.PutChunk_Meta{Meta: &filestorev1.PutMeta{
			TenantSlug:  tenantSlug,
			Path:        path,
			ContentType: ct,
			Size:        size,
		}},
	}); err != nil {
		return "", err
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if serr := stream.Send(&filestorev1.PutChunk{
				Data: &filestorev1.PutChunk_Chunk{Chunk: buf[:n]},
			}); serr != nil {
				return "", serr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return "", err
	}
	return resp.Key, nil
}

// Get streams file bytes from filestore.
func (c *Client) Get(ctx context.Context, tenantSlug, path string) (io.ReadCloser, error) {
	stream, err := c.conn.Get(ctx, &filestorev1.GetRequest{TenantSlug: tenantSlug, Path: path})
	if err != nil {
		return nil, err
	}
	return &streamReader{stream: stream}, nil
}

// Delete removes a file (or directory tree when recursive=true).
func (c *Client) Delete(ctx context.Context, tenantSlug, path string, recursive bool) error {
	_, err := c.conn.Delete(ctx, &filestorev1.DeleteRequest{
		TenantSlug: tenantSlug, Path: path, Recursive: recursive,
	})
	return err
}

// Move renames/moves a file within the same tenant.
func (c *Client) Move(ctx context.Context, tenantSlug, from, to string) error {
	_, err := c.conn.Move(ctx, &filestorev1.MoveRequest{TenantSlug: tenantSlug, From: from, To: to})
	return err
}

// List returns directory entries up to maxDepth levels deep (0 = immediate children).
func (c *Client) List(ctx context.Context, tenantSlug, path string, maxDepth int) ([]Entry, error) {
	resp, err := c.conn.List(ctx, &filestorev1.ListRequest{
		TenantSlug: tenantSlug, Path: path, MaxDepth: int32(maxDepth),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(resp.Entries))
	for i, e := range resp.Entries {
		out[i] = Entry{
			Path: e.Path, Name: e.Name, IsDir: e.IsDir,
			Size: e.Size, ModTimeUnix: e.ModTimeUnix, HasChildren: e.HasChildren,
		}
	}
	return out, nil
}

// Stat returns metadata for a single path.
func (c *Client) Stat(ctx context.Context, tenantSlug, path string) (*Entry, error) {
	resp, err := c.conn.Stat(ctx, &filestorev1.StatRequest{TenantSlug: tenantSlug, Path: path})
	if err != nil {
		return nil, err
	}
	e := resp.Entry
	return &Entry{
		Path: e.Path, Name: e.Name, IsDir: e.IsDir,
		Size: e.Size, ModTimeUnix: e.ModTimeUnix, HasChildren: e.HasChildren,
	}, nil
}

// TotalSize returns the total bytes used by the tenant.
func (c *Client) TotalSize(ctx context.Context, tenantSlug string) (int64, error) {
	resp, err := c.conn.TotalSize(ctx, &filestorev1.TotalSizeRequest{TenantSlug: tenantSlug})
	if err != nil {
		return 0, err
	}
	return resp.Bytes, nil
}

// streamReader adapts a grpc streaming DataChunk response to io.ReadCloser.
type streamReader struct {
	stream filestorev1.FileStore_GetClient
	buf    []byte
}

func (r *streamReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		chunk, err := r.stream.Recv()
		if err != nil {
			return 0, err // io.EOF propagates as-is
		}
		r.buf = chunk.Data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *streamReader) Close() error { return nil }
