package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/filesys"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// StorageHandler provides HTTP endpoints for browsing and managing
// files inside the ~/.goclaw/ data directory.
// Skills directories are browsable (read-only) but deletion is blocked.
// sizeCacheEntry holds a cached storage size calculation for one tenant.
type sizeCacheEntry struct {
	total    int64
	files    int
	cachedAt time.Time
}

// OnFileCreatedFunc is called after a successful file upload.
// s3Key is the canonical storage key returned by the backend.
type OnFileCreatedFunc func(path, s3Key, mimeType string, size int64)

type StorageHandler struct {
	baseDir string // global data dir (resolved absolute path)
	fsFn    func(tenantRoot, tenantSlug string) filesys.Filesystem
	tenants store.TenantStore

	// onFileCreated is fired after a successful Put in handleUpload.
	onFileCreated OnFileCreatedFunc

	// sizeCache caches the total storage size per tenant for 60 minutes.
	sizeCache sync.Map // tenantBaseDir (string) → *sizeCacheEntry
}

// NewStorageHandler creates a handler for workspace storage management.
// baseDir is the global data directory; fsFn receives the resolved tenant root
// directory and tenant slug and returns a filesystem implementation for that tenant.
func NewStorageHandler(baseDir string, fsFn func(tenantRoot, tenantSlug string) filesys.Filesystem, tenants ...store.TenantStore) *StorageHandler {
	h := &StorageHandler{baseDir: baseDir, fsFn: fsFn}
	if len(tenants) > 0 {
		h.tenants = tenants[0]
	}
	return h
}

// WithOnFileCreated registers a callback fired after each successful upload.
func (h *StorageHandler) WithOnFileCreated(fn OnFileCreatedFunc) *StorageHandler {
	h.onFileCreated = fn
	return h
}

// RegisterRoutes registers storage management routes on the given mux.
func (h *StorageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/storage/files", h.auth(h.handleList))
	mux.HandleFunc("GET /v1/storage/files/{path...}", h.auth(h.handleRead))
	mux.HandleFunc("DELETE /v1/storage/files/{path...}", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleDelete)))
	mux.HandleFunc("GET /v1/storage/size", h.auth(h.handleSize))
	mux.HandleFunc("POST /v1/storage/files", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleUpload)))
	mux.HandleFunc("PUT /v1/storage/move", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleMove)))
}

func (h *StorageHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *StorageHandler) requireTenantAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pkgGatewayToken == "" && store.TenantIDFromContext(r.Context()) == store.MasterTenantID {
			next(w, r)
			return
		}
		if !requireTenantAdmin(w, r, h.tenants) {
			return
		}
		next(w, r)
	}
}

// pathWithinDir reports whether path is inside dir, allowing dir itself.
func pathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func evalSymlinkOrClean(path string) string {
	realPath, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(realPath)
	}
	return filepath.Clean(path)
}

// tenantBaseDir resolves the data directory scoped to the requesting tenant.
// Master tenant returns the global baseDir (backward compat).
func (h *StorageHandler) tenantBaseDir(r *http.Request) string {
	tid := store.TenantIDFromContext(r.Context())
	slug := store.TenantSlugFromContext(r.Context())
	return config.TenantDataDir(h.baseDir, tid, slug)
}

// protectedDirs are top-level directories where upload, move, and deletion are blocked.
// These are system-managed: skills (managed via Skills page), media (managed via media handler),
// tenants (tenant isolation root — each tenant's data is scoped internally).
var protectedDirs = []string{"skills", "skills-store", "media", "tenants"}

// topLevelPath returns the first path component of rel.
func topLevelPath(rel string) string {
	if before, _, ok := strings.Cut(rel, "/"); ok {
		return before
	}
	return rel
}

func isProtectedPath(rel string) bool {
	top := topLevelPath(rel)
	for _, d := range protectedDirs {
		if strings.EqualFold(top, d) {
			return true
		}
	}
	return false
}

// isHiddenPath reports paths that should not be surfaced in the Storage UI/API.
// Master tenant keeps its legacy base dir for backward compatibility, but must
// not expose the cross-tenant isolation root.
func (h *StorageHandler) isHiddenPath(r *http.Request, rel string) bool {
	if rel == "" {
		return false
	}
	if store.TenantIDFromContext(r.Context()) != store.MasterTenantID {
		return false
	}
	return strings.EqualFold(topLevelPath(rel), "tenants")
}

// fsForRequest returns the filesystem for the requesting tenant.
func (h *StorageHandler) fsForRequest(r *http.Request) filesys.Filesystem {
	return h.fsFn(h.tenantBaseDir(r), store.TenantSlugFromContext(r.Context()))
}

