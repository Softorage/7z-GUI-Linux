package app

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

var (
	CutOperation  = "cut   " // TODO: no hacky solution
	CopyOperation = "copy"

	GlobalClipboard []domain.ClipboardItem
	ClipboardMu     sync.Mutex
)

// GetClipboardClearOnSuccess returns whether the clipboard should auto-clear after a successful paste.
func GetClipboardClearOnSuccess() bool {
	UserConfigMu.RLock()
	defer UserConfigMu.RUnlock()
	return UserConfig.System.ClipboardClearOnSuccess
}

// SetClipboardClearOnSuccess updates and persists the clipboard auto-clear preference to config.yaml.
func SetClipboardClearOnSuccess(clear bool) {
	UserConfigMu.Lock()
	UserConfig.System.ClipboardClearOnSuccess = clear
	UserConfigMu.Unlock()
	_ = SaveConfig()
}

// IsTempDirPinned returns true if any item currently in the global clipboard
// resides within the given temporary directory path.
func IsTempDirPinned(tempDir string) bool {
	if tempDir == "" {
		return false
	}
	ClipboardMu.Lock()
	defer ClipboardMu.Unlock()

	for _, cb := range GlobalClipboard {
		if cb.IsArchive && strings.HasPrefix(cb.ArchivePath, tempDir) {
			return true
		}
	}
	return false
}

// RemoveFromClipboard removes paths matching deleted target entries from global clipboard state.
func RemoveFromClipboard(deletedPaths []string, isArchive bool) {
	ClipboardMu.Lock()
	defer ClipboardMu.Unlock()

	var newClipboard []domain.ClipboardItem
	for _, cbItem := range GlobalClipboard {
		keep := true
		for _, delPath := range deletedPaths {
			if !isArchive {
				if cbItem.IsArchive {
					continue
				}
				cbClean, delClean := filepath.Clean(cbItem.Path), filepath.Clean(delPath)
				if cbClean == delClean || strings.HasPrefix(cbClean, delClean+string(filepath.Separator)) {
					keep = false
					break
				}
			} else {
				if !cbItem.IsArchive {
					continue
				}
				cbClean, delClean := filepath.ToSlash(filepath.Clean(cbItem.Path)), filepath.ToSlash(filepath.Clean(delPath))
				if cbClean == delClean || strings.HasPrefix(cbClean, delClean+"/") {
					keep = false
					break
				}
			}
		}
		if keep {
			newClipboard = append(newClipboard, cbItem)
		}
	}
	GlobalClipboard = newClipboard
}