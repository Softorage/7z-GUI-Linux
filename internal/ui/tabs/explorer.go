package tabs

import (
	"fmt"
	//"io"
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/engine"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/components"
)

// TODO:
//
// add a help button that displays a modal(dialog) with help info in it. like: an iamge explaining each icon, how to add files to existing archives, how adding files to queue for creating new archives works (it adds the files over existing files ignoring duplicate file paths)
//
// use startoperations for all operations.
//
// pathbar: up, reload, archvie indicator, path, copy
// topbar: cut,copy,paste, separattor, clipboard, search bar, delete, separator, hide/show
// headers for columns, sort columns (on clicking headers)
// disable the buttons on bottombar when not relevant. there shouldnt be any button in primary (blue) color... honestly we should get rid of the part in extract button where it asks for destination when you select a file inside archive and extract it. instead it should say to 'simply copy/paste the file where you want it to be extracted'.
//
// zstd support

// explorerTabState holds the full runtime state for an individual tab in the File Explorer.
type explorerTabState struct {
	currentPath     string
	isArchive       bool
	archivePath     string
	archiveRelPath  string
	archivePassword string
	archiveStack    []domain.ArchiveLevel   // Navigation stack supporting arbitrary archive-in-archive depth
	archiveItems    []domain.ArchiveItem    // Full raw archive entry cache
	items           []domain.FileSystemItem // Display items currently visible in the active folder level
	selectedItems   map[string]bool         // Set of item names marked via checkboxes
	showHidden      bool

	badgeLabel *widget.Label
	pathEntry  *widget.Entry
	fileList   *widget.List
	tabItem    *container.TabItem
	cutBtn     *widget.Button
}

var (
	docTabs             *container.DocTabs
	explorerTabsState   = make(map[*container.TabItem]*explorerTabState)
	explorerTabsStateMu sync.RWMutex // Protects parallel access to tab states across goroutines
)

// Helper for Tab & Stack Management

// getDisplayArchivePath constructs breadcrumb display string showing nested archive path hierarchy.
func (state *explorerTabState) getDisplayArchivePath() string {
	if len(state.archiveStack) == 0 {
		rel := state.archiveRelPath
		if rel == "" {
			rel = "/"
		}
		return state.archivePath + " :: " + rel
	}
	var parts []string
	for _, lvl := range state.archiveStack {
		parts = append(parts, lvl.DisplayName)
	}
	rel := state.archiveRelPath
	if rel == "" {
		rel = "/"
	}
	return strings.Join(parts, " :: ") + " :: " + rel
}

// cleanupTempLevel deletes the level's temporary directory unless it is pinned in the clipboard.
func cleanupTempLevel(lvl domain.ArchiveLevel) {
	if lvl.TempDir != "" && !appstate.IsTempDirPinned(lvl.TempDir) {
		_ = os.RemoveAll(lvl.TempDir)
	}
}

// cleanupTemp removes temporary working folders created during nested archive exploration,
// provided the paths aren't currently pinned in the global clipboard.
func (state *explorerTabState) cleanupTemp() {
	var remainingStack []domain.ArchiveLevel
	for _, lvl := range state.archiveStack {
		if appstate.IsTempDirPinned(lvl.TempDir) {
			remainingStack = append(remainingStack, lvl)
		} else if lvl.TempDir != "" {
			_ = os.RemoveAll(lvl.TempDir)
		}
	}
	state.archiveStack = remainingStack
}

// propagateWriteBack updates outer parent archives when modifications occur within nested layers.
// Uses parent's password if protected and runs detached from global state mutex locks to prevent UI stalls.
func propagateWriteBack(state *explorerTabState) {
	if state == nil || len(state.archiveStack) <= 1 {
		return
	}
	for i := len(state.archiveStack) - 1; i > 0; i-- {
		child := state.archiveStack[i]
		parent := state.archiveStack[i-1]

		args := []string{"a", parent.ArchivePath, child.ArchivePath}
		if parent.ArchivePassword != "" {
			args = append(args, "-p"+parent.ArchivePassword)
		}
		cmd := exec.Command(engine.Root7zCmd, args...)
		cmd.Stdin = strings.NewReader("")
		_ = cmd.Run()
	}
}

// Main UI Construction

