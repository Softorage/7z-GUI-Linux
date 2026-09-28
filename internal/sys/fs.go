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

// ArchiveVolumeType identifies whether an archive is standalone, a starting multi-volume, or a continuation part.
type ArchiveVolumeType int

const (
	VolumeTypeNone ArchiveVolumeType = iota
	VolumeTypeSingle
	VolumeTypeSplitPrimary
	VolumeTypeSplitContinuation
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

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func formatPaddedInt1(width int) string {
	if width <= 1 {
		return "1"
	}
	buf := make([]byte, width)
	for i := 0; i < width-1; i++ {
		buf[i] = '0'
	}
	buf[width-1] = '1'
	return string(buf)
}

// findPartSubExtension extracts the base stem preceding '.part<number>' and the part index before a trailing '.rar' extension.
func findPartSubExtension(stem string) (base string, num int, width int, ok bool) {
	digitsEnd := len(stem)
	digitsStart := digitsEnd
	for digitsStart > 0 && isDigit(stem[digitsStart-1]) {
		digitsStart--
	}
	if digitsStart == digitsEnd || digitsStart < 5 {
		return "", 0, 0, false
	}

	// Must be preceded by ".part" (case-insensitive, 5 characters)
	if CompareFold(stem[digitsStart-5:digitsStart], ".part") != 0 {
		return "", 0, 0, false
	}

	val := 0
	for i := digitsStart; i < digitsEnd; i++ {
		val = val*10 + int(stem[i]-'0')
	}

	return stem[:digitsStart-5], val, digitsEnd - digitsStart, true
}

// ClassifyArchiveVolume categorizes an archive by volume type using allocation-free byte scanning.
func ClassifyArchiveVolume(path string) ArchiveVolumeType {
	ext := filepath.Ext(path)
	if ext == "" {
		return VolumeTypeNone
	}

	// Numeric split extension check (.001, .002, ...)
	if len(ext) >= 3 && ext[0] == '.' {
		allDigits := true
		for i := 1; i < len(ext); i++ {
			if !isDigit(ext[i]) {
				allDigits = false
				break
			}
		}
		if allDigits {
			val := 0
			for i := 1; i < len(ext); i++ {
				val = val*10 + int(ext[i]-'0')
			}
			if val == 1 {
				return VolumeTypeSplitPrimary
			}
			return VolumeTypeSplitContinuation
		}
	}

	// Legacy RAR volumes (.r00 - .r99, .s00 - .s99)
	if len(ext) == 4 && ext[0] == '.' {
		firstChar := ext[1]
		if (firstChar == 'r' || firstChar == 'R' || firstChar == 's' || firstChar == 'S') &&
			isDigit(ext[2]) && isDigit(ext[3]) {
			return VolumeTypeSplitContinuation
		}
	}

	// Split PKZIP volumes (.z01 - .z99)
	if len(ext) == 4 && ext[0] == '.' {
		firstChar := ext[1]
		if (firstChar == 'z' || firstChar == 'Z') && isDigit(ext[2]) && isDigit(ext[3]) {
			return VolumeTypeSplitContinuation
		}
	}

	// RAR with modern .partN.rar naming
	if HasSuffixFold(ext, ".rar") {
		stem := path[:len(path)-len(ext)]
		if _, num, _, ok := findPartSubExtension(stem); ok {
			if num == 1 {
				return VolumeTypeSplitPrimary
			}
			return VolumeTypeSplitContinuation
		}
		return VolumeTypeSingle
	}

	// Standard single-volume archive extensions
	if HasSuffixFold(ext, ".7z") ||
		HasSuffixFold(ext, ".zip") ||
		HasSuffixFold(ext, ".tar") ||
		HasSuffixFold(ext, ".gz") ||
		HasSuffixFold(ext, ".bz2") ||
		HasSuffixFold(ext, ".xz") ||
		HasSuffixFold(ext, ".wim") {
		return VolumeTypeSingle
	}

	return VolumeTypeNone
}

// IsArchiveExtension returns true if the given path has a supported archive extension or split volume suffix.
func IsArchiveExtension(path string) bool {
	return ClassifyArchiveVolume(path) != VolumeTypeNone
}

// IsMultiVolumeArchive returns true if path belongs to a split or multi-volume archive set.
func IsMultiVolumeArchive(path string) bool {
	vType := ClassifyArchiveVolume(path)
	return vType == VolumeTypeSplitPrimary || vType == VolumeTypeSplitContinuation
}

// IsSplitContinuationVolume returns true if path represents a secondary volume (.002+, .part2.rar+, .r00+, .z01+).
func IsSplitContinuationVolume(path string) bool {
	return ClassifyArchiveVolume(path) == VolumeTypeSplitContinuation
}

// ResolvePrimaryVolume returns the path of the starting/primary volume for multi-volume archives,
// TODO: checking filesystem existence with case tolerance for Linux file systems where applicable.
// Standalone archives and primary volumes return the original path unmodified.
func ResolvePrimaryVolume(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path
	}

	// Numeric split extensions (.002+ -> .001)
	if len(ext) >= 3 && ext[0] == '.' {
		allDigits := true
		for i := 1; i < len(ext); i++ {
			if !isDigit(ext[i]) {
				allDigits = false
				break
			}
		}
		if allDigits {
			base := path[:len(path)-len(ext)]
			width := len(ext) - 1
			return base + "." + formatPaddedInt1(width)
		}
	}

	//  Modern RAR volumes (.part02.rar -> .part01.rar)
	if HasSuffixFold(ext, ".rar") {
		stem := path[:len(path)-len(ext)]
		if base, _, width, ok := findPartSubExtension(stem); ok {
			return base + stem[len(base):len(base)+5] + formatPaddedInt1(width) + ext
		}
		return path
	}

	// Legacy RAR volumes (.r00 -> .rar)
	if len(ext) == 4 && ext[0] == '.' {
		firstChar := ext[1]
		if (firstChar == 'r' || firstChar == 'R' || firstChar == 's' || firstChar == 'S') &&
			isDigit(ext[2]) && isDigit(ext[3]) {
			base := path[:len(path)-len(ext)]
			if firstChar == 'R' || firstChar == 'S' {
				return base + ".RAR"
			}
			return base + ".rar"
		}
	}

	// Split PKZIP volumes (.z01 -> .zip)
	if len(ext) == 4 && ext[0] == '.' {
		firstChar := ext[1]
		if (firstChar == 'z' || firstChar == 'Z') && isDigit(ext[2]) && isDigit(ext[3]) {
			base := path[:len(path)-len(ext)]
			if firstChar == 'Z' {
				return base + ".ZIP"
			}
			return base + ".zip"
		}
	}

	return path
}

