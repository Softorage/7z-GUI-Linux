package sys

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
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

	numEntries := len(entries)
	// Pre-allocating dirs with capacity = numEntries guarantees that
	// `append(dirs, files...)` at the end never triggers a heap reallocation.
	dirs := make([]domain.FileSystemItem, 0, numEntries)
	files := make([]domain.FileSystemItem, 0, numEntries)

	// Pre-normalize base directory prefix once to avoid calling filepath.Join / filepath.Clean
	// on every entry inside the loop.
	baseDir := filepath.Clean(dirPath)
	if baseDir == "." {
		baseDir = ""
	} else if !os.IsPathSeparator(baseDir[len(baseDir)-1]) {
		baseDir += string(filepath.Separator)
	}

	for _, entry := range entries {
		name := entry.Name()
		// Fast direct byte check instead of calling strings.HasPrefix
		if !showHidden && len(name) > 0 && name[0] == '.' {
			continue
		}

		fullPath := baseDir + name

		size := int64(0)
		modified := ""
		if info, err := entry.Info(); err == nil {
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

	// Zero-allocation, reflection-free case-insensitive sort with tie-breaker
	caseInsensitiveSort := func(a, b domain.FileSystemItem) int {
		if c := compareFold(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	}

	slices.SortFunc(dirs, caseInsensitiveSort)
	slices.SortFunc(files, caseInsensitiveSort)

	return append(dirs, files...), nil
}

// GetVirtualItems maps raw flat archive entry paths into a virtual folder hierarchy corresponding to currentRelPath level
// using an allocation-free single scan on sorted archive items without intermediate map allocations.
// PRECONDITION: `all` MUST be pre-sorted lexicographically by Path.
func GetVirtualItems(all []domain.ArchiveItem, currentRelPath string) []domain.FileSystemItem {
	prefix := ""
	if currentRelPath != "" {
		prefix = path.Clean(currentRelPath)
		if prefix == "." || prefix == "/" {
			prefix = ""
		} else if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}

	dirs := make([]domain.FileSystemItem, 0, 16)
	files := make([]domain.FileSystemItem, 0, 32)

	lastDir := ""
	lastFile := ""

	for i := range all {
		itemPath := all[i].Path
		if prefix != "" && !strings.HasPrefix(itemPath, prefix) {
			continue
		}

		rel := strings.TrimPrefix(itemPath, prefix)
		if rel == "" {
			continue
		}

		if before, _, ok := strings.Cut(rel, "/"); ok {
			// Sub-directory element
			if before == "" || before == lastDir {
				continue
			}
			lastDir = before
			dirs = append(dirs, domain.FileSystemItem{
				Name:     before,
				Path:     prefix + before,
				IsDir:    true,
				Size:     0,
				Modified: all[i].Modified,
			})
		} else if all[i].IsDir {
			// Direct directory entry
			if rel == lastDir {
				continue
			}
			lastDir = rel
			dirs = append(dirs, domain.FileSystemItem{
				Name:     rel,
				Path:     prefix + rel,
				IsDir:    true,
				Size:     0,
				Modified: all[i].Modified,
			})
		} else {
			// Direct file entry
			if rel == lastFile {
				continue
			}
			lastFile = rel
			files = append(files, domain.FileSystemItem{
				Name:     rel,
				Path:     all[i].Path,
				IsDir:    false,
				Size:     all[i].Size,
				Modified: all[i].Modified,
			})
		}
	}

	// Zero-allocation, reflection-free case-insensitive sort with tie-breaker
	caseInsensitiveSort := func(a, b domain.FileSystemItem) int {
		if c := compareFold(a.Name, b.Name); c != 0 {
			return c
		}
		// Exact case tie-breaker (stable order for Linux case-sensitive entries)
		return cmp.Compare(a.Name, b.Name)
	}

	slices.SortFunc(dirs, caseInsensitiveSort)
	slices.SortFunc(files, caseInsensitiveSort)

	return append(dirs, files...)
}

// compareFold compares two ASCII/UTF-8 strings case-insensitively without heap allocation.
func compareFold(s1, s2 string) int {
	for len(s1) > 0 && len(s2) > 0 {
		c1, c2 := s1[0], s2[0]
		// Fast-path ASCII lowercasing inline
		if 'A' <= c1 && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if 'A' <= c2 && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return cmp.Compare(c1, c2)
		}
		s1 = s1[1:]
		s2 = s2[1:]
	}
	return cmp.Compare(len(s1), len(s2))
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
