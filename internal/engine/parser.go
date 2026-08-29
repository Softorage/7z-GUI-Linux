package engine

import (
	"bufio"
	"bytes"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// ParseSLTReader stream-parses key-value structured text emitted by `7z l -slt` directly from an io.Reader.
// It avoids allocating the full process output as a string in memory and processes lines with byte slices.
func ParseSLTReader(r io.Reader, archivePath string) ([]domain.ArchiveItem, bool, error) {
	reader := bufio.NewReaderSize(r, 64*1024)
	baseArchive := filepath.Base(archivePath)

	items := make([]domain.ArchiveItem, 0, 128)
	var currentItem *domain.ArchiveItem
	var isSolid bool

	sep := []byte(" = ")
	solidPrefix := []byte("Solid = ")
	plusSign := []byte("+")

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Strip \r\n or \n
			line = bytes.TrimRight(line, "\r\n")
			trimmed := bytes.TrimSpace(line)

			if len(trimmed) == 0 {
				if currentItem != nil {
					if currentItem.Path != "" && currentItem.Path != baseArchive {
						items = append(items, *currentItem)
					}
					currentItem = nil
				}
			} else if bytes.HasPrefix(trimmed, solidPrefix) {
				val := bytes.TrimSpace(trimmed[len(solidPrefix):])
				isSolid = bytes.Equal(val, plusSign)
			} else if keyBytes, valBytes, found := bytes.Cut(line, sep); found {
				key := string(bytes.TrimSpace(keyBytes))
				valStr := string(valBytes)

				switch key {
				case "Path":
					if currentItem == nil {
						currentItem = &domain.ArchiveItem{}
					}
					valStr = filepath.ToSlash(valStr)
					if strings.HasSuffix(valStr, "/") || strings.HasSuffix(valStr, "\\") {
						currentItem.IsDir = true
						valStr = strings.TrimRight(valStr, "/\\")
					}
					currentItem.Path = valStr
				case "Folder":
					if currentItem != nil {
						currentItem.IsDir = (strings.TrimSpace(valStr) == "+")
					}
				case "Attributes":
					if currentItem != nil && strings.Contains(strings.ToUpper(valStr), "D") {
						currentItem.IsDir = true
					}
				case "Size":
					if currentItem != nil {
						size, _ := strconv.ParseInt(strings.TrimSpace(valStr), 10, 64)
						currentItem.Size = size
					}
				case "Modified":
					if currentItem != nil {
						currentItem.Modified = strings.TrimSpace(valStr)
					}
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, false, err
		}
	}

	if currentItem != nil && currentItem.Path != "" && currentItem.Path != baseArchive {
		items = append(items, *currentItem)
	}

	return items, isSolid, nil
}

// ParseSLTOutput parses key-value structured text emitted by `7z l -slt`.
// Kept for compatibility; wraps ParseSLTReader.
func ParseSLTOutput(outStr, archivePath string) ([]domain.ArchiveItem, bool, error) {
	return ParseSLTReader(strings.NewReader(outStr), archivePath)
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
