package engine

import (
	"fmt"
	"strings"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// ParseDictSizeBytes parses a dictionary size string (e.g. "64 KB", "32 MB") into raw bytes.
func ParseDictSizeBytes(dictStr string) uint64 {
	trimmed := strings.TrimSpace(dictStr)
	switch trimmed {
	case "64 KB":
		return 64 * 1024
	case "1 MB":
		return 1 * 1024 * 1024
	case "16 MB":
		return 16 * 1024 * 1024
	case "32 MB":
		return 32 * 1024 * 1024
	case "64 MB":
		return 64 * 1024 * 1024
	case "128 MB":
		return 128 * 1024 * 1024
	default:
		return 32 * 1024 * 1024 // Safe 32 MB default
	}
}

// EstimateCompressionMemory calculates approximate memory required for compression and decompression.
// Formulas closely replicate 7-Zip's internal allocation tables without heap allocations.
func EstimateCompressionMemory(format, level, method string, dictBytes uint64, threads int) (compBytes, decompBytes uint64) {
	if threads < 1 {
		threads = 1
	}

	const mb uint64 = 1024 * 1024
	const threadBuffer uint64 = 16 * mb

	if format == "tar" || level == "Store" || method == "Copy" {
		return 2 * mb, 1 * mb
	}

	switch format {
	case "bzip2":
		compBytes = (32*mb + dictBytes) * uint64(threads)
		decompBytes = 4 * mb
		return compBytes, decompBytes

	case "gzip":
		compBytes = 2 * mb * uint64(threads)
		decompBytes = 1 * mb
		return compBytes, decompBytes

	case "wim":
		compBytes = 100 * mb * uint64(threads)
		decompBytes = 20 * mb
		return compBytes, decompBytes
	}

	// Format is 7z, xz, or zip
	switch method {
	case "BZip2":
		compBytes = (32*mb + dictBytes) * uint64(threads)
		decompBytes = 4 * mb

	case "Deflate", "Deflate64":
		compBytes = 2 * mb * uint64(threads)
		decompBytes = 1 * mb

	case "PPMd":
		compBytes = (dictBytes + 8*mb) * uint64(threads)
		decompBytes = dictBytes + 2*mb

	default:
		// LZMA / LZMA2 (standard default for 7z and xz)
		var multiplier float64
		if level == "Fastest" || level == "Fast" {
			multiplier = 7.5 // HC4 match finder
		} else {
			multiplier = 11.5 // BT4 match finder (Normal, Maximum, Ultra)
		}

		memPerThread := uint64(float64(dictBytes)*multiplier) + threadBuffer
		compBytes = memPerThread * uint64(threads)
		decompBytes = dictBytes + 2*mb
	}

	return compBytes, decompBytes
}

// ResolveMemoryLimitBytes converts the user-selected value and unit into an absolute byte cap.
func ResolveMemoryLimitBytes(unit string, value int, totalRAM uint64) uint64 {
	const mb uint64 = 1024 * 1024
	const gb uint64 = 1024 * mb

	if totalRAM == 0 {
		totalRAM = 8 * gb // Fallback baseline
	}

	switch unit {
	case domain.MemoryUnitPercent:
		if value < domain.MinMemoryLimitPercent {
			value = domain.MinMemoryLimitPercent
		} else if value > 100 {
			value = 100
		}
		return (totalRAM * uint64(value)) / 100

	case domain.MemoryUnitMB:
		if value < domain.MinMemoryLimitMB {
			value = domain.MinMemoryLimitMB
		}
		limit := uint64(value) * mb
		if limit > totalRAM {
			limit = totalRAM
		}
		return limit

	case domain.MemoryUnitGB:
		if value < 1 {
			value = 1
		}
		limit := uint64(value) * gb
		if limit > totalRAM {
			limit = totalRAM
		}
		return limit

	default:
		// domain.MemoryUnitDefault: 80% of total RAM (7-Zip native default)
		return (totalRAM * 80) / 100
	}
}

// GovernThreads determines the maximum threads allowable within the memory budget.
// It returns the allowed thread count, predicted memory, and whether even 1 thread exceeds the budget.
func GovernThreads(format, level, method, dictSize string, reqThreads int, limitBytes uint64) (allowedThreads int, compBytes, decompBytes uint64, xceedsSingleThread bool) {
	if reqThreads < 1 {
		reqThreads = 1
	}
	dictBytes := ParseDictSizeBytes(dictSize)

	singleThreadComp, decomp := EstimateCompressionMemory(format, level, method, dictBytes, 1)
	if limitBytes > 0 && singleThreadComp > limitBytes {
		return 1, singleThreadComp, decomp, true
	}

	fullComp, _ := EstimateCompressionMemory(format, level, method, dictBytes, reqThreads)
	if limitBytes == 0 || fullComp <= limitBytes {
		return reqThreads, fullComp, decomp, false
	}

	// Step down threads until predicted usage fits within the limit
	memPerThread := singleThreadComp
	if memPerThread == 0 {
		memPerThread = 1
	}
	maxPossibleThreads := int(limitBytes / memPerThread)
	if maxPossibleThreads < 1 {
		maxPossibleThreads = 1
	}
	if maxPossibleThreads > reqThreads {
		maxPossibleThreads = reqThreads
	}

	compBytes, decompBytes = EstimateCompressionMemory(format, level, method, dictBytes, maxPossibleThreads)
	return maxPossibleThreads, compBytes, decompBytes, false
}

// SupportsMemuse checks if the configured 7-Zip executable supports the -mmemuse flag.
func SupportsMemuse(rootCmd string) bool {
	lower := strings.ToLower(rootCmd)
	// Modern 7-Zip standalone or POSIX builds contain "7zz"
	return strings.Contains(lower, "7zz")
}

// FormatMemuseArg constructs the -mmemuse switch if supported by the active 7-Zip binary.
func FormatMemuseArg(unit string, value int, totalRAM uint64, rootCmd string) string {
	if !SupportsMemuse(rootCmd) {
		return ""
	}
	switch unit {
	case domain.MemoryUnitPercent:
		return fmt.Sprintf("-mmemuse=p%d", value)
	case domain.MemoryUnitMB:
		return fmt.Sprintf("-mmemuse=%dm", value)
	case domain.MemoryUnitGB:
		return fmt.Sprintf("-mmemuse=%dg", value)
	default:
		return "-mmemuse=p80"
	}
}
