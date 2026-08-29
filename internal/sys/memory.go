package sys

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// Helper for Memory & System Storage

// parseMeminfoValue parses an unsigned integer in KiB from lines starting with the prefix without string allocations.
func parseMeminfoValue(data []byte, prefix []byte) (uint64, bool) {
	idx := bytes.Index(data, prefix)
	if idx == -1 {
		return 0, false
	}
	rest := data[idx+len(prefix):]
	var val uint64
	var foundDigits bool
	for _, b := range rest {
		if b >= '0' && b <= '9' {
			val = val*10 + uint64(b-'0')
			foundDigits = true
		} else if foundDigits {
			break
		}
	}
	if foundDigits {
		return val * 1024, true // KiB to bytes
	}
	return 0, false
}

// GetTotalRAMBytes reads Linux `/proc/meminfo` to calculate total system RAM.
func GetTotalRAMBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	val, _ := parseMeminfoValue(data, []byte("MemTotal:"))
	return val
}

// GetAvailableRAMBytes reads Linux `/proc/meminfo` to calculate available system RAM.
func GetAvailableRAMBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 2 * 1024 * 1024 * 1024 // 2GB fallback
	}
	if val, ok := parseMeminfoValue(data, []byte("MemAvailable:")); ok {
		return val
	}
	return 2 * 1024 * 1024 * 1024
}

// GetPathAvailableBytes returns available free space in bytes on the filesystem mount containing path.
func GetPathAvailableBytes(path string) uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}

// IsTmpfs executes statfs syscall to verify if target path resides in RAM (tmpfs magic number 0x01021994).
func IsTmpfs(path string) bool {
	var stat syscall.Statfs_t
	return syscall.Statfs(path, &stat) == nil && uint64(stat.Type) == 0x01021994
}

// SelectTempStorage decides whether to stage uncompressed files in RAM (tmpfs) or disk storage.
// Dynamically scales RAM budget according to configured percentage of available memory and hard limit cap.
func SelectTempStorage(requiredBytes uint64, ramPercent int, ramLimitMB int64) (string, bool) {
	if ramPercent <= 0 {
		ramPercent = domain.DefaultRAMPercent
	}
	if ramPercent < domain.MinRAMUsagePercent {
		ramPercent = domain.MinRAMUsagePercent
	} else if ramPercent > domain.MaxRAMUsagePercent {
		ramPercent = domain.MaxRAMUsagePercent
	}

	if ramLimitMB < domain.MinRAMLimitMB {
		ramLimitMB = domain.DefaultRAMLimitMB
	}

	availableRAM := GetAvailableRAMBytes()
	ramBudget := (availableRAM * uint64(ramPercent)) / 100
	maxRAMBudget := uint64(ramLimitMB) * 1024 * 1024
	if ramBudget > maxRAMBudget {
		ramBudget = maxRAMBudget
	}

	// Stage in RAM (tmpfs) only if requiredBytes fits within calculated budget and mount capacity
	if requiredBytes <= ramBudget {
		if IsTmpfs("/dev/shm") && GetPathAvailableBytes("/dev/shm") >= requiredBytes {
			if dir, err := os.MkdirTemp("/dev/shm", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}

		if IsTmpfs("/tmp") && GetPathAvailableBytes("/tmp") >= requiredBytes {
			if dir, err := os.MkdirTemp("/tmp", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}
	}

	// Fallback to disk cache directory
	nestedCache := filepath.Join(GetDiskCacheDir(), "nested")
	_ = os.MkdirAll(nestedCache, 0755)

	dir, err := os.MkdirTemp(nestedCache, "7gl-disk-*")
	if err != nil {
		dir, _ = os.MkdirTemp("", "7gl-disk-*")
	}
	return dir, false
}