// BuildExplorerTab creates and sets up the primary Explorer tab view, including sidebar favorites and dynamic tabs.
func BuildExplorerTab(w fyne.Window) fyne.CanvasObject {
	appstate.FavoritesMu.Lock()
	if len(appstate.Favorites) == 0 {
		appstate.Favorites = appstate.GetInitialFavorites()
	}
	appstate.FavoritesMu.Unlock()

	appstate.FavList = widget.NewList(
		func() int {
			appstate.FavoritesMu.Lock()
			defer appstate.FavoritesMu.Unlock()
			return len(appstate.Favorites)
		},
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			appstate.FavoritesMu.Lock()
			defer appstate.FavoritesMu.Unlock()
			if id < len(appstate.Favorites) {
				o.(*widget.Label).SetText(appstate.Favorites[id].Name)
			}
		},
	)

	var selectedFavIndex = -1

	appstate.FavList.OnSelected = func(id widget.ListItemID) {
		selectedFavIndex = id
		appstate.FavoritesMu.Lock()
		if id >= len(appstate.Favorites) {
			appstate.FavoritesMu.Unlock()
			return
		}
		fav := appstate.Favorites[id]
		appstate.FavoritesMu.Unlock()

		activeTab := docTabs.Selected()
		if activeTab != nil {
			explorerTabsStateMu.Lock()
			state, ok := explorerTabsState[activeTab]
			explorerTabsStateMu.Unlock()
			if ok {
				state.cleanupTemp()
				state.isArchive = false
				state.currentPath = fav.Path
				state.archivePath = ""
				state.archiveRelPath = ""
				state.archivePassword = ""
				state.refresh(w)
			}
		} else {
			newTab := createBrowserTab(w, fav.Path)
			docTabs.Append(newTab)
			docTabs.Select(newTab)
		}
	}

	appstate.FavList.OnUnselected = func(id widget.ListItemID) { selectedFavIndex = -1 }

	addFavBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		activeTab := docTabs.Selected()
		if activeTab == nil {
			return
		}
		explorerTabsStateMu.Lock()
		state, ok := explorerTabsState[activeTab]
		explorerTabsStateMu.Unlock()

		if ok && !state.isArchive {
			dir := state.currentPath
			name := filepath.Base(dir)
			if name == "" || name == "." || name == "/" {
				name = "Root"
			}
			appstate.FavoritesMu.Lock()
			// Prevent adding duplicate entries
			exists := false
			for _, fav := range appstate.Favorites {
				if fav.Path == dir {
					exists = true
					break
				}
			}
			if !exists {
				appstate.Favorites = append(appstate.Favorites, domain.FavoriteItem{Name: name, Path: dir})
			}
			appstate.FavoritesMu.Unlock()
			appstate.UpdateFavoritesList()
		} else {
			dialog.ShowInformation("Action Restricted", "Favorites can only represent local directories on disk.", w)
		}
	})
	addFavBtn.Importance = widget.LowImportance

	removeFavBtn := widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
		if selectedFavIndex == -1 {
			dialog.ShowInformation("No Selection", "Please select a favorite item to remove.", w)
			return
		}
		appstate.FavoritesMu.Lock()
		if selectedFavIndex < len(appstate.Favorites) {
			appstate.Favorites = append(appstate.Favorites[:selectedFavIndex], appstate.Favorites[selectedFavIndex+1:]...)
		}
		appstate.FavoritesMu.Unlock()
		selectedFavIndex = -1
		appstate.UpdateFavoritesList()
	})
	removeFavBtn.Importance = widget.LowImportance

	renameFavBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if selectedFavIndex == -1 {
			dialog.ShowInformation("No Selection", "Please select a favorite item to rename.", w)
			return
		}
		appstate.FavoritesMu.Lock()
		currentName := appstate.Favorites[selectedFavIndex].Name
		appstate.FavoritesMu.Unlock()

		entry := widget.NewEntry()
		entry.SetText(currentName)

		d := dialog.NewForm("Rename Favorite", "Rename", "Cancel", []*widget.FormItem{widget.NewFormItem("Nickname", entry)}, func(confirmed bool) {
			if !confirmed {
				return
			}
			newName := strings.TrimSpace(entry.Text)
			if newName == "" || newName == currentName {
				return
			}
			appstate.FavoritesMu.Lock()
			if selectedFavIndex >= 0 && selectedFavIndex < len(appstate.Favorites) {
				appstate.Favorites[selectedFavIndex].Name = newName
			}
			appstate.FavoritesMu.Unlock()
			appstate.UpdateFavoritesList()
		}, w)
		d.Resize(fyne.NewSize(450, 180))
		d.Show()
	})
	renameFavBtn.Importance = widget.LowImportance

	favSidebar := container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Explorer"),
			widget.NewSeparator(), widget.NewSeparator(),
			widget.NewLabelWithStyle("Favorites", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
		),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(addFavBtn, removeFavBtn, renameFavBtn)),
		nil, nil, appstate.FavList,
	)

	docTabs = container.NewDocTabs()
	docTabs.CreateTab = func() *container.TabItem {
		homePath, err := os.UserHomeDir()
		if err != nil {
			homePath = "/"
		}
		return createBrowserTab(w, homePath)
	}

	docTabs.OnClosed = func(tab *container.TabItem) {
		explorerTabsStateMu.Lock()
		if state, ok := explorerTabsState[tab]; ok {
			state.cleanupTemp()
		}
		delete(explorerTabsState, tab)
		explorerTabsStateMu.Unlock()
	}

	homePath, err := os.UserHomeDir()
	if err != nil {
		homePath = "/"
	}
	initialTab := createBrowserTab(w, homePath)
	docTabs.Append(initialTab)
	docTabs.Select(initialTab)

	split := container.NewHSplit(favSidebar, docTabs)
	split.Offset = 0.2

	return container.NewPadded(split)
}

