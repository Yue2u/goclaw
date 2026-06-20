package filesys

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// LocalFilesystem implements Filesystem on top of the local disk.
type LocalFilesystem struct {
	root          string // absolute, resolved tenant root directory
	hiddenChecker func(realPath string) bool
}

// NewLocalFilesystem creates a local disk filesystem rooted at root.
// The root directory must already exist or be creatable by callers.
func NewLocalFilesystem(root string) *LocalFilesystem {
	return &LocalFilesystem{root: filepath.Clean(root)}
}

// SetHiddenPathChecker sets an optional callback that reports whether a resolved
// absolute path should be treated as hidden (returns true for hidden).
func (fs *LocalFilesystem) SetHiddenPathChecker(fn func(realPath string) bool) *LocalFilesystem {
	fs.hiddenChecker = fn
	return fs
}

func (fs *LocalFilesystem) resolve(rel string) (string, error) {
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	abs := filepath.Join(fs.root, rel)
	if !strings.HasPrefix(abs, fs.root+string(filepath.Separator)) && abs != fs.root {
		return "", ErrInvalidPath
	}
	return abs, nil
}

func (fs *LocalFilesystem) realRoot() string {
	realRoot, err := filepath.EvalSymlinks(fs.root)
	if err != nil {
		realRoot = fs.root
	}
	return filepath.Clean(realRoot)
}

func (fs *LocalFilesystem) realPath(abs string) (string, bool) {
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	return filepath.Clean(realPath), true
}

func (fs *LocalFilesystem) withinRoot(abs string) bool {
	realRoot := fs.realRoot()
	realPath, ok := fs.realPath(abs)
	if !ok {
		return false
	}
	if realPath == realRoot {
		return true
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (fs *LocalFilesystem) isHiddenPath(abs string) bool {
	if fs.hiddenChecker == nil {
		return false
	}
	realPath, ok := fs.realPath(abs)
	if !ok {
		return false
	}
	return fs.hiddenChecker(realPath)
}

// List returns entries under path up to maxDepth levels deep.
func (fs *LocalFilesystem) List(ctx context.Context, path string, maxDepth int) ([]Entry, error) {
	root, err := fs.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrNotFound
	}
	if fs.isHiddenPath(root) {
		return nil, ErrNotFound
	}

	var entries []Entry
	err = filepath.WalkDir(root, func(abs string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if abs == root {
			return nil
		}
		rel, _ := filepath.Rel(fs.root, abs)
		rel = filepath.ToSlash(rel)

		// Skip symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		if fs.isHiddenPath(abs) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip system artifacts.
		if skills.IsSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relToRoot, _ := filepath.Rel(root, abs)
		depth := strings.Count(relToRoot, string(filepath.Separator)) + 1

		e := Entry{
			Path:  rel,
			Name:  d.Name(),
			IsDir: d.IsDir(),
		}

		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				e.Size = info.Size()
				e.ModTime = info.ModTime()
			}
		} else {
			if info, err := d.Info(); err == nil {
				e.ModTime = info.ModTime()
			}
		}

		if d.IsDir() && depth > maxDepth {
			if dirEntries, err := os.ReadDir(abs); err == nil && len(dirEntries) > 0 {
				e.HasChildren = true
			}
			entries = append(entries, e)
			return filepath.SkipDir
		}

		if d.IsDir() && depth == maxDepth {
			if dirEntries, err := os.ReadDir(abs); err == nil && len(dirEntries) > 0 {
				e.HasChildren = true
			}
		}

		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []Entry{}
	}
	return entries, nil
}

