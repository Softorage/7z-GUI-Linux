package sys

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// Global active RAM staging quota tracking across all open archive tabs
var activeRAMStagingBytes atomic.Uint64

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

func readMeminfo(buf []byte) int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	n, _ := f.Read(buf)
	return n
}

// GetTotalRAMBytes reads Linux `/proc/meminfo` to calculate total system RAM.
func GetTotalRAMBytes() uint64 {
	var buf [2048]byte
	n := readMeminfo(buf[:])
	if n == 0 {
		return 0
	}
	val, _ := parseMeminfoValue(buf[:n], []byte("MemTotal:"))
	return val
}

// GetAvailableRAMBytes reads Linux `/proc/meminfo` to calculate available system RAM.
func GetAvailableRAMBytes() uint64 {
	var buf [2048]byte
	n := readMeminfo(buf[:])
	if n == 0 {
		return 2 * 1024 * 1024 * 1024 // 2GB fallback
	}
	if val, ok := parseMeminfoValue(buf[:n], []byte("MemAvailable:")); ok {
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

type ramCandidate struct {
	baseDir     string
	parentMount string
	minReserve  uint64 // Hard safety floor in bytes
}

var ramCandidates [3]ramCandidate

func init() {
	idx := 0
	if xdgRuntime := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); xdgRuntime != "" {
		ramCandidates[idx] = ramCandidate{
			baseDir:     filepath.Join(xdgRuntime, domain.AppDirName),
			parentMount: xdgRuntime,
			minReserve:  128 * 1024 * 1024, // 128 MB floor for Wayland/PipeWire/session sockets
		}
		idx++
	}
	uid := os.Getuid()
	ramCandidates[idx] = ramCandidate{
		baseDir:     fmt.Sprintf("/dev/shm/7gl-%d", uid),
		parentMount: "/dev/shm",
		minReserve:  256 * 1024 * 1024, // 256 MB floor for browser/IPC shared memory
	}
	idx++
	ramCandidates[idx] = ramCandidate{
		baseDir:     fmt.Sprintf("/tmp/7gl-%d", uid),
		parentMount: "/tmp",
		minReserve:  256 * 1024 * 1024, // 256 MB floor for general temporary workloads
	}
}

// GetRAMBaseDir resolves the primary active user-isolated tmpfs mount for telemetry and staging.
func GetRAMBaseDir() (string, bool) {
	for _, c := range ramCandidates {
		if c.baseDir != "" && IsTmpfs(c.parentMount) {
			return c.baseDir, true
		}
	}
	return "", false
}

// hasSufficientRAMHeadroom verifies that requiredBytes can be staged while preserving
// at least an 83.4% usable limit and a safety cushion (minReserve).
// Uses pure division instead of multiplication to guarantee immunity from uint64 overflow.
func hasSufficientRAMHeadroom(parentMount string, requiredBytes, minReserve uint64) bool {
	avail := GetPathAvailableBytes(parentMount)
	ratioReserve := avail / 6 // ~16.6% reserve (83.4% usable); strictly overflow-safe
	reserve := minReserve
	if ratioReserve > reserve {
		reserve = ratioReserve
	}
	return avail > reserve && requiredBytes <= avail-reserve
}

// SelectTempStorage decides whether to stage uncompressed files in RAM (tmpfs) or disk storage.
// strictly bounded by available system RAM, user preferences, and global active staging quotas.
func SelectTempStorage(category string, requiredBytes uint64, ramPercent int, ramLimitMB int64) (string, bool, uint64) {
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

	// Verify that aggregate RAM staging across all open tabs remains within the budget
	// NOTE: Subtraction used instead of addition (currentActive + requiredBytes <= ramBudget).
	// If an archive reports an adversarial or corrupted uncompressed size near MaxUint64,
	// addition wraps around to a small integer and silently bypasses the budget check.
	currentActive := activeRAMStagingBytes.Load()
	if currentActive <= ramBudget && requiredBytes <= ramBudget-currentActive {
		// Evaluate candidate tmpfs mounts in priority order with practical headroom cushions (zero heap allocation).
		for _, c := range ramCandidates {
			if c.baseDir == "" {
				continue
			}
			if IsTmpfs(c.parentMount) && hasSufficientRAMHeadroom(c.parentMount, requiredBytes, c.minReserve) {
				if err := os.MkdirAll(c.baseDir, 0700); err == nil {
					if dir, err := os.MkdirTemp(c.baseDir, "7gl-ram-*"); err == nil {
						activeRAMStagingBytes.Add(requiredBytes)
						return dir, true, requiredBytes
					}
				}
			}
		}
	}

	// Fallback to disk cache scratch/nested partition
	diskScratch := GetDiskScratchDir(category)
	dir, err := os.MkdirTemp(diskScratch, "7gl-disk-*")
	if err != nil {
		dir, _ = os.MkdirTemp("", "7gl-disk-*")
	}
	return dir, false, 0
}

// CleanupStaleWorkspaces purges leftover scratch directories older than 2 hours from prior crashes.
func CleanupStaleWorkspaces() {
	const maxAge = 2 * time.Hour
	now := time.Now()

	cleanDir := func(parent string) {
		entries, err := os.ReadDir(parent)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "7gl-") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > maxAge {
				_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
			}
		}
	}

	cleanDir(filepath.Join(GetDiskCacheDir(), domain.DiskSubdirScratch))
	cleanDir(filepath.Join(GetDiskCacheDir(), domain.DiskSubdirNested))

	for _, c := range ramCandidates {
		if c.baseDir != "" {
			cleanDir(c.baseDir)
		}
	}
}

// ReleaseTempStorage removes the temporary workspace and decrements any active RAM staging quota.
func ReleaseTempStorage(dir string, isRAM bool, allocatedBytes uint64) {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	if isRAM && allocatedBytes > 0 {
		activeRAMStagingBytes.Add(^uint64(allocatedBytes - 1)) // Atomic subtraction
	}
}