// handleList lists files and directories under ~/.goclaw/ with depth limiting.
// Query params:
//   - ?path=  scopes the listing to a subtree
//   - ?depth= max depth to walk (default 3, max 20)
func (h *StorageHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	subPath := r.URL.Query().Get("path")
	if strings.Contains(subPath, "..") {
		slog.Warn("security.storage_traversal", "path", subPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	maxDepth := 3
	if d := r.URL.Query().Get("depth"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v >= 1 && v <= 20 {
			maxDepth = v
		}
	}

	if subPath != "" && h.isHiddenPath(r, subPath) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "path", subPath)})
		return
	}

	fs := h.fsForRequest(r)
	entries, err := fs.List(r.Context(), subPath, maxDepth)
	if err != nil {
		if err == filesys.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "path", subPath)})
			return
		}
		if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
		slog.Error("storage.list_failed", "path", subPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to list files")})
		return
	}

	type fileEntry struct {
		Path        string `json:"path"`
		Name        string `json:"name"`
		IsDir       bool   `json:"isDir"`
		Size        int64  `json:"size"`
		HasChildren bool   `json:"hasChildren,omitempty"`
		Protected   bool   `json:"protected"`
	}

	var out []fileEntry
	for _, e := range entries {
		// Hide tenant isolation root from master storage listing.
		if h.isHiddenPath(r, e.Path) {
			continue
		}
		// Skip system artifacts.
		if skills.IsSystemArtifact(e.Path) {
			continue
		}
		out = append(out, fileEntry{
			Path:        e.Path,
			Name:        e.Name,
			IsDir:       e.IsDir,
			Size:        e.Size,
			HasChildren: e.HasChildren,
			Protected:   isProtectedPath(e.Path),
		})
	}

	if out == nil {
		out = []fileEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files":   out,
		"baseDir": h.tenantBaseDir(r),
	})
}

// sizeCacheTTL is how long storage size calculations are cached.
const sizeCacheTTL = 60 * time.Minute

// handleSize streams the total storage size via SSE.
// Cached for 60 minutes; returns cached result immediately if valid.
func (h *StorageHandler) handleSize(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgStreamingNotSupported)})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sizeBase := h.tenantBaseDir(r)

	// Check per-tenant cache
	if entry, ok := h.sizeCache.Load(sizeBase); ok {
		ce := entry.(*sizeCacheEntry)
		if time.Since(ce.cachedAt) < sizeCacheTTL {
			writeSizeEvent(w, flusher, map[string]any{"total": ce.total, "files": ce.files, "done": true, "cached": true})
			return
		}
	}

	// Walk and stream progress
	var total int64
	var fileCount int
	lastFlush := time.Now()

	fs := h.fsForRequest(r)
	err := fs.Walk(r.Context(), "", func(path string, entry filesys.Entry) error {
		if entry.IsDir {
			return nil
		}
		if r.Context().Err() != nil {
			return r.Context().Err()
		}
		total += entry.Size
		fileCount++
		if fileCount%50 == 0 || time.Since(lastFlush) > 200*time.Millisecond {
			writeSizeEvent(w, flusher, map[string]any{"current": total, "files": fileCount})
			lastFlush = time.Now()
		}
		return nil
	})
	if err != nil {
		slog.Error("storage.size_failed", "error", err)
		writeSizeEvent(w, flusher, map[string]any{"error": "failed to calculate size", "done": true})
		return
	}

	// Update per-tenant cache
	h.sizeCache.Store(sizeBase, &sizeCacheEntry{total: total, files: fileCount, cachedAt: time.Now()})

	// Send final event
	writeSizeEvent(w, flusher, map[string]any{"total": total, "files": fileCount, "done": true, "cached": false})
}

func writeSizeEvent(w http.ResponseWriter, flusher http.Flusher, data map[string]any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// handleRead reads a single file's content by relative path.
func (h *StorageHandler) handleRead(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}
	if h.isHiddenPath(r, relPath) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	fs := h.fsForRequest(r)
	stat, err := fs.Stat(r.Context(), relPath)
	if err != nil {
		if err == filesys.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		} else if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		} else {
			slog.Error("storage.stat_failed", "path", relPath, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to read file")})
		}
		return
	}
	if stat.IsDir {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		return
	}

	rc, size, ct, err := fs.Get(r.Context(), relPath)
	if err != nil {
		if err == filesys.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgFileNotFound)})
		} else if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		} else {
			slog.Error("storage.read_failed", "path", relPath, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		}
		return
	}
	defer rc.Close()

	// Raw mode: serve the file with its native content type (for images, downloads, etc.)
	if r.URL.Query().Get("raw") == "true" {
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=300")
		if r.URL.Query().Get("download") == "true" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(relPath)))
		}
		if size >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		}
		_, _ = io.Copy(w, rc)
		return
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		slog.Error("storage.read_failed", "path", relPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToReadFile)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content": string(data),
		"path":    relPath,
		"size":    size,
	})
}

