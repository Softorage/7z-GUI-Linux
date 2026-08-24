package sys

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// Helper for Memory & System Storage

// GetAvailableRAMBytes reads Linux `/proc/meminfo` to calculate available system RAM.
func GetAvailableRAMBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 2 * 1024 * 1024 * 1024 // 2GB fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if val, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return val * 1024 // KiB to bytes
				}
			}
		}
	}
	return 2 * 1024 * 1024 * 1024
}

// IsTmpfs executes statfs syscall to verify if target path resides in RAM (tmpfs magic number 0x01021994).
func IsTmpfs(path string) bool {
	var stat syscall.Statfs_t
	return syscall.Statfs(path, &stat) == nil && uint64(stat.Type) == 0x01021994
}

// SelectTempStorage decides whether to stage uncompressed files in RAM (tmpfs) or disk storage.
// Dynamically scales RAM budget up to 49% of available memory (capped at 8GB for high-spec workstations).
func SelectTempStorage(requiredBytes uint64) (string, bool) {
	ramBudget := GetAvailableRAMBytes() * 49 / 100
	maxRAMBudget := uint64(8 * 1024 * 1024 * 1024)
	if ramBudget > maxRAMBudget {
		ramBudget = maxRAMBudget
	}

	// RAM (tmpfs) if requiredBytes fits within budget
	if requiredBytes <= ramBudget {
		if IsTmpfs("/tmp") {
			if dir, err := os.MkdirTemp("/tmp", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}
		if IsTmpfs("/dev/shm") {
			if dir, err := os.MkdirTemp("/dev/shm", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}
	}

	// Fallback to disk cache directory
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	nestedCache := filepath.Join(cacheDir, domain.AppDirName, "nested")
	_ = os.MkdirAll(nestedCache, 0755)

	dir, err := os.MkdirTemp(nestedCache, "7gl-disk-*")
	if err != nil {
		dir, _ = os.MkdirTemp("", "7gl-disk-*")
	}
	return dir, false
}