// createBrowserTab constructs an explorer browser view (path bar, toolbar, file list, bottom actions).
func createBrowserTab(w fyne.Window, initialPath string) *container.TabItem {
	state := &explorerTabState{
		currentPath:   initialPath,
		selectedItems: make(map[string]bool),
		showHidden:    false,
	}

	state.badgeLabel = widget.NewLabel("[Local Files]")
	state.badgeLabel.TextStyle = fyne.TextStyle{Bold: true}
	state.pathEntry = widget.NewEntry()

	upBtn := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() { state.goUp(w) })
	upBtn.Importance = widget.LowImportance

	copyPathBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		p := state.currentPath
		if state.isArchive {
			p = state.getDisplayArchivePath()
		}
		w.Clipboard().SetContent(p)
		appstate.SetInfo("Current path copied to clipboard.")
	})
	copyPathBtn.Importance = widget.LowImportance

	pathBar := container.NewBorder(nil, nil, upBtn, copyPathBtn, container.NewBorder(nil, nil, state.badgeLabel, nil, state.pathEntry))

	var list *widget.List
	list = widget.NewList(
		func() int { return len(state.items) },
		func() fyne.CanvasObject {
			check := widget.NewCheck("", nil)
			icon := widget.NewIcon(theme.FileIcon())
			name := widget.NewLabel("")
			size := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})
			modified := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})
			return container.NewBorder(nil, nil, check, nil, container.NewHBox(container.NewHBox(icon, name), layout.NewSpacer(), size, widget.NewSeparator(), modified))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(state.items) {
				return
			}
			item := state.items[id]
			border := o.(*fyne.Container)

			var check *widget.Check
			var rowContainer *fyne.Container
			for _, obj := range border.Objects {
				if chk, ok := obj.(*widget.Check); ok {
					check = chk
				} else if r, ok := obj.(*fyne.Container); ok {
					rowContainer = r
				}
			}
			if check == nil || rowContainer == nil {
				return
			}

			colName := rowContainer.Objects[0].(*fyne.Container)
			icon := colName.Objects[0].(*widget.Icon)
			name := colName.Objects[1].(*widget.Label)
			size := rowContainer.Objects[2].(*widget.Label)
			modified := rowContainer.Objects[4].(*widget.Label)

			// Temporarily unbind OnChanged to prevent false triggers during item recycled re-rendering
			check.OnChanged = nil
			check.SetChecked(state.selectedItems[item.Name])
			check.OnChanged = func(checked bool) { state.selectedItems[item.Name] = checked }

			name.SetText(sys.TruncateDisplayPath(item.Name, 40))

			if item.IsDir {
				if item.IsSymlink {
					icon.SetResource(theme.NavigateNextIcon())
					size.SetText("Directory Link")
				} else {
					icon.SetResource(theme.FolderIcon())
					size.SetText("Directory")
				}
			} else {
				if item.IsSymlink {
					icon.SetResource(theme.NavigateNextIcon())
					size.SetText("File Link")
				} else {
					icon.SetResource(theme.FileIcon())
					size.SetText(sys.FormatSize(item.Size))
				}
			}
			modified.SetText(item.Modified)
			icon.Refresh()
		},
	)

	// Single-click selection vs double-click navigation thresholding
	var lastClickedName string
	var lastClickedTime time.Time

	list.OnSelected = func(id widget.ListItemID) {
		list.Unselect(id)
		if id >= len(state.items) {
			return
		}
		item := state.items[id]
		now := time.Now()

		// Detect double-click within 300ms window
		if lastClickedName == item.Name && now.Sub(lastClickedTime) < 300*time.Millisecond {
			if item.IsDir {
				if state.isArchive {
					state.archiveRelPath = filepath.Join(state.archiveRelPath, item.Name)
					if len(state.archiveStack) > 0 {
						state.archiveStack[len(state.archiveStack)-1].ArchiveRelPath = state.archiveRelPath
					}
				} else {
					state.currentPath = item.Path
				}
				state.refresh(w)
			} else if sys.IsArchiveExtension(item.Name) {
				openArchiveLevel(w, state, item)
			}
		}
		lastClickedName = item.Name
		lastClickedTime = now
	}

	state.fileList = list

	// We disable cutBtn when user is in nested archive
	state.cutBtn = widget.NewButtonWithIcon("", theme.ContentCutIcon(), func() { addToClipboard(state, appstate.CutOperation) })
	state.cutBtn.Importance = widget.LowImportance

	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() { addToClipboard(state, appstate.CopyOperation) })
	copyBtn.Importance = widget.LowImportance

	pasteBtn := widget.NewButtonWithIcon("", theme.ContentPasteIcon(), func() { handlePaste(state, w) })
	pasteBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() { handleDelete(state, w) })
	deleteBtn.Importance = widget.LowImportance

	clipBtn := widget.NewButtonWithIcon("", theme.ListIcon(), func() { showClipboardDialog(w) })
	clipBtn.Importance = widget.LowImportance

	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { state.refresh(w) })
	refreshBtn.Importance = widget.LowImportance

	var showHiddenFilesBtn *widget.Button
	showHiddenFilesBtn = widget.NewButtonWithIcon("", theme.VisibilityOffIcon(), func() {
		state.showHidden = !state.showHidden
		if state.showHidden {
			showHiddenFilesBtn.SetIcon(theme.VisibilityIcon())
			appstate.SetInfo("Showing hidden files.")
		} else {
			showHiddenFilesBtn.SetIcon(theme.VisibilityOffIcon())
			appstate.SetInfo("Hiding hidden files.")
		}
		state.refresh(w)
	})
	showHiddenFilesBtn.Importance = widget.LowImportance

	topActionBar := container.NewHBox(state.cutBtn, copyBtn, pasteBtn, deleteBtn, layout.NewSpacer(), clipBtn, refreshBtn, showHiddenFilesBtn)

	compressContextBtn := widget.NewButtonWithIcon("Compress", theme.ConfirmIcon(), func() { handleContextCompress(state, w) })
	compressContextBtn.Importance = widget.HighImportance

	extractContextBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() { handleContextExtract(state, w) })
	extractContextBtn.Importance = widget.HighImportance

	copySelectedPathBtn := widget.NewButton("Copy Selected Path", func() { handleCopySelectedPath(state, w) })
	copySelectedPathBtn.Importance = widget.LowImportance

	checksumContextBtn := widget.NewButton("Checksum", func() { handleContextChecksum(state, w) })
	checksumContextBtn.Importance = widget.LowImportance

	bottomActionBar := container.NewHBox(compressContextBtn, extractContextBtn, layout.NewSpacer(), copySelectedPathBtn, checksumContextBtn)

	tabContent := container.NewBorder(
		container.NewVBox(pathBar, topActionBar, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), bottomActionBar),
		nil, nil, list,
	)

	state.refresh(w)

	tabTitle := filepath.Base(state.currentPath)
	if tabTitle == "." || tabTitle == "/" || tabTitle == "" {
		tabTitle = "Home"
	}
	tabItem := container.NewTabItem(tabTitle, tabContent)
	state.tabItem = tabItem

	explorerTabsStateMu.Lock()
	explorerTabsState[tabItem] = state
	explorerTabsStateMu.Unlock()

	return tabItem
}

// openArchiveLevel handles entering archives (including nested archives within virtual views).
func openArchiveLevel(w fyne.Window, state *explorerTabState, item domain.FileSystemItem) {
	targetPath := item.Path

	if state.isArchive {
		// Asynchronously extract nested archive inside existing virtual view
		go func() {
			uncompressedSize := uint64(item.Size)
			sizeMB := float64(uncompressedSize) / (1024 * 1024)

			if uncompressedSize > 100*1024*1024 {
				appstate.SetInfo(fmt.Sprintf("Decompressing nested archive %s (%.1f MB)...", item.Name, sizeMB))
			} else {
				appstate.SetInfo(fmt.Sprintf("Opening nested archive %s...", item.Name))
			}

			// Allocate temp workspace (RAM tmpfs or disk cache) based on user config
			appstate.UserConfigMu.RLock()
			ramPercent := appstate.UserConfig.System.RAMUsagePercent
			ramLimitMB := appstate.UserConfig.System.RAMLimitMB
			appstate.UserConfigMu.RUnlock()

			tempDir, isRAM := sys.SelectTempStorage(uncompressedSize, ramPercent, ramLimitMB)
			if isRAM {
				appstate.SetInfo(fmt.Sprintf("Extracting %s to RAM (tmpfs)...", item.Name))
			}

			if err := engine.ExtractArchive(state.archivePath, tempDir, state.archivePassword, item.Path); err != nil {
				os.RemoveAll(tempDir)
				fyne.Do(func() { dialog.ShowError(fmt.Errorf("failed to extract nested archive: %v", err), w) })
				return
			}

			extractedPath := filepath.Join(tempDir, filepath.FromSlash(item.Path))
			if _, err := os.Stat(extractedPath); err != nil {
				os.RemoveAll(tempDir)
				fyne.Do(func() { dialog.ShowError(fmt.Errorf("nested archive file not found after extraction: %v", err), w) })
				return
			}

			protected := engine.IsPasswordProtected(extractedPath)
			openNested := func(pwd string) {
				lvl := domain.ArchiveLevel{
					DisplayName:     item.Name,
					ArchivePath:     extractedPath,
					ArchiveRelPath:  "",
					ArchivePassword: pwd,
					TempDir:         tempDir,
				}
				state.archiveStack = append(state.archiveStack, lvl)
				state.isArchive = true
				state.archivePath = extractedPath
				state.archiveRelPath = ""
				state.archivePassword = pwd
				state.refresh(w)
			}

			if protected {
				fyne.Do(func() {
					components.PromptArchivePassword(w, extractedPath, "Open", openNested, func() {
						os.RemoveAll(tempDir)
						appstate.SetInfo("Opening password-protected archive cancelled.")
					})
				})
			} else {
				fyne.Do(func() { openNested("") })
			}
		}()
	} else {
		// Opening archive directly from local file system
		go func() {
			protected := engine.IsPasswordProtected(targetPath)
			openRoot := func(pwd string) {
				lvl := domain.ArchiveLevel{
					DisplayName:     filepath.Base(targetPath),
					ArchivePath:     targetPath,
					ArchiveRelPath:  "",
					ArchivePassword: pwd,
				}
				state.archiveStack = []domain.ArchiveLevel{lvl}
				state.isArchive = true
				state.archivePath = targetPath
				state.archiveRelPath = ""
				state.archivePassword = pwd
				state.refresh(w)
			}

			if protected {
				fyne.Do(func() {
					components.PromptArchivePassword(w, targetPath, "Open", openRoot, func() {
						appstate.SetInfo("Opening password-protected archive cancelled.")
					})
				})
			} else {
				fyne.Do(func() { openRoot("") })
			}
		}()
	}
}

