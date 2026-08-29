package engine

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// ParseSLTOutput parses key-value structured text emitted by `7z l -slt`.
// Preserves deliberate leading/trailing whitespace in property values (e.g. filenames).
func ParseSLTOutput(outStr, archivePath string) ([]domain.ArchiveItem, bool, error) {
	lines := strings.Split(outStr, "\n")
	var items []domain.ArchiveItem
	var currentItem *domain.ArchiveItem
	var isSolid bool

	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			// Blank line signifies end of property block for current entry
			if currentItem != nil {
				items = append(items, *currentItem)
				currentItem = nil
			}
			continue
		}

		if strings.HasPrefix(trimmedLine, "Solid = ") {
			isSolid = strings.TrimSpace(trimmedLine[len("Solid = "):]) == "+"
		}

		if parts := strings.SplitN(line, " = ", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := parts[1] // Retain exact spacing in value; only strip trailing carriage return

			switch key {
			case "Path":
				if currentItem == nil {
					currentItem = &domain.ArchiveItem{}
				}
				val = filepath.ToSlash(val)
				if strings.HasSuffix(val, "/") || strings.HasSuffix(val, "\\") {
					currentItem.IsDir = true
					val = strings.TrimSuffix(strings.TrimSuffix(val, "/"), "\\")
				}
				currentItem.Path = val
			case "Folder":
				if currentItem != nil {
					currentItem.IsDir = (strings.TrimSpace(val) == "+")
				}
			case "Attributes":
				if currentItem != nil && strings.Contains(strings.ToUpper(val), "D") {
					currentItem.IsDir = true
				}
			case "Size":
				if currentItem != nil {
					size, _ := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
					currentItem.Size = size
				}
			case "Modified":
				if currentItem != nil {
					currentItem.Modified = strings.TrimSpace(val)
				}
			}
		}
	}
	if currentItem != nil {
		items = append(items, *currentItem)
	}

	// Filter out top-level self-referential archive container entry if present
	var filtered []domain.ArchiveItem
	for _, it := range items {
		if it.Path != "" && it.Path != filepath.Base(archivePath) {
			filtered = append(filtered, it)
		}
	}

	return filtered, isSolid, nil
}

// ParseHashesFromLog scans log outputs and isolates algorithm results.
func ParseHashesFromLog(allLines []string) map[string]string {
	hashes := make(map[string]string)

	// Scan backward to pull target checksum markers specifically from the latest run
	for i := len(allLines) - 1; i >= 0; i-- {
		line := allLines[i]
		if strings.Contains(line, "Running:") {
			// Stop scanning when reaching the header boundary of the current execution
			break
		}
		if strings.Contains(line, "for data:") {
			parts := strings.Split(line, "for data:")
			if len(parts) == 2 {
				algo := strings.TrimSpace(parts[0])
				hashVal := strings.TrimSpace(parts[1])

				// Standardize output formats
				if idx := strings.Index(hashVal, "-"); idx != -1 {
					hashVal = hashVal[:idx]
				}
				hashes[strings.ToUpper(algo)] = strings.TrimSpace(hashVal)
			}
		}
	}
	return hashes
}
