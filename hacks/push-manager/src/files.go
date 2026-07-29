package main

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// FileOps handles filesystem operations within allowed roots.
// All paths are validated against allowedRoots before any operation.
type FileOps struct {
	allowedRoots []string
	showHidden   bool
}

func NewFileOps(roots []string, showHidden bool) *FileOps {
	clean := make([]string, len(roots))
	for i, r := range roots {
		clean[i] = filepath.Clean(r)
	}
	return &FileOps{allowedRoots: clean, showHidden: showHidden}
}

// Entry represents a single filesystem entry in a directory listing
type Entry struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	Extension    string    `json:"extension,omitempty"`
}

// SafePath resolves and validates a path against allowed roots.
// Returns cleaned absolute path if allowed, error otherwise.
func (f *FileOps) SafePath(raw string) (string, error) {
	// Clean and make absolute
	clean := filepath.Clean(raw)

	// Must fall under at least one allowed root
	for _, root := range f.allowedRoots {
		// filepath.Rel returns a path without ".." if clean is under root
		rel, err := filepath.Rel(root, clean)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") {
			return clean, nil
		}
	}

	return "", fmt.Errorf("path not allowed: %s (must be under an allowed root)", raw)
}

// List returns directory entries for the given path.
func (f *FileOps) List(dir string) ([]Entry, error) {
	safe, err := f.SafePath(dir)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(safe)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	dirEntries, err := os.ReadDir(safe)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()

		// Skip hidden files unless enabled
		if !f.showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Under /run/media, skip swap partition mounts (e.g. "swap1-nvme0n1p2")
		if safe == "/run/media" && strings.HasPrefix(name, "swap") {
			continue
		}

		info, err := de.Info()
		if err != nil {
			continue // skip unreadable entries
		}

		fullPath := filepath.Join(safe, name)
		ext := ""
		if !de.IsDir() {
			ext = strings.ToLower(filepath.Ext(name))
		}

		entries = append(entries, Entry{
			Name:      name,
			Path:      fullPath,
			IsDir:     de.IsDir(),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Extension: ext,
		})
	}

	// Sort: directories first, then files, both alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries, nil
}

// Upload saves an uploaded file into the target directory.
// Returns the final filename.
func (f *FileOps) Upload(dir string, fh *multipart.FileHeader) (string, error) {
	return f.UploadWithRelPath(dir, fh, "")
}

// UploadWithRelPath saves an uploaded file, optionally preserving a relative
// subdirectory path (e.g. "MyProject/Samples/kick.wav" from a folder upload).
// relPath must be a clean relative path with no ".." components.
func (f *FileOps) UploadWithRelPath(dir string, fh *multipart.FileHeader, relPath string) (string, error) {
	safeDir, err := f.SafePath(dir)
	if err != nil {
		return "", err
	}

	var destPath string
	if relPath != "" && relPath != "." {
		// Validate: no absolute path, no traversal
		rel := filepath.Clean(relPath)
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("invalid relative path: %s", relPath)
		}
		destPath = filepath.Join(safeDir, rel)
	} else {
		// Plain upload: sanitise filename only
		filename := filepath.Base(fh.Filename)
		if filename == "." || filename == "/" || filename == "" {
			return "", fmt.Errorf("invalid filename")
		}
		destPath = filepath.Join(safeDir, filename)
	}

	// Double-check destination is still under allowed roots
	if _, err := f.SafePath(destPath); err != nil {
		return "", fmt.Errorf("destination not allowed: %w", err)
	}

	// Create intermediate directories (needed for folder uploads)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	src, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("open upload: %w", err)
	}
	defer src.Close()

	// Atomic write: temp file → rename
	tmpPath := destPath + ".tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create dest: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write file: %w", err)
	}
	dst.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("finalise upload: %w", err)
	}

	// Chown to match destination directory owner (push-manager runs as root)
	if uid, gid := ownerOf(filepath.Dir(destPath)); uid >= 0 {
		if err := os.Chown(destPath, uid, gid); err != nil {
			log.Printf("chown %s: %v", destPath, err)
		}
	}

	return filepath.Base(destPath), nil
}

// Delete removes a file or an empty directory.
func (f *FileOps) Delete(path string) error {
	safe, err := f.SafePath(path)
	if err != nil {
		return err
	}

	// Refuse to delete an allowed root itself
	for _, root := range f.allowedRoots {
		if filepath.Clean(safe) == filepath.Clean(root) {
			return fmt.Errorf("cannot delete an allowed root directory")
		}
	}

	info, err := os.Stat(safe)
	if err != nil {
		return err
	}

	if info.IsDir() {
		// Only remove empty directories for safety
		entries, err := os.ReadDir(safe)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("directory not empty: %s", path)
		}
		return os.Remove(safe)
	}

	return os.Remove(safe)
}

// DeleteAll removes a file or entire directory tree.
func (f *FileOps) DeleteAll(path string) error {
	safe, err := f.SafePath(path)
	if err != nil {
		return err
	}
	for _, root := range f.allowedRoots {
		if filepath.Clean(safe) == filepath.Clean(root) {
			return fmt.Errorf("cannot delete an allowed root directory")
		}
	}
	return os.RemoveAll(safe)
}

// Copy copies src to dst (both must be within allowed roots).
// dst must not already exist.
func (f *FileOps) Copy(srcPath, dstPath string) error {
	safeSrc, err := f.SafePath(srcPath)
	if err != nil {
		return err
	}
	safeDst, err := f.SafePath(dstPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(safeDst); err == nil {
		return fmt.Errorf("destination already exists: %s", filepath.Base(dstPath))
	}
	info, err := os.Stat(safeSrc)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(safeSrc, safeDst)
	}
	return copyFile(safeSrc, safeDst)
}

// ownerOf returns the uid/gid of path's owner, or -1/-1 on error.
func ownerOf(path string) (uid, gid int) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return -1, -1
	}
	return int(st.Uid), int(st.Gid)
}

func copyFile(src, dst string) error {
	parentDir := filepath.Dir(dst)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	// Chown to match destination parent directory owner
	if uid, gid := ownerOf(parentDir); uid >= 0 {
		if err := os.Chown(dst, uid, gid); err != nil {
			log.Printf("chown %s: %v", dst, err)
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			if err := os.MkdirAll(target, fi.Mode()); err != nil {
				return err
			}
			// Chown dir to match its parent's owner
			if uid, gid := ownerOf(filepath.Dir(target)); uid >= 0 {
				if err := os.Chown(target, uid, gid); err != nil {
					log.Printf("chown %s: %v", target, err)
				}
			}
			return nil
		}
		return copyFile(p, target)
	})
}

// Rename moves oldPath to newPath (both must be within allowed roots).
func (f *FileOps) Rename(oldPath, newPath string) error {
	safeOld, err := f.SafePath(oldPath)
	if err != nil {
		return err
	}
	safeNew, err := f.SafePath(newPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(safeNew); err == nil {
		return fmt.Errorf("name already in use: %s", filepath.Base(newPath))
	}
	return os.Rename(safeOld, safeNew)
}