// refresh reloads the folder/archive entry listing off the main thread and resets list selection states.
func (state *explorerTabState) refresh(w fyne.Window) {
	if state.isArchive {
		state.badgeLabel.SetText("[Archive View]")
		state.badgeLabel.Refresh()
		state.pathEntry.SetText(state.getDisplayArchivePath())

		go func() {
			all, _, err := engine.ListArchive(state.archivePath, state.archivePassword)
			if err != nil {
				fyne.Do(func() { dialog.ShowError(fmt.Errorf("failed to list archive contents: %v", err), w) })
				return
			}

			state.archiveItems = all
			virtualItems := sys.GetVirtualItems(all, state.archiveRelPath)

			fyne.Do(func() {
				state.items = virtualItems
				state.selectedItems = make(map[string]bool)
				if state.cutBtn != nil {
					if len(state.archiveStack) > 1 {
						state.cutBtn.Disable()
					} else {
						state.cutBtn.Enable()
					}
				}
				if state.fileList != nil {
					state.fileList.UnselectAll()
					state.fileList.Refresh()
				}

				tabTitle := filepath.Base(state.archivePath)
				if len(state.archiveStack) > 0 {
					tabTitle = state.archiveStack[len(state.archiveStack)-1].DisplayName
				}
				if state.archiveRelPath != "" {
					tabTitle += " :: " + filepath.Base(state.archiveRelPath)
				}
				state.tabItem.Text = tabTitle
				docTabs.Refresh()
			})
		}()
	} else {
		state.badgeLabel.SetText("[Local Files]")
		state.badgeLabel.Refresh()
		state.pathEntry.SetText(state.currentPath)

		go func() {
			localItems, err := sys.GetLocalItems(state.currentPath, state.showHidden)
			if err != nil {
				fyne.Do(func() { dialog.ShowError(err, w) })
				return
			}
			fyne.Do(func() {
				state.items = localItems
				state.selectedItems = make(map[string]bool)
				if state.cutBtn != nil {
					state.cutBtn.Enable()
				}
				if state.fileList != nil {
					state.fileList.UnselectAll()
					state.fileList.Refresh()
				}

				tabTitle := filepath.Base(state.currentPath)
				if tabTitle == "." || tabTitle == "/" || tabTitle == "" {
					tabTitle = "Home"
				}
				state.tabItem.Text = tabTitle
				docTabs.Refresh()
			})
		}()
	}
}

// goUp navigates up one level in the file hierarchy or nested archive stack.
func (state *explorerTabState) goUp(w fyne.Window) {
	if state.isArchive {
		if state.archiveRelPath != "" && state.archiveRelPath != "/" {
			parent := filepath.Dir(state.archiveRelPath)
			if parent == "." || parent == "/" {
				state.archiveRelPath = ""
			} else {
				state.archiveRelPath = parent
			}
			if len(state.archiveStack) > 0 {
				state.archiveStack[len(state.archiveStack)-1].ArchiveRelPath = state.archiveRelPath
			}
		} else {
			if len(state.archiveStack) > 1 {
				// Pop nested archive level off navigation stack
				top := state.archiveStack[len(state.archiveStack)-1]
				state.archiveStack = state.archiveStack[:len(state.archiveStack)-1]

				// Clean up top level temp dir if unpinned
				cleanupTempLevel(top)

				prev := state.archiveStack[len(state.archiveStack)-1]
				state.archivePath = prev.ArchivePath
				state.archiveRelPath = prev.ArchiveRelPath
				state.archivePassword = prev.ArchivePassword
			} else {
				// Pop out of archive view back to standard local file view
				state.cleanupTemp()
				state.isArchive = false
				state.currentPath = filepath.Dir(state.archivePath)
				state.archivePath = ""
				state.archiveRelPath = ""
				state.archivePassword = ""
			}
		}
	} else {
		parent := filepath.Dir(state.currentPath)
		if parent == state.currentPath {
			return
		}
		state.currentPath = parent
	}
	state.refresh(w)
}