// IsSingleFileArchive returns true if the archive format can only pack a single file directly.
// TODO: Same logic as isSingleStream in ui_compress. Consider DRYing it.
func IsSingleFileArchive(path string) bool {
	ext := filepath.Ext(path)
	return HasSuffixFold(ext, ".gz") ||
		HasSuffixFold(ext, ".bz2") ||
		HasSuffixFold(ext, ".xz")
}

// IsTarballExtension checks if a filename uses a double-compression TAR extension.
// 7-Zip treats tarballs (.tar.gz, .tgz, etc.) as two distinct archive layers.
func IsTarballExtension(path string) bool {
	return HasSuffixFold(path, ".tar.gz") ||
		HasSuffixFold(path, ".tar.bz2") ||
		HasSuffixFold(path, ".tar.xz") ||
		HasSuffixFold(path, ".tgz") ||
		HasSuffixFold(path, ".tbz2") ||
		HasSuffixFold(path, ".tbz") ||
		HasSuffixFold(path, ".txz")
}

// GetArchiveBaseName computes a clean folder name by stripping container and multi-volume suffixes.
func GetArchiveBaseName(archivePath string) string {
	name := filepath.Base(archivePath)
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}

	// Compound tarball extensions (.tar.gz, .tar.bz2, etc.)
	for _, tarExt := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".tbz", ".txz"} {
		if HasSuffixFold(name, tarExt) {
			return name[:len(name)-len(tarExt)]
		}
	}

	// Numeric split extensions (.7z.001, .zip.001, .001)
	if ClassifyArchiveVolume(name) != VolumeTypeNone && len(ext) >= 3 && ext[0] == '.' {
		stem := name[:len(name)-len(ext)]
		innerExt := filepath.Ext(stem)
		if innerExt != "" && IsArchiveExtension(stem) {
			return stem[:len(stem)-len(innerExt)]
		}
		return stem
	}

	// RAR multi-volume parts (.part1.rar, .part01.rar)
	if HasSuffixFold(ext, ".rar") {
		stem := name[:len(name)-len(ext)]
		if base, _, _, ok := findPartSubExtension(stem); ok {
			return base
		}
		return stem
	}

	return strings.TrimSuffix(name, ext)
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

// packSortedFileSystemItems sorts directory and file slices in-place and packs them
// into a single, exact-capacity slice with directories preceding files.
func packSortedFileSystemItems(dirs, files []domain.FileSystemItem) []domain.FileSystemItem {
	slices.SortFunc(dirs, CompareFileSystemItems)
	slices.SortFunc(files, CompareFileSystemItems)

	total := len(dirs) + len(files)
	if total == 0 {
		return nil
	}

	result := make([]domain.FileSystemItem, total)
	copy(result, dirs)
	copy(result[len(dirs):], files)
	return result
}

// GetLocalItems reads directory entries on disk, sorts folders before files, and evaluates symlinks.
func GetLocalItems(dirPath string, showHidden bool) ([]domain.FileSystemItem, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	numEntries := len(entries)
	if numEntries == 0 {
		return nil, nil
	}

	// Bound initial capacities to prevent allocating oversized slices for directories
	// with many entries or large proportions of hidden files.
	dirsCap := min(numEntries/4+4, 64)
	filesCap := min(numEntries, 256)
	dirs := make([]domain.FileSystemItem, 0, dirsCap)
	files := make([]domain.FileSystemItem, 0, filesCap)

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

	return packSortedFileSystemItems(dirs, files), nil
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

	return packSortedFileSystemItems(dirs, files)
}

// CompareFold compares two ASCII/UTF-8 strings case-insensitively without heap allocation.
// It returns -1 if s1 < s2, 0 if s1 == s2, and 1 if s1 > s2.
func CompareFold(s1, s2 string) int {
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

// HasSuffixFold tests whether string s ends with suffix case-insensitively without heap allocation.
func HasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return CompareFold(s[len(s)-len(suffix):], suffix) == 0
}

// CompareFileSystemItems compares two FileSystemItem records case-insensitively, breaking ties by exact case.
func CompareFileSystemItems(a, b domain.FileSystemItem) int {
	if c := CompareFold(a.Name, b.Name); c != 0 {
		return c
	}
	return cmp.Compare(a.Name, b.Name)
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
