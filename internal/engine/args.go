package engine

import (
	"path/filepath"
	"strings"
)

// GetArchiveDestination calculates the full target path for the archive.
// Used in ui_compress and the build7zArgs arguments.
func GetArchiveDestination(sources []string, format string, customName string, sfx bool) string {
	if len(sources) == 0 {
		return ""
	}

	extMap := map[string]string{
		"7z":    ".7z",
		"xz":    ".xz",
		"bzip2": ".bz2",
		"gzip":  ".gz",
		"tar":   ".tar",
		"zip":   ".zip",
		"wim":   ".wim",
	}

	ext, ok := extMap[format]
	if !ok {
		ext = "." + format
	}

	// Only 7z truly supports SFX cleanly via standard 7-Zip module
	if sfx && format == "7z" {
		ext = ".exe"
	}

	firstSrc := sources[0]
	dir := filepath.Dir(firstSrc)

	var filename string
	if customName != "" {
		filename = customName
		// Ensure custom name has correct extension if not already present
		if !strings.HasSuffix(strings.ToLower(filename), ext) {
			filename += ext
		}
	} else {
		var base string
		if len(sources) == 1 {
			base = strings.TrimSuffix(filepath.Base(firstSrc), filepath.Ext(firstSrc))
		} else {
			// If packaging multiple items, standard UI logic sets the parent directory's folder name as default
			base = filepath.Base(dir)
			if base == "." || base == "/" || base == string(filepath.Separator) {
				base = "archive"
			}
		}
		filename = base + ext
	}

	return filepath.Join(dir, filename)
}

func Build7zArgs(src []string, customName string, format string, level string, method string, dictSize string, wordSize, blockSize string, threads, update string, sfx bool, shared bool, split string, enc bool, pass string, encName bool) []string {

	// Call unified helper to get the target destination
	dest := GetArchiveDestination(src, format, customName, sfx)

	// Determine if the format supports multi-file archiving/updating features
	updatableArchiveFormat := format != "tar" && format != "gzip" && format != "bzip2" && format != "xz"

	// Determine command line action (a = Add, u = Update)
	cmdAction := "a"
	var updateSwitches []string

	if updatableArchiveFormat {
		if update != "Add and replace files" {
			cmdAction = "u"
			if update == "Freshen existing files" {
				// -uw0 avoids adding new files that are on disk only
				updateSwitches = append(updateSwitches, "-uw0")
			} else if update == "Synchronize files" {
				// -up0 deletes files from the archive that are missing on disk
				updateSwitches = append(updateSwitches, "-up0")
			}
		}
	}

	// Build base parameters: command first, then output path, then all sources dynamically
	args := []string{cmdAction, dest}
	args = append(args, src...)
	args = append(args, "-bsp1", "-t"+format)
	args = append(args, updateSwitches...)

	// Only apply compression settings if the format supports it (tar does not)
	if format != "tar" {
		lvlMap := map[string]string{"Store": "0", "Fastest": "1", "Fast": "3", "Normal": "5", "Maximum": "7", "Ultra": "9"}
		args = append(args, "-mx="+lvlMap[level])

		// Only apply Method, Dictionary, and Word Size if we are compressing
		if level != "Store" {
			// Apply Compression Method
			if method != "" {
				if format == "zip" {
					args = append(args, "-mm="+method)
				} else if format == "7z" || format == "wim" {
					// 7z uses -m0 switch to assign a generic method
					args = append(args, "-m0="+method)
				}
			}

			// Dictionary and Word size are only reliably scalable across 7z and xz.
			// Exposing large dictionary values to deflaters (zip/gzip) will prompt "Unsupported Method" errors in 7z CLI
			if format == "7z" || format == "xz" {
				dictMap := map[string]string{
					"64 KB":  "64k",
					"1 MB":   "1m",
					"16 MB":  "16m",
					"32 MB":  "32m",
					"64 MB":  "64m",
					"128 MB": "128m",
				}
				if d, ok := dictMap[dictSize]; ok {
					args = append(args, "-md="+d)
				}

				if wordSize != "" {
					args = append(args, "-mfb="+wordSize)
				}
			}
		}
	}

	// Solid Block Size and Solid Sorting (-mqs) is primarily a 7z concept
	if format == "7z" {
		blockMap := map[string]string{
			"Non-solid": "off",
			"1 MB":      "1m",
			"16 MB":     "16m",
			"64 MB":     "64m",
			"256 MB":    "256m",
			"4 GB":      "4g",
			"Solid":     "on",
		}
		if b, ok := blockMap[blockSize]; ok {
			args = append(args, "-ms="+b)
		}

		if shared {
			// Instructs solid mode to sort by extension, intelligently matching similar files into the same data blocks
			args = append(args, "-mqs=on")
		}
	}

	// Compress shared files allows the system to read files opened/locked for writing by other applications
	if shared {
		args = append(args, "-ssw")
	}

	// Map threads (generally accepted across the board, ignored if unsupported)
	args = append(args, "-mmt="+threads)

	// Map Split (Not supported by tar/gzip/bzip2/xz)
	if split != "" && updatableArchiveFormat {
		args = append(args, "-v"+split)
	}

	if sfx && format == "7z" {
		args = append(args, "-sfx")
	}

	// Map Encryption (Only supported by 7z and zip)
	if enc && pass != "" && (format == "7z" || format == "zip") {
		args = append(args, "-p"+pass)

		// Only 7z supports header/filename encryption natively this way
		if encName && format == "7z" {
			args = append(args, "-mhe=on")
		}
	}

	return args
}