// addToClipboard adds currently selected explorer items to the thread-safe app clipboard.
func addToClipboard(state *explorerTabState, op string) {
	if state.isArchive && len(state.archiveStack) > 1 && op == appstate.CutOperation {
		appstate.SetInfo("Cut operation is disabled in nested archives.")
		return
	}

	appstate.ClipboardMu.Lock()
	defer appstate.ClipboardMu.Unlock()

	hasSelection := false
	addedCount, updatedCount := 0, 0
	var lastUpdatedFrom, lastUpdatedTo, lastUpdatedPath string

	for name, selected := range state.selectedItems {
		if !selected {
			continue
		}
		hasSelection = true

		var item domain.FileSystemItem
		found := false
		for _, it := range state.items {
			if it.Name == name {
				item = it
				found = true
				break
			}
		}
		if !found {
			continue
		}

		// Search if the item path is already present in the clipboard
		existsIdx := -1
		for idx, cbItem := range appstate.GlobalClipboard {
			if cbItem.Path == item.Path && cbItem.IsArchive == state.isArchive && cbItem.ArchivePath == state.archivePath {
				existsIdx = idx
				break
			}
		}

		if existsIdx != -1 {
			oldOp := appstate.GlobalClipboard[existsIdx].Op
			if oldOp != op {
				appstate.GlobalClipboard[existsIdx].Op = op
				updatedCount++
				lastUpdatedFrom = oldOp
				lastUpdatedTo = op
				lastUpdatedPath = item.Name
			}
		} else {
			appstate.GlobalClipboard = append(appstate.GlobalClipboard, domain.ClipboardItem{
				Path:        item.Path,
				IsDir:       item.IsDir,
				Op:          op,
				IsArchive:   state.isArchive,
				ArchivePath: state.archivePath,
				Password:    state.archivePassword,
			})
			addedCount++
		}
	}

	if !hasSelection {
		appstate.SetInfo("No items selected to copy/cut.")
		return
	}

	if updatedCount > 0 && addedCount == 0 {
		if updatedCount == 1 {
			appstate.SetInfo(fmt.Sprintf("Clipboard updated for '%s': changed from %s to %s.", lastUpdatedPath, lastUpdatedFrom, lastUpdatedTo))
		} else {
			appstate.SetInfo(fmt.Sprintf("Updated %d items in clipboard to %s.", updatedCount, op))
		}
	} else if addedCount > 0 && updatedCount == 0 {
		appstate.SetInfo(fmt.Sprintf("Added %d item(s) to clipboard (%s).", addedCount, op))
	} else if addedCount > 0 && updatedCount > 0 {
		appstate.SetInfo(fmt.Sprintf("Added %d new item(s) and updated %d existing item(s) to %s.", addedCount, updatedCount, op))
	} else {
		appstate.SetInfo(fmt.Sprintf("Selected item(s) already in clipboard as %s.", op))
	}

	state.selectedItems = make(map[string]bool)
	if state.fileList != nil {
		state.fileList.UnselectAll()
		state.fileList.Refresh()
	}
}

type pasteContext struct {
	window          fyne.Window
	hasGlobalAction bool
	globalAction    domain.ConflictAction
	usedNames       map[string]bool
	cancelled       bool
	typeConflicts   []domain.TypeConflictInfo
	typeConflictsMu sync.Mutex
}

// resolveConflict prompts the user or applies an established batch action (e.g. Skip All, Replace All).
func resolveConflict(ctx *pasteContext, filename string) domain.ConflictAction {
	if ctx.cancelled {
		return domain.ActionCancel
	}
	if ctx.hasGlobalAction {
		return ctx.globalAction
	}

	action := components.PromptConflict(ctx.window, filename)
	if action == domain.ActionCancel {
		ctx.cancelled = true
	} else if action == domain.ActionReplaceAll {
		ctx.globalAction = domain.ActionReplace
		ctx.hasGlobalAction = true
		return domain.ActionReplace
	} else if action == domain.ActionSkipAll {
		ctx.globalAction = domain.ActionSkip
		ctx.hasGlobalAction = true
		return domain.ActionSkip
	} else if action == domain.ActionRenameAll {
		ctx.globalAction = domain.ActionRename
		ctx.hasGlobalAction = true
		return domain.ActionRename
	}
	return action
}

// handlePaste executes paste operation into current local directory or virtual archive target.
func handlePaste(state *explorerTabState, w fyne.Window) {
	appstate.ClipboardMu.Lock()
	if len(appstate.GlobalClipboard) == 0 {
		appstate.ClipboardMu.Unlock()
		dialog.ShowInformation("Clipboard Empty", "No items are currently in your custom clipboard.", w)
		return
	}
	itemsCopy := make([]domain.ClipboardItem, len(appstate.GlobalClipboard))
	copy(itemsCopy, appstate.GlobalClipboard)
	appstate.ClipboardMu.Unlock()

	if state.isArchive {
		if sys.IsSingleFileArchive(state.archivePath) {
			dialog.ShowError(fmt.Errorf("%s archives do not support adding multiple items or folders", filepath.Ext(state.archivePath)), w)
			return
		}

		_, isSolid, err := engine.ListArchive(state.archivePath, state.archivePassword)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		onSuccess := func() {
			if appstate.ClipboardClearOnSuccess {
				appstate.ClipboardMu.Lock()
				appstate.GlobalClipboard = nil
				appstate.ClipboardMu.Unlock()
			}
			fyne.Do(func() {
				state.refresh(w)
				appstate.SetInfo("Successfully added items to archive.")
			})
		}

		addFilesToArchive(state.archivePath, state.archiveRelPath, state.archivePassword, itemsCopy, w, isSolid, onSuccess)
	} else {
		go func() {
			appstate.SetInfo("Pasting items...")

			// Extract virtual archive items to a temporary location first
			pathMap, tempDir, err := engine.ExtractArchiveItems(itemsCopy)
			if err != nil {
				fyne.Do(func() { dialog.ShowError(err, w) })
				return
			}
			if tempDir != "" {
				defer os.RemoveAll(tempDir)
			}

			var errors []error
			ctx := &pasteContext{window: w, usedNames: make(map[string]bool)}

			for _, item := range itemsCopy {
				if ctx.cancelled {
					break
				}

				srcPath := item.Path
				if item.IsArchive {
					srcPath = pathMap[item.Path]
				}

				baseName := filepath.Base(item.Path)
				dstPath := filepath.Join(state.currentPath, baseName)

				if err = copyFileOrDir(ctx, srcPath, dstPath); err != nil {
					if err.Error() == "cancelled by user" {
						appstate.SetInfo("Paste operation cancelled by user.")
						break
					}
					errors = append(errors, err)
					continue
				}

				if item.Op == appstate.CutOperation {
					if item.IsArchive {
						// For a cutOperation from inside an archive, delete it from the source archive
						cmd := exec.Command(engine.Root7zCmd, "d", item.ArchivePath, item.Path)
						cmd.Stdin = strings.NewReader("")
						if delErr := cmd.Run(); delErr != nil {
							errors = append(errors, fmt.Errorf("failed to remove cut item from source archive: %w", delErr))
						}
					} else {
						_ = os.RemoveAll(srcPath)
					}
				}
			}

			fyne.Do(func() {
				if len(errors) > 0 {
					dialog.ShowError(fmt.Errorf("completed with errors: %v", errors), w)
				} else if ctx.cancelled {
					appstate.SetInfo("Paste operation stopped.")
				} else {
					appstate.SetInfo("Paste completed successfully.")
					if appstate.ClipboardClearOnSuccess {
						appstate.ClipboardMu.Lock()
						appstate.GlobalClipboard = nil
						appstate.ClipboardMu.Unlock()
					}
				}

				// Inform the user if any type conflicts were handled during the run
				if len(ctx.typeConflicts) > 0 {
					var sb strings.Builder
					for _, conflict := range ctx.typeConflicts {
						sb.WriteString(fmt.Sprintf("Name: %s, Src: %s, Dst: %s, Resolution: %v\n", conflict.Name, conflict.SrcPath, conflict.DstPath, conflict.Resolution))
					}
					appstate.SetInfo("Type Conflict: \n" + sb.String()) // TODO: simply log instead of setInfo feedback
				}

				state.refresh(w)
			})
		}()
	}
}

