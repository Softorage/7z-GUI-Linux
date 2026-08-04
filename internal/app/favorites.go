package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fyne.io/fyne/v2/widget"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

var (
	Favorites   []domain.FavoriteItem
	FavoritesMu sync.Mutex
	FavList     *widget.List
)

// GetInitialFavorites discovers initial bookmarks in the user's home directory.
func GetInitialFavorites() []domain.FavoriteItem {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}
	var dirs []domain.FavoriteItem
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, domain.FavoriteItem{Name: e.Name(), Path: filepath.Join(home, e.Name())})
		}
		if len(dirs) >= 5 {
			break
		}
	}
	return dirs
}

// UpdateFavoritesList refreshes the UI list widget and triggers persistence to config
func UpdateFavoritesList() {
	if FavList != nil {
		FavList.Refresh()
	}
	_ = SaveConfig()
}