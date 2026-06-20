package filesys

import "github.com/nextlevelbuilder/goclaw/internal/filestoreclient"

// FileStorageConfig selects the storage backend.
type FileStorageConfig struct {
	Backend string // "local" or "filestore"
}

// New creates a Filesystem for the given tenant.
// tenantRoot is the resolved local tenant data directory (used by local backend).
// tenantSlug is the tenant slug used by the filestore backend for key prefixes.
// When cfg.Backend == "filestore", client must be non-nil.
func New(cfg FileStorageConfig, client *filestoreclient.Client, tenantRoot, tenantSlug string) Filesystem {
	if cfg.Backend == "filestore" {
		if client == nil {
			// Fallback to local disk if no client is available.
			return NewLocalFilesystem(tenantRoot)
		}
		return NewFilestoreFilesystem(client, tenantSlug)
	}
	return NewLocalFilesystem(tenantRoot)
}