// handleDelete deletes selected items from local disk or virtual archive path.
func handleDelete(state *explorerTabState, w fyne.Window) {
	var targets []string
	for name, selected := range state.selectedItems {
		if selected {
			targets = append(targets, name)
		}
	}

	if len(targets) == 0 {
		dialog.ShowInformation("No Selection", "Please select items to delete.", w)
		return
	}

	msg := fmt.Sprintf("Are you sure you want to delete these %d selected items?", len(targets))
	dialog.ShowConfirm("Confirm Delete", msg, func(confirmed bool) {
		if !confirmed {
			return
		}

		if state.isArchive {
			if sys.IsSingleFileArchive(state.archivePath) {
				dialog.ShowError(fmt.Errorf("%s archives do not support item deletion", filepath.Ext(state.archivePath)), w)
				return
			}

			onSuccess := func() {
				var deletedPaths []string
				for _, t := range targets {
					deletedPaths = append(deletedPaths, filepath.Join(state.archiveRelPath, t))
				}
				appstate.RemoveFromClipboard(deletedPaths, true)

				fyne.Do(func() {
					state.refresh(w)
					appstate.SetInfo("Successfully deleted from archive.")
				})
			}
			var relPaths []string
			for _, t := range targets {
				relPaths = append(relPaths, filepath.Join(state.archiveRelPath, t))
			}
			deleteFromArchive(state.archivePath, relPaths, state.archivePassword, w, onSuccess)
		} else {
			go func() {
				appstate.SetInfo("Deleting items...")
				var errors []error
				var deletedPaths []string
				for _, t := range targets {
					fullPath := filepath.Join(state.currentPath, t)
					if err := os.RemoveAll(fullPath); err != nil {
						errors = append(errors, err)
					} else {
						deletedPaths = append(deletedPaths, fullPath)
					}
				}

				if len(deletedPaths) > 0 {
					appstate.RemoveFromClipboard(deletedPaths, false)
				}

				fyne.Do(func() {
					if len(errors) > 0 {
						dialog.ShowError(fmt.Errorf("delete failed for some items: %v", errors), w)
					} else {
						appstate.SetInfo("Deletion completed.")
					}
					state.refresh(w)
				})
			}()
		}
	}, w)
}

// handleContextCompress forwards selected disk paths to the Compress panel tab.
func handleContextCompress(state *explorerTabState, w fyne.Window) {
	var targets []string
	for name, selected := range state.selectedItems {
		if selected {
			if state.isArchive {
				dialog.ShowError(fmt.Errorf("cannot compress virtual archive items directly; extract first"), w)
				return
			}
			targets = append(targets, filepath.Join(state.currentPath, name))
		}
	}

	if len(targets) == 0 {
		dialog.ShowInformation("No Selection", "Please select files or folders to compress.", w)
		return
	}

	if CompressSrcEntry != nil {
		CompressSrcEntry.SetText(strings.Join(targets, "\n"))
	}
	if appstate.Tabs != nil {
		appstate.Tabs.Select(domain.CompressTabRank)
	}
	appstate.SetInfo("Selected files loaded into Compress panel.")
}

// handleContextExtract shows helpful dialog for virtual archive selection and sends local archives to Extract panel.
func handleContextExtract(state *explorerTabState, w fyne.Window) {
	if state.isArchive {
		dialog.ShowInformation("Browsing Archive", "You are currently browsing through an archive. To extract from an archive, simply copy it and paste to the destination (which can also be another archive).", w)
		return
	} else {
		var targetArchives []string
		for name, selected := range state.selectedItems {
			if selected && sys.IsArchiveExtension(name) {
				targetArchives = append(targetArchives, filepath.Join(state.currentPath, name))
			}
		}

		if len(targetArchives) == 0 {
			dialog.ShowInformation("No Archive Selected", "Please select an archive file on disk to extract.", w)
			return
		}

		if ExtractSrcEntry != nil {
			ExtractSrcEntry.SetText(strings.Join(targetArchives, "\n"))
		}
		if appstate.Tabs != nil {
			appstate.Tabs.Select(domain.ExtractTabRank)
		}
		appstate.SetInfo("Selected archives loaded into Extract panel.")
	}
}

// handleCopySelectedPath copies clean path representation of single selected entry to OS clipboard.
func handleCopySelectedPath(state *explorerTabState, w fyne.Window) {
	var target string
	count := 0
	for name, selected := range state.selectedItems {
		if selected {
			target = name
			count++
		}
	}

	if count != 1 {
		dialog.ShowInformation("Selection Error", "Please select exactly one item to copy its path.", w)
		return
	}

	fullPath := filepath.Join(state.currentPath, target)
	if state.isArchive {
		fullPath = state.archivePath + " :: " + filepath.ToSlash(filepath.Clean(filepath.Join(state.archiveRelPath, target)))
	}

	w.Clipboard().SetContent(fullPath)
	appstate.SetInfo("Selected path copied to clipboard.")
}

// handleContextChecksum loads single local file path into Checksum calculation panel.
func handleContextChecksum(state *explorerTabState, w fyne.Window) {
	var target string
	count := 0
	for name, selected := range state.selectedItems {
		if selected {
			isDir := false
			for _, it := range state.items {
				if it.Name == name {
					isDir = it.IsDir
					break
				}
			}
			if isDir {
				dialog.ShowError(fmt.Errorf("checksum calculation is supported only on files"), w)
				return
			}
			target = name
			count++
		}
	}

	if count != 1 {
		dialog.ShowInformation("Selection Error", "Please select exactly one file to calculate checksums.", w)
		return
	}

	if state.isArchive {
		dialog.ShowError(fmt.Errorf("checksum calculations require extracting the file onto local disk first"), w)
		return
	}

	if ChecksumFileEntry != nil {
		ChecksumFileEntry.SetText(filepath.Join(state.currentPath, target))
	}
	if appstate.Tabs != nil {
		appstate.Tabs.Select(domain.ChecksumTabRank)
	}
	appstate.SetInfo("Selected file loaded into Checksum panel.")
}

