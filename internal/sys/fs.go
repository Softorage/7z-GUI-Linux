package sys

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// TruncateDisplayPath truncates a string with leading ellipsis if it exceeds maxLen
func TruncateDisplayPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	if maxLen <= 3 {
		return path
	}
	return "..." + path[len(path)-(maxLen-3):]
}

// GetDiskCacheDir returns the resolved disk cache directory for the application.
func GetDiskCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	return filepath.Join(cacheDir, domain.AppDirName)
}

// IsArchiveExtension returns true if the given path has a supported archive extension.
func IsArchiveExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".7z" || ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".bz2" || ext == ".xz" || ext == ".wim" || ext == ".rar"
}

// IsSingleFileArchive returns true if the archive format can only pack a single file directly.
// TODO: Same logic as isSingleStream in ui_compress. Consider DRYing it.
func IsSingleFileArchive(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".gz" || ext == ".bz2" || ext == ".xz"
}

// IsTarballExtension checks if a filename uses a double-compression TAR extension.
// 7-Zip treats tarballs (.tar.gz, .tgz, etc.) as two distinct archive layers.
func IsTarballExtension(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tar.xz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tbz") ||
		strings.HasSuffix(lower, ".txz")
}

// FormatSize formats byte values into human-readable strings (B, KB, MB, GB, TB).
func FormatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// HasDuplicateFilenames checks if there are files in the sources list that share the same base name
// but originate from different paths, which causes 7-Zip to fail with "Duplicate filename on disk".
func HasDuplicateFilenames(sources []string) bool {
	seen := make(map[string]string)
	for _, src := range sources {
		absPath, err := filepath.Abs(src)
		if err != nil {
			absPath = src
		}

		fi, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		if fi.IsDir() {
			continue // 7-Zip naturally preserves directory hierarchies, so conflicts won't occur at the root.
		}

		base := filepath.Base(absPath)
		if existing, found := seen[base]; found && existing != absPath {
			return true
		}
		seen[base] = absPath
	}
	return false
}

// GetLocalItems reads directory entries on disk, sorts folders before files, and evaluates symlinks.
func GetLocalItems(dirPath string, showHidden bool) ([]domain.FileSystemItem, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	var dirs, files []domain.FileSystemItem

	for _, entry := range entries {
		name := entry.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(dirPath, name)
		info, err := entry.Info()
		size := int64(0)
		modified := ""
		if err == nil {
			size = info.Size()
			modified = info.ModTime().Format("2006-01-02 15:04:05")
		}

		isSymlink := entry.Type()&os.ModeSymlink != 0
		isDir := entry.IsDir()
		if !isDir && isSymlink {
			// Resolve symlink target type
			if targetInfo, err := os.Stat(fullPath); err == nil {
				isDir = targetInfo.IsDir()
			}
		}

		item := domain.FileSystemItem{
			Name:      name,
			Path:      fullPath,
			IsDir:     isDir,
			IsSymlink: isSymlink,
			Size:      size,
			Modified:  modified,
		}

		if isDir {
			dirs = append(dirs, item)
		} else {
			files = append(files, item)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })

	return append(dirs, files...), nil
}

// GetVirtualItems maps raw flat archive entry paths into a virtual folder hierarchy corresponding to currentRelPath level.
func GetVirtualItems(all []domain.ArchiveItem, currentRelPath string) []domain.FileSystemItem {
	prefix := ""
	if currentRelPath != "" {
		prefix = currentRelPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}

	type tempItem struct {
		isDir    bool
		size     int64
		modified string
		path     string
	}

	merged := make(map[string]tempItem)

	for _, item := range all {
		path := item.Path
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			continue
		}

		rel := strings.TrimPrefix(path, prefix)
		if rel == "" {
			continue
		}

		isDir := item.IsDir || strings.HasSuffix(path, "/")
		trimmedRel := strings.TrimSuffix(rel, "/")
		if trimmedRel == "" {
			continue
		}

		parts := strings.Split(trimmedRel, "/")
		name := parts[0]
		if name == "" {
			continue
		}

		if len(parts) > 1 {
			// Sub-path element implies directory container at current level
			existing, exists := merged[name]
			if !exists || !existing.isDir {
				merged[name] = tempItem{isDir: true, size: 0, modified: item.Modified, path: prefix + name}
			}
		} else {
			if isDir {
				merged[name] = tempItem{isDir: true, size: 0, modified: item.Modified, path: prefix + name}
			} else {
				if _, exists := merged[name]; !exists {
					merged[name] = tempItem{isDir: false, size: item.Size, modified: item.Modified, path: item.Path}
				}
			}
		}
	}

	var dirs, files []domain.FileSystemItem
	for name, info := range merged {
		fItem := domain.FileSystemItem{
			Name:     name,
			Path:     info.path,
			IsDir:    info.isDir,
			Size:     info.size,
			Modified: info.modified,
		}
		if info.isDir {
			dirs = append(dirs, fItem)
		} else {
			files = append(files, fItem)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })

	return append(dirs, files...)
}

// CopyFile copies standard file bytes from src to dst.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// CopyDir recursively traverses source directory entries and creates destination folders.
func CopyDir(src, dst string, onFile func(s, d string) error) error {
	info, err := os.Lstat(src) // Read directory metadata safely
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if onFile != nil {
			if err := onFile(s, d); err != nil {
				return err
			}
		} else {
			if entry.IsDir() {
				if err := CopyDir(s, d, nil); err != nil {
					return err
				}
			} else {
				if err := CopyFile(s, d); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// GetUniqueDstPath generates auto-incremented non-conflicting local destination path (e.g., file_copy1.txt).
func GetUniqueDstPath(path string, usedNames map[string]bool) string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	counter := 1
	newPath := path
	for {
		_, err := os.Lstat(newPath)
		if os.IsNotExist(err) && !usedNames[filepath.Base(newPath)] {
			break
		}
		newPath = filepath.Join(dir, fmt.Sprintf("%s_copy%d%s", name, counter, ext))
		counter++
	}
	return newPath
}

// GetUniqueArchiveDstPath generates auto-incremented non-conflicting destination path inside archive.
func GetUniqueArchiveDstPath(baseName, relPath string, existingPaths map[string]bool) string {
	ext := filepath.Ext(baseName)
	name := strings.TrimSuffix(baseName, ext)

	counter := 1
	newName := baseName
	for {
		archivePath := filepath.Clean(filepath.Join(relPath, newName))
		if !existingPaths[archivePath] {
			break
		}
		newName = fmt.Sprintf("%s_copy%d%s", name, counter, ext)
		counter++
	}
	return newName
}