// Stat returns metadata for path.
func (fs *LocalFilesystem) Stat(ctx context.Context, path string) (*Entry, error) {
	abs, err := fs.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrNotFound
	}
	if !fs.withinRoot(abs) || fs.isHiddenPath(abs) {
		return nil, ErrNotFound
	}
	return &Entry{
		Path:    path,
		Name:    info.Name(),
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// Get opens the file at path.
func (fs *LocalFilesystem) Get(ctx context.Context, path string) (io.ReadCloser, int64, string, error) {
	abs, err := fs.resolve(path)
	if err != nil {
		return nil, 0, "", ErrInvalidPath
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, "", ErrNotFound
		}
		return nil, 0, "", err
	}
	if info.IsDir() {
		return nil, 0, "", ErrNotFound
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, "", ErrNotFound
	}
	if !fs.withinRoot(abs) || fs.isHiddenPath(abs) {
		return nil, 0, "", ErrNotFound
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, "", err
	}
	ct := mime.TypeByExtension(filepath.Ext(abs))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return f, info.Size(), ct, nil
}

// Put writes r to path atomically. It creates parent directories as needed.
func (fs *LocalFilesystem) Put(ctx context.Context, path string, r io.Reader, size int64, contentType string) (string, error) {
	abs, err := fs.resolve(path)
	if err != nil {
		return "", ErrInvalidPath
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	if !fs.withinRoot(dir) || fs.isHiddenPath(dir) {
		return "", ErrInvalidPath
	}

	out, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", err
	}
	tmpPath := out.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if !fs.withinRoot(dir) || fs.isHiddenPath(dir) {
		return "", ErrInvalidPath
	}
	if err := os.Rename(tmpPath, abs); err != nil {
		return "", err
	}
	cleanup = false
	return path, nil
}

// Delete removes the file or directory at path.
func (fs *LocalFilesystem) Delete(ctx context.Context, path string) error {
	abs, err := fs.resolve(path)
	if err != nil {
		return ErrInvalidPath
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	if !fs.withinRoot(abs) || fs.isHiddenPath(abs) {
		return ErrNotFound
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(abs)
	}
	if info.IsDir() {
		return os.RemoveAll(abs)
	}
	return os.Remove(abs)
}

// Move renames from to to within the same filesystem.
func (fs *LocalFilesystem) Move(ctx context.Context, from, to string) error {
	absFrom, err := fs.resolve(from)
	if err != nil {
		return ErrInvalidPath
	}
	absTo, err := fs.resolve(to)
	if err != nil {
		return ErrInvalidPath
	}
	if !fs.withinRoot(absFrom) || fs.isHiddenPath(absFrom) {
		return ErrInvalidPath
	}
	toDir := filepath.Dir(absTo)
	if err := os.MkdirAll(toDir, 0750); err != nil {
		return err
	}
	if !fs.withinRoot(toDir) || fs.isHiddenPath(toDir) {
		return ErrInvalidPath
	}
	if _, err := os.Stat(absTo); err == nil {
		return fmt.Errorf("destination already exists")
	}
	return os.Rename(absFrom, absTo)
}

// Walk visits every entry under root.
func (fs *LocalFilesystem) Walk(ctx context.Context, root string, fn WalkFn) error {
	rootAbs, err := fs.resolve(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootAbs, func(abs string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if abs == rootAbs {
			return nil
		}
		rel, _ := filepath.Rel(fs.root, abs)
		rel = filepath.ToSlash(rel)

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if skills.IsSystemArtifact(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fs.isHiddenPath(abs) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, _ := d.Info()
		var modTime time.Time
		var size int64
		if info != nil {
			modTime = info.ModTime()
			size = info.Size()
		}

		return fn(rel, Entry{
			Path:    rel,
			Name:    d.Name(),
			IsDir:   d.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
	})
}

// TotalSize returns the total bytes used by files under the tenant root.
func (fs *LocalFilesystem) TotalSize(ctx context.Context) (int64, error) {
	var total int64
	err := filepath.WalkDir(fs.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(fs.root, path)
		rel = filepath.ToSlash(rel)
		if skills.IsSystemArtifact(rel) {
			return nil
		}
		if fs.isHiddenPath(path) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