// addFilesToArchive stages items in temporary directory and runs `7z a -snh` to store physical files.
func addFilesToArchive(archivePath, relPath, password string, items []domain.ClipboardItem, w fyne.Window, isSolid bool, onSuccess func()) {
	allEntries, _, err := engine.ListArchive(archivePath, password)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	existingPaths := make(map[string]bool)
	for _, entry := range allEntries {
		existingPaths[filepath.Clean(entry.Path)] = true
	}

	go func() {
		// Extract virtual source archive items first
		pathMap, extractDir, err := engine.ExtractArchiveItems(items)
		if err != nil {
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}

		var itemsToStage []domain.StageItem
		globalAction := domain.ActionCancel
		hasGlobalAction := false

		for _, item := range items {
			srcPath := item.Path
			if item.IsArchive {
				srcPath = pathMap[item.Path]
			}

			baseName := filepath.Base(item.Path)
			archiveDstRelPath := filepath.Join(relPath, baseName)
			archiveDstClean := filepath.Clean(archiveDstRelPath)

			var action domain.ConflictAction
			if existingPaths[archiveDstClean] {
				if hasGlobalAction {
					action = globalAction
				} else {
					action = components.PromptConflict(w, baseName)
					if action == domain.ActionReplaceAll {
						globalAction = domain.ActionReplace
						hasGlobalAction = true
						action = domain.ActionReplace
					} else if action == domain.ActionSkipAll {
						globalAction = domain.ActionSkip
						hasGlobalAction = true
						action = domain.ActionSkip
					} else if action == domain.ActionCancel {
						if extractDir != "" {
							os.RemoveAll(extractDir)
						}
						return
					}
				}
			} else {
				action = domain.ActionReplace
			}

			if action == domain.ActionSkip {
				continue
			}

			dstName := baseName
			if action == domain.ActionRename {
				dstName = sys.GetUniqueArchiveDstPath(baseName, relPath, existingPaths)
				existingPaths[filepath.Clean(filepath.Join(relPath, dstName))] = true
			}

			itemsToStage = append(itemsToStage, domain.StageItem{SrcPath: srcPath, DstName: dstName, IsDir: item.IsDir})
		}

		if len(itemsToStage) == 0 {
			if extractDir != "" {
				os.RemoveAll(extractDir)
			}
			return
		}

		tempDir, err := os.MkdirTemp("", "7gl-stage-*")
		if err != nil {
			if extractDir != "" {
				os.RemoveAll(extractDir)
			}
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}

		targetDir := tempDir
		if relPath != "" {
			targetDir = filepath.Join(tempDir, relPath)
			if err = os.MkdirAll(targetDir, 0755); err != nil {
				os.RemoveAll(tempDir)
				if extractDir != "" {
					os.RemoveAll(extractDir)
				}
				fyne.Do(func() { dialog.ShowError(err, w) })
				return
			}
		}

		for _, item := range itemsToStage {
			destPath := filepath.Join(targetDir, item.DstName)
			// Prefer symlinking into temp directory to avoid wasteful disk copies prior to compression
			if err = os.Symlink(item.SrcPath, destPath); err != nil {
				// Stage to target without conflict prompts (pass nil pasteContext)
				if err = copyFileOrDir(nil, item.SrcPath, destPath); err != nil {
					os.RemoveAll(tempDir)
					if extractDir != "" {
						os.RemoveAll(extractDir)
					}
					fyne.Do(func() { dialog.ShowError(err, w) })
					return
				}
			}
		}

		var args []string
		if relPath != "" {
			cleanRel := filepath.ToSlash(filepath.Clean(relPath))
			topFolder := strings.Split(cleanRel, "/")[0]
			args = []string{"a", "-snh", archivePath, topFolder}
		} else {
			args = []string{"a", "-snh", archivePath}
			for _, item := range itemsToStage {
				args = append(args, item.DstName)
			}
		}
		if password != "" {
			args = append(args, "-p"+password)
		}

		cleanupAndRun := func() {
			fyne.Do(func() {
				targetArchive := filepath.Base(archivePath)
				engine.StartOperation(targetArchive, args, "Adding to Archive", tempDir, w, func() {
					os.RemoveAll(tempDir)
					if extractDir != "" {
						os.RemoveAll(extractDir)
					}

					var stateCopy *explorerTabState
					explorerTabsStateMu.RLock()
					if activeTab := docTabs.Selected(); activeTab != nil {
						stateCopy = explorerTabsState[activeTab]
					}
					explorerTabsStateMu.RUnlock()

					if stateCopy != nil {
						propagateWriteBack(stateCopy)
					}

					if onSuccess != nil {
						onSuccess()
					}
				})
			})
		}

		if isSolid {
			fyne.Do(func() {
				dialog.ShowConfirm(
					"Modify Solid Archive?",
					"Modifying a solid archive: this operation may take longer as 7-Zip must decompress and re-compress solid blocks. Proceed?",
					func(confirmed bool) {
						if confirmed {
							go cleanupAndRun()
						} else {
							os.RemoveAll(tempDir)
							if extractDir != "" {
								os.RemoveAll(extractDir)
							}
						}
					},
					w,
				)
			})
		} else {
			cleanupAndRun()
		}
	}()
}

// deleteFromArchive executes `7z d` to delete relative paths from target archive.
func deleteFromArchive(archivePath string, relPaths []string, password string, w fyne.Window, onSuccess func()) {
	args := append([]string{"d", archivePath}, relPaths...)
	if password != "" {
		args = append(args, "-p"+password)
	}
	targetArchive := filepath.Base(archivePath)
	engine.StartOperation(targetArchive, args, "Deleting from Archive", "", w, func() {
		var stateCopy *explorerTabState
		explorerTabsStateMu.RLock()
		if activeTab := docTabs.Selected(); activeTab != nil {
			stateCopy = explorerTabsState[activeTab]
		}
		explorerTabsStateMu.RUnlock()

		if stateCopy != nil {
			propagateWriteBack(stateCopy)
		}

		if onSuccess != nil {
			onSuccess()
		}
	})
}