// handleDelete removes a file or directory (recursively).
// Rejects deletion of the root dir and any path inside excluded directories.
func (h *StorageHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	relPath := r.PathValue("path")
	if relPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}
	if strings.Contains(relPath, "..") {
		slog.Warn("security.storage_traversal", "path", relPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	if isProtectedPath(relPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	fs := h.fsForRequest(r)
	if err := fs.Delete(r.Context(), relPath); err != nil {
		if err == filesys.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "path", relPath)})
			return
		}
		if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
		slog.Error("storage.delete_failed", "path", relPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToDeleteFile)})
		return
	}

	// Invalidate cached size for this tenant after successful deletion.
	h.sizeCache.Delete(h.tenantBaseDir(r))

	slog.Info("storage.deleted", "path", relPath)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleUpload uploads a file into the storage data directory.
// Admin-only. Rejects uploads into protected directories (skills, skills-store).
func (h *StorageHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	subPath := r.URL.Query().Get("path")
	if strings.Contains(subPath, "..") {
		slog.Warn("security.storage_upload_traversal", "path", subPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Reject upload into protected directories.
	if subPath != "" && isProtectedPath(subPath) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	// Enforce file size limit.
	r.Body = http.MaxBytesReader(w, r.Body, tools.MaxFileSizeBytes)
	if err := r.ParseMultipartForm(tools.MaxFileSizeBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	// Sanitize filename.
	origName := filepath.Base(header.Filename)
	if origName == "." || origName == "/" || strings.Contains(origName, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}

	// Check blocked extensions.
	ext := strings.ToLower(filepath.Ext(origName))
	if tools.IsBlockedExtension(ext) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file type %s is not allowed", ext)})
		return
	}

	relPath := origName
	if subPath != "" {
		relPath = filepath.Join(subPath, origName)
	}
	relPath = filepath.ToSlash(relPath)

	// Determine content type.
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	if ct == "" {
		ct = "application/octet-stream"
	}

	fs := h.fsForRequest(r)
	key, err := fs.Put(r.Context(), relPath, file, header.Size, ct)
	if err != nil {
		if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
		slog.Error("storage.upload_failed", "path", relPath, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save file")})
		return
	}

	// Invalidate size cache for this tenant.
	h.sizeCache.Delete(h.tenantBaseDir(r))

	size := header.Size
	if size == 0 {
		// Header.Size is not always populated; try to stat the written file.
		if st, err := fs.Stat(r.Context(), relPath); err == nil {
			size = st.Size
		}
	}

	if h.onFileCreated != nil {
		go h.onFileCreated(relPath, key, ct, size)
	}

	slog.Info("storage.uploaded", "path", relPath, "size", size)
	writeJSON(w, http.StatusOK, map[string]any{
		"path":     relPath,
		"filename": origName,
		"size":     size,
	})
}

// handleMove moves/renames a file within the storage data directory.
// Admin-only. Rejects moves involving protected directories.
// Query params: ?from=relPath&to=relPath
func (h *StorageHandler) handleMove(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	fromRel := r.URL.Query().Get("from")
	toRel := r.URL.Query().Get("to")
	if fromRel == "" || toRel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "from, to")})
		return
	}

	// Reject path traversal in both paths.
	if strings.Contains(fromRel, "..") || strings.Contains(toRel, "..") {
		slog.Warn("security.storage_move_traversal", "from", fromRel, "to", toRel)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
		return
	}

	// Reject moves involving protected directories.
	if isProtectedPath(fromRel) || isProtectedPath(toRel) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgCannotDeleteSkillsDir)})
		return
	}

	fs := h.fsForRequest(r)
	if err := fs.Move(r.Context(), fromRel, toRel); err != nil {
		if err == filesys.ErrInvalidPath {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidPath)})
			return
		}
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "destination already exists") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a file with that name already exists at the destination"})
			return
		}
		slog.Error("storage.move_failed", "from", fromRel, "to", toRel, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to move file")})
		return
	}

	// Invalidate cached size for this tenant after successful move.
	h.sizeCache.Delete(h.tenantBaseDir(r))

	slog.Info("storage.moved", "from", fromRel, "to", toRel)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": fromRel,
		"to":   toRel,
	})
}