// copyFileOrDir handles recursive directory/file copying while resolving conflicts or type mismatches.
func copyFileOrDir(ctx *pasteContext, src, dst string) error {
	info, err := os.Lstat(src) // Read entry metadata without resolving symlinks
	if err != nil {
		return err
	}

	dstInfo, err := os.Lstat(dst)
	dstExists := err == nil

	if dstExists && ctx != nil {
		isTypeConflict := info.IsDir() != dstInfo.IsDir()
		var action domain.ConflictAction

		if isTypeConflict {
			// Safe global actions (Rename All or Skip All) require no user interaction and can be automatically applied.
			// Destructive global actions (Replace All) must be ignored, forcing the explicit warnings in promptTypeConflict.
			if ctx.hasGlobalAction && (ctx.globalAction == domain.ActionRename || ctx.globalAction == domain.ActionSkip) {
				action = ctx.globalAction
			} else {
				action = components.PromptTypeConflict(ctx.window, filepath.Base(dst), info.IsDir(), dstInfo.IsDir())
				if action == domain.ActionRenameAll {
					ctx.globalAction = domain.ActionRename
					ctx.hasGlobalAction = true
					action = domain.ActionRename
				} else if action == domain.ActionSkipAll {
					ctx.globalAction = domain.ActionSkip
					ctx.hasGlobalAction = true
					action = domain.ActionSkip
				}
			}

			// Log how this type mismatch was handled
			resolution := "Cancelled"
			switch action {
			case domain.ActionSkip:
				resolution = "Skipped"
			case domain.ActionRename:
				tempDst := sys.GetUniqueDstPath(dst, ctx.usedNames)
				resolution = fmt.Sprintf("Renamed to '%s'", filepath.Base(tempDst))
			case domain.ActionReplace:
				resolution = "Replaced (Existing directory/file deleted)"
			}

			ctx.typeConflictsMu.Lock()
			ctx.typeConflicts = append(ctx.typeConflicts, domain.TypeConflictInfo{
				Name:       filepath.Base(dst),
				SrcPath:    src,
				DstPath:    dst,
				Resolution: resolution,
			})
			ctx.typeConflictsMu.Unlock()
		} else {
			action = resolveConflict(ctx, filepath.Base(dst))
		}

		if action == domain.ActionSkip {
			return nil
		}
		if action == domain.ActionCancel {
			return fmt.Errorf("cancelled by user")
		}
		if action == domain.ActionRename {
			dst = sys.GetUniqueDstPath(dst, ctx.usedNames)
			ctx.usedNames[filepath.Base(dst)] = true
			dstExists = false
		} else if action == domain.ActionReplace {
			if isTypeConflict {
				// Permanently delete the conflicting mismatched path
				_ = os.RemoveAll(dst)
				dstExists = false
			}
		}
	}
	// Check for symlink
	if info.Mode()&os.ModeSymlink != 0 {
		// Perform a shallow copy by recreating the link
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if dstExists {
			_ = os.Remove(dst) // Remove any pre-existing file/link at destination
		}
		return os.Symlink(target, dst)
	}

	if info.IsDir() {
		return copyDir(ctx, src, dst)
	}

	return sys.CopyFile(src, dst)
}

// copyDir recursively traverses source directory entries and creates destination folders.
func copyDir(ctx *pasteContext, src, dst string) error {
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
		if ctx != nil && ctx.cancelled {
			return fmt.Errorf("cancelled by user")
		}

		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if err := copyFileOrDir(ctx, s, d); err != nil {
			return err
		}
	}
	return nil
}

// showClipboardDialog constructs custom window modal listing current staged clipboard items.
func showClipboardDialog(w fyne.Window) {
	appstate.ClipboardMu.Lock()
	// Create a local copy to avoid holding the global clipboard lock during UI initialization and rendering
	localItems := make([]domain.ClipboardItem, len(appstate.GlobalClipboard))
	copy(localItems, appstate.GlobalClipboard)
	appstate.ClipboardMu.Unlock()

	var list *widget.List
	list = widget.NewList(
		func() int { return len(localItems) },
		func() fyne.CanvasObject {
			pathLbl := widget.NewLabel("")
			typeLbl := widget.NewLabel("")
			opLbl := widget.NewLabel("")
			delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			delBtn.Importance = widget.LowImportance
			return container.NewBorder(nil, nil, nil, delBtn, container.NewHBox(opLbl, typeLbl, pathLbl))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(localItems) {
				return
			}
			item := localItems[id]

			border := o.(*fyne.Container)
			var delBtn *widget.Button
			var hBox *fyne.Container

			for _, obj := range border.Objects {
				if btn, ok := obj.(*widget.Button); ok {
					delBtn = btn
				} else if hb, ok := obj.(*fyne.Container); ok {
					hBox = hb
				}
			}

			if hBox == nil || delBtn == nil {
				return
			}

			opLbl := hBox.Objects[0].(*widget.Label)
			typeLbl := hBox.Objects[1].(*widget.Label)
			pathLbl := hBox.Objects[2].(*widget.Label)

			if item.Op == appstate.CutOperation {
				opLbl.Importance = widget.DangerImportance
			} else {
				opLbl.Importance = widget.SuccessImportance
			}
			opLbl.SetText(strings.ToUpper(item.Op))
			opLbl.Refresh()

			if item.IsDir {
				typeLbl.SetText("[Dir]")
			} else {
				typeLbl.SetText("[File]")
			}

			if item.IsArchive {
				pathLbl.SetText(fmt.Sprintf("%s :: %s", filepath.Base(item.ArchivePath), sys.TruncateDisplayPath(item.Path, 40)))
			} else {
				pathLbl.SetText(sys.TruncateDisplayPath(item.Path, 50))
			}

			delBtn.OnTapped = func() {
				appstate.ClipboardMu.Lock()
				if id < len(localItems) {
					localItems = append(localItems[:id], localItems[id+1:]...)
				}
				appstate.GlobalClipboard = make([]domain.ClipboardItem, len(localItems))
				copy(appstate.GlobalClipboard, localItems)
				appstate.ClipboardMu.Unlock()
				list.Refresh()
			}
		},
	)

	clearBtn := widget.NewButton("Clear All", func() {
		appstate.ClipboardMu.Lock()
		localItems = nil
		appstate.GlobalClipboard = nil
		appstate.ClipboardMu.Unlock()
		list.Refresh()
	})
	clearBtn.Importance = widget.DangerImportance

	clearOnSuccessCheck := widget.NewCheck("Clear clipboard on successful paste", func(checked bool) {
		appstate.ClipboardClearOnSuccess = checked
	})
	clearOnSuccessCheck.SetChecked(appstate.ClipboardClearOnSuccess)

	content := container.NewBorder(
		container.NewVBox(clearOnSuccessCheck, widget.NewSeparator()),
		container.NewHBox(layout.NewSpacer(), clearBtn),
		nil, nil, list,
	)

	d := dialog.NewCustom("Custom Clipboard", "Close", content, w)
	d.Resize(fyne.NewSize(600, 400))
	d.Show()
}