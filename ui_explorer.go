package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ncruces/zenity"
)

// fileSystemItem represents a local directory entry or a virtual file inside an archive
type fileSystemItem struct {
	Name     string
	Path     string // Full disk path or full internal relative path
	IsDir    bool
	Size     int64
	Modified string
}

// archiveItem holds the parsed metadata for entries within a compressed archive
type archiveItem struct {
	Path     string
	IsDir    bool
	Size     int64
	Modified string
}

// explorerTabState holds the isolated runtime data of an individual browser tab
type explorerTabState struct {
	currentPath    string
	isArchive      bool
	archivePath    string
	archiveRelPath string
	archiveItems   []archiveItem
	items          []fileSystemItem
	selectedItems  map[string]bool
	showHidden     bool

	badgeLabel *widget.Label
	pathEntry  *widget.Entry
	fileList   *widget.List
	tabItem    *container.TabItem
}

// clipboardItem handles items stored in our custom application clipboard
type clipboardItem struct {
	Path  string
	IsDir bool
	Op    string // "cut" or "copy"
}

// favoriteItem handles the metadata of user-saved directories
type favoriteItem struct {
	Name string
	Path string
}

var (
	docTabs           *container.DocTabs
	explorerTabsState   = make(map[*container.TabItem]*explorerTabState)
	explorerTabsStateMu sync.RWMutex

	globalClipboard         []clipboardItem
	clipboardClearOnSuccess = true
	clipboardMu             sync.Mutex

	favorites   []favoriteItem
	favoritesMu sync.Mutex
	favList     *widget.List
)

// buildExplorerTab constructs the main view structure of the Explorer tab
func buildExplorerTab(w fyne.Window) fyne.CanvasObject {
	// Pre-populate with up to 5 non-hidden home subdirectories alphabetically
	favorites = getInitialFavorites()

	favList = widget.NewList(
		func() int {
			favoritesMu.Lock()
			defer favoritesMu.Unlock()
			return len(favorites)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			favoritesMu.Lock()
			defer favoritesMu.Unlock()
			if id >= len(favorites) {
				return
			}
			o.(*widget.Label).SetText(favorites[id].Name)
		},
	)

	var selectedFavIndex int = -1

	favList.OnSelected = func(id widget.ListItemID) {
		selectedFavIndex = id
		favoritesMu.Lock()
		if id >= len(favorites) {
			favoritesMu.Unlock()
			return
		}
		fav := favorites[id]
		favoritesMu.Unlock()

		activeTab := docTabs.Selected()
		if activeTab != nil {
			explorerTabsStateMu.Lock()
			state, ok := explorerTabsState[activeTab]
			explorerTabsStateMu.Unlock()

			if ok {
				state.isArchive = false
				state.currentPath = fav.Path
				state.archivePath = ""
				state.archiveRelPath = ""
				state.refresh(w)
			}
		}
	}

	favList.OnUnselected = func(id widget.ListItemID) {
		selectedFavIndex = -1
	}

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
			favoritesMu.Lock()
			// Prevent adding duplicate entries
			exists := false
			for _, fav := range favorites {
				if fav.Path == dir {
					exists = true
					break
				}
			}
			if !exists {
				favorites = append(favorites, favoriteItem{
					Name: name,
					Path: dir,
				})
			}
			favoritesMu.Unlock()
			updateFavoritesList()
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
		favoritesMu.Lock()
		if selectedFavIndex < len(favorites) {
			favorites = append(favorites[:selectedFavIndex], favorites[selectedFavIndex+1:]...)
		}
		favoritesMu.Unlock()
		selectedFavIndex = -1
		updateFavoritesList()
	})
	removeFavBtn.Importance = widget.LowImportance

	renameFavBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if selectedFavIndex == -1 {
			dialog.ShowInformation("No Selection", "Please select a favorite item to rename.", w)
			return
		}
		favoritesMu.Lock()
		currentName := favorites[selectedFavIndex].Name
		favoritesMu.Unlock()

		dialog.ShowEntryDialog("Rename Favorite", "Enter a nickname for this location:", func(newName string) {
			newName = strings.TrimSpace(newName)
			if newName == "" || newName == currentName {
				return
			}
			favoritesMu.Lock()
			if selectedFavIndex >= 0 && selectedFavIndex < len(favorites) {
				favorites[selectedFavIndex].Name = newName
			}
			favoritesMu.Unlock()
			updateFavoritesList()
		}, w)
	})
	renameFavBtn.Importance = widget.LowImportance

	favToolbar := container.NewHBox(addFavBtn, removeFavBtn, renameFavBtn)
	favSidebar := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Favorites", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			favToolbar,
		),
		nil,
		nil,
		favList,
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

	rightLayout := docTabs

	split := container.NewHSplit(favSidebar, rightLayout)
	split.Offset = 0.2

	finalLayout := container.NewPadded(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Explorer"),
			widget.NewSeparator(),
		),
		nil,
		nil,
		nil,
		split,
	))

	return finalLayout
}

// createBrowserTab instantiates the UI components and context for a unique tab
func createBrowserTab(w fyne.Window, initialPath string) *container.TabItem {
	state := &explorerTabState{
		currentPath:   initialPath,
		selectedItems: make(map[string]bool),
		showHidden:    false,
	}

	state.badgeLabel = widget.NewLabel("[Local Files]")
	state.badgeLabel.TextStyle = fyne.TextStyle{Bold: true}

	state.pathEntry = widget.NewEntry()
	state.pathEntry.Disable()

	upBtn := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		state.goUp(w)
	})
	upBtn.Importance = widget.LowImportance

	copyPathBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		p := state.currentPath
		if state.isArchive {
			rel := state.archiveRelPath
			if rel == "" {
				rel = "/"
			}
			p = state.archivePath + " :: " + rel
		}
		w.Clipboard().SetContent(p)
		setInfo("Current path copied to clipboard.")
	})
	copyPathBtn.Importance = widget.LowImportance

	pathBar := container.NewBorder(nil, nil, upBtn, copyPathBtn, container.NewBorder(nil, nil, state.badgeLabel, nil, state.pathEntry))

	var list *widget.List
	list = widget.NewList(
		func() int {
			return len(state.items)
		},
		func() fyne.CanvasObject {
			check := widget.NewCheck("", nil)
			icon := widget.NewIcon(theme.FileIcon())
			name := widget.NewLabel("")
			size := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})
			modified := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})

			colName := container.NewHBox(icon, name)
			row := container.NewHBox(colName, layout.NewSpacer(), size, widget.NewSeparator(), modified)
			return container.NewBorder(nil, nil, check, nil, row)
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

			check.OnChanged = nil
			check.SetChecked(state.selectedItems[item.Name])
			check.OnChanged = func(checked bool) {
				state.selectedItems[item.Name] = checked
			}

			name.SetText(truncateDisplayPath(item.Name, 50))
			if item.IsDir {
				icon.SetResource(theme.FolderIcon())
				size.SetText("Directory")
			} else {
				icon.SetResource(theme.FileIcon())
				size.SetText(formatSize(item.Size))
			}
			modified.SetText(item.Modified)

			icon.Refresh()
		},
	)

	var lastClickedName string
	var lastClickedTime time.Time

	list.OnSelected = func(id widget.ListItemID) {
		list.Unselect(id)
		if id >= len(state.items) {
			return
		}
		item := state.items[id]

		now := time.Now()
		if lastClickedName == item.Name && now.Sub(lastClickedTime) < 300*time.Millisecond {
			if item.IsDir {
				if state.isArchive {
					state.archiveRelPath = filepath.Join(state.archiveRelPath, item.Name)
				} else {
					state.currentPath = item.Path
				}
				state.refresh(w)
			} else {
				ext := strings.ToLower(filepath.Ext(item.Name))
				isArch := ext == ".7z" || ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".bz2" || ext == ".xz" || ext == ".wim" || ext == ".rar"
				if isArch {
					state.isArchive = true
					state.archivePath = item.Path
					state.archiveRelPath = ""
					state.refresh(w)
				}
			}
		}
		lastClickedName = item.Name
		lastClickedTime = now
	}

	state.fileList = list

	cutBtn := widget.NewButtonWithIcon("", theme.ContentCutIcon(), func() {
		addToClipboard(state, "cut")
	})
	cutBtn.Importance = widget.LowImportance

	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		addToClipboard(state, "copy")
	})
	copyBtn.Importance = widget.LowImportance

	pasteBtn := widget.NewButtonWithIcon("", theme.ContentPasteIcon(), func() {
		handlePaste(state, w)
	})
	pasteBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		handleDelete(state, w)
	})
	deleteBtn.Importance = widget.LowImportance

	clipBtn := widget.NewButtonWithIcon("", theme.ListIcon(), func() {
		showClipboardDialog(w)
	})
	clipBtn.Importance = widget.LowImportance

	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		state.refresh(w)
	})
	refreshBtn.Importance = widget.LowImportance

	var showHiddenFilesBtn *widget.Button
	showHiddenFilesBtn = widget.NewButtonWithIcon("", theme.VisibilityOffIcon(), func() {
		state.showHidden = !state.showHidden
		if state.showHidden {
			showHiddenFilesBtn.SetIcon(theme.VisibilityIcon())
			setInfo("Showing hidden files.")
		} else {
			showHiddenFilesBtn.SetIcon(theme.VisibilityOffIcon())
			setInfo("Hiding hidden files.")
		}
		state.refresh(w)
	})
	showHiddenFilesBtn.Importance = widget.LowImportance


	topActionBar := container.NewHBox(
		cutBtn, copyBtn, pasteBtn, deleteBtn, layout.NewSpacer(), clipBtn, refreshBtn, showHiddenFilesBtn,
	)

	compressContextBtn := widget.NewButtonWithIcon("Compress", theme.ConfirmIcon(), func() {
		handleContextCompress(state, w)
	})
	compressContextBtn.Importance = widget.HighImportance

	extractContextBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() {
		handleContextExtract(state, w)
	})
	extractContextBtn.Importance = widget.HighImportance

	copySelectedPathBtn := widget.NewButton("Copy Selected Path", func() {
		handleCopySelectedPath(state, w)
	})
	copySelectedPathBtn.Importance = widget.LowImportance

	checksumContextBtn := widget.NewButton("Checksum", func() {
		handleContextChecksum(state, w)
	})
	checksumContextBtn.Importance = widget.LowImportance

	bottomActionBar := container.NewHBox(
		compressContextBtn, extractContextBtn, layout.NewSpacer(), copySelectedPathBtn, checksumContextBtn,
	)

	tabContent := container.NewBorder(
		container.NewVBox(pathBar, topActionBar, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), bottomActionBar),
		nil,
		nil,
		list,
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

// refresh re-loads directory and virtual archive contents onto the interface
func (state *explorerTabState) refresh(w fyne.Window) {
	if state.isArchive {
		state.badgeLabel.SetText("[Archive View]")
		state.badgeLabel.Refresh()

		rel := state.archiveRelPath
		if rel == "" {
			rel = "/"
		}
		state.pathEntry.SetText(state.archivePath + " :: " + rel)

		go func() {
			all, _, err := parseArchiveEntries(state.archivePath)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf("failed to list archive contents: %v", err), w)
				})
				return
			}

			state.archiveItems = all
			virtualItems := getVirtualItems(all, state.archiveRelPath)

			fyne.Do(func() {
				state.items = virtualItems
				state.selectedItems = make(map[string]bool)
				state.fileList.Refresh()

				tabTitle := filepath.Base(state.archivePath)
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
			localItems, err := getLocalItems(state.currentPath, state.showHidden)
			if err != nil {
				fyne.Do(func() {
					dialog.ShowError(err, w)
				})
				return
			}
			fyne.Do(func() {
				state.items = localItems
				state.selectedItems = make(map[string]bool)
				state.fileList.Refresh()

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

// goUp processes upward navigation logic for both nested paths and archive borders
func (state *explorerTabState) goUp(w fyne.Window) {
	if state.isArchive {
		if state.archiveRelPath == "" || state.archiveRelPath == "/" {
			state.isArchive = false
			state.currentPath = filepath.Dir(state.archivePath)
			state.archivePath = ""
			state.archiveRelPath = ""
		} else {
			parent := filepath.Dir(state.archiveRelPath)
			if parent == "." || parent == "/" {
				state.archiveRelPath = ""
			} else {
				state.archiveRelPath = parent
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

func updateFavoritesList() {
	// The list widget's callbacks (Length and Update) already handle their own locking
	favList.Refresh()
}

func getInitialFavorites() []favoriteItem {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}
	var dirs []favoriteItem
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, favoriteItem{
				Name: e.Name(),
				Path: filepath.Join(home, e.Name()),
			})
		}
		if len(dirs) >= 5 {
			break
		}
	}
	return dirs
}

func getLocalItems(dirPath string, showHidden bool) ([]fileSystemItem, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	var dirs []fileSystemItem
	var files []fileSystemItem

	for _, entry := range entries {
		name := entry.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(dirPath, name)
		info, err := entry.Info()
		size := int64(0)
		modified := ""
		if err == nil {
			size = info.Size()
			modified = info.ModTime().Format("2006-01-02 15:04:05")
		}

		item := fileSystemItem{
			Name:     name,
			Path:     fullPath,
			IsDir:    entry.IsDir(),
			Size:     size,
			Modified: modified,
		}

		if entry.IsDir() {
			dirs = append(dirs, item)
		} else {
			files = append(files, item)
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	return append(dirs, files...), nil
}

func parseArchiveEntries(archivePath string) ([]archiveItem, bool, error) {
	cmd := exec.Command(root7zCmd, "l", "-slt", archivePath)
	out, err := cmd.Output()
	if err != nil {
		return nil, false, err
	}

	lines := strings.Split(string(out), "\n")
	var items []archiveItem
	var currentItem *archiveItem
	var isSolid bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if currentItem != nil {
				items = append(items, *currentItem)
				currentItem = nil
			}
			continue
		}

		if strings.HasPrefix(line, "Solid = ") {
			isSolid = strings.TrimSpace(line[len("Solid = "):]) == "+"
		}

		if strings.Contains(line, " = ") {
			parts := strings.SplitN(line, " = ", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])

			switch key {
			case "Path":
				if currentItem == nil {
					currentItem = &archiveItem{}
				}
				currentItem.Path = val
			case "Folder":
				if currentItem != nil {
					currentItem.IsDir = (val == "+")
				}
			case "Size":
				if currentItem != nil {
					size, _ := strconv.ParseInt(val, 10, 64)
					currentItem.Size = size
				}
			case "Modified":
				if currentItem != nil {
					currentItem.Modified = val
				}
			}
		}
	}
	if currentItem != nil {
		items = append(items, *currentItem)
	}

	var filtered []archiveItem
	for _, it := range items {
		if it.Path != "" && it.Path != filepath.Base(archivePath) {
			filtered = append(filtered, it)
		}
	}

	return filtered, isSolid, nil
}

func getVirtualItems(all []archiveItem, currentRelPath string) []fileSystemItem {
	seenDirs := make(map[string]bool)
	var dirs []fileSystemItem
	var files []fileSystemItem

	prefix := ""
	if currentRelPath != "" {
		prefix = currentRelPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}

	for _, item := range all {
		path := item.Path
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			continue
		}

		rel := strings.TrimPrefix(path, prefix)
		if rel == "" {
			continue
		}

		parts := strings.Split(rel, "/")
		if len(parts) > 1 {
			dirName := parts[0]
			if !seenDirs[dirName] {
				seenDirs[dirName] = true
				dirs = append(dirs, fileSystemItem{
					Name:     dirName,
					Path:     prefix + dirName,
					IsDir:    true,
					Size:     0,
					Modified: item.Modified,
				})
			}
		} else {
			fItem := fileSystemItem{
				Name:     rel,
				Path:     item.Path,
				IsDir:    item.IsDir,
				Size:     item.Size,
				Modified: item.Modified,
			}
			if item.IsDir {
				dirs = append(dirs, fItem)
			} else {
				files = append(files, fItem)
			}
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	return append(dirs, files...)
}

func addToClipboard(state *explorerTabState, op string) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	hasSelection := false
	addedCount := 0
	updatedCount := 0
	var lastUpdatedFrom, lastUpdatedTo string
	var lastUpdatedPath string

	for name, selected := range state.selectedItems {
		if selected {
			hasSelection = true
			var item fileSystemItem
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
			for idx, cbItem := range globalClipboard {
				if cbItem.Path == item.Path {
					existsIdx = idx
					break
				}
			}

			if existsIdx != -1 {
				oldOp := globalClipboard[existsIdx].Op
				if oldOp != op {
					globalClipboard[existsIdx].Op = op
					updatedCount++
					lastUpdatedFrom = oldOp
					lastUpdatedTo = op
					lastUpdatedPath = item.Name
				}
			} else {
				globalClipboard = append(globalClipboard, clipboardItem{
					Path:  item.Path,
					IsDir: item.IsDir,
					Op:    op,
				})
				addedCount++
			}
		}
	}

	if !hasSelection {
		setInfo("No items selected to copy/cut.")
		return
	}

	if updatedCount > 0 && addedCount == 0 {
		if updatedCount == 1 {
			setInfo(fmt.Sprintf("Clipboard updated for '%s': changed from %s to %s.", lastUpdatedPath, lastUpdatedFrom, lastUpdatedTo))
		} else {
			setInfo(fmt.Sprintf("Updated %d items in clipboard to %s.", updatedCount, op))
		}
	} else if addedCount > 0 && updatedCount == 0 {
		setInfo(fmt.Sprintf("Added %d item(s) to clipboard (%s).", addedCount, op))
	} else if addedCount > 0 && updatedCount > 0 {
		setInfo(fmt.Sprintf("Added %d new item(s) and updated %d existing item(s) to %s.", addedCount, updatedCount, op))
	} else {
		setInfo(fmt.Sprintf("Selected item(s) already in clipboard as %s.", op))
	}

	state.selectedItems = make(map[string]bool)
	state.fileList.Refresh()
}

func handlePaste(state *explorerTabState, w fyne.Window) {
	clipboardMu.Lock()
	if len(globalClipboard) == 0 {
		clipboardMu.Unlock()
		dialog.ShowInformation("Clipboard Empty", "No items are currently in your custom clipboard.", w)
		return
	}
	itemsCopy := make([]clipboardItem, len(globalClipboard))
	copy(itemsCopy, globalClipboard)
	clipboardMu.Unlock()

	if state.isArchive {
		ext := strings.ToLower(filepath.Ext(state.archivePath))
		if ext == ".gz" || ext == ".bz2" || ext == ".xz" {
			dialog.ShowError(fmt.Errorf("%s archives do not support adding multiple items or folders", ext), w)
			return
		}

		_, isSolid, err := parseArchiveEntries(state.archivePath)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		onSuccess := func() {
			if clipboardClearOnSuccess {
				clipboardMu.Lock()
				globalClipboard = nil
				clipboardMu.Unlock()
			}
			fyne.Do(func() {
				state.refresh(w)
				setInfo("Successfully added items to archive.")
			})
		}

		addFilesToArchive(state.archivePath, state.archiveRelPath, itemsCopy, w, isSolid, onSuccess)
	} else {
		go func() {
			setInfo("Pasting items...")
			var errors []error
			for _, item := range itemsCopy {
				dstPath := filepath.Join(state.currentPath, filepath.Base(item.Path))
				var err error
				if item.Op == "copy" {
					err = copyFileOrDir(item.Path, dstPath)
				} else {
					err = moveFileOrDir(item.Path, dstPath)
				}
				if err != nil {
					errors = append(errors, err)
				}
			}

			fyne.Do(func() {
				if len(errors) > 0 {
					dialog.ShowError(fmt.Errorf("completed with errors: %v", errors), w)
				} else {
					setInfo("Paste completed successfully.")
					if clipboardClearOnSuccess {
						clipboardMu.Lock()
						globalClipboard = nil
						clipboardMu.Unlock()
					}
				}
				state.refresh(w)
			})
		}()
	}
}

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
			ext := strings.ToLower(filepath.Ext(state.archivePath))
			if ext == ".gz" || ext == ".bz2" || ext == ".xz" {
				dialog.ShowError(fmt.Errorf("%s archives do not support item deletion", ext), w)
				return
			}

			onSuccess := func() {
				fyne.Do(func() {
					state.refresh(w)
					setInfo("Successfully deleted from archive.")
				})
			}
			var relPaths []string
			for _, t := range targets {
				relPaths = append(relPaths, filepath.Join(state.archiveRelPath, t))
			}
			deleteFromArchive(state.archivePath, relPaths, w, onSuccess)
		} else {
			go func() {
				setInfo("Deleting items...")
				var errors []error
				for _, t := range targets {
					fullPath := filepath.Join(state.currentPath, t)
					err := os.RemoveAll(fullPath)
					if err != nil {
						errors = append(errors, err)
					}
				}

				fyne.Do(func() {
					if len(errors) > 0 {
						dialog.ShowError(fmt.Errorf("delete failed for some items: %v", errors), w)
					} else {
						setInfo("Deletion completed.")
					}
					state.refresh(w)
				})
			}()
		}
	}, w)
}

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

	compressSrcEntry.SetText(strings.Join(targets, "\n"))
	tabs.Select(CompressTabRank)
	setInfo("Selected files loaded into Compress panel.")
}

func handleContextExtract(state *explorerTabState, w fyne.Window) {
	if state.isArchive {
		var targets []string
		for name, selected := range state.selectedItems {
			if selected {
				targets = append(targets, filepath.Join(state.archiveRelPath, name))
			}
		}

		if len(targets) == 0 {
			dialog.ShowInformation("No Selection", "Please select items within the archive to extract.", w)
			return
		}

		go func() {
			folder, err := zenity.SelectFile(
				zenity.Title("Select Destination Directory"),
				zenity.Directory(),
			)
			if err != nil || folder == "" {
				return
			}

			args := []string{"x", state.archivePath, "-o" + folder, "-bsp1", "-y"}
			args = append(args, targets...)

			fyne.Do(func() {
				tabs.Select(StatusTabRank)
				startOperation(args, "Extracting", "", w, func() {
					setInfo("Selected items extracted successfully.")
				})
			})
		}()
	} else {
		var targetArchives []string
		for name, selected := range state.selectedItems {
			if selected {
				ext := strings.ToLower(filepath.Ext(name))
				isArch := ext == ".7z" || ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".bz2" || ext == ".xz" || ext == ".wim" || ext == ".rar"
				if isArch {
					targetArchives = append(targetArchives, filepath.Join(state.currentPath, name))
				}
			}
		}

		if len(targetArchives) == 0 {
			dialog.ShowInformation("No Archive Selected", "Please select an archive file on disk to extract.", w)
			return
		}

		extractSrcEntry.SetText(targetArchives[0])
		destPath := filepath.Dir(targetArchives[0])
		baseName := strings.TrimSuffix(filepath.Base(targetArchives[0]), filepath.Ext(targetArchives[0]))
		extractDestEntry.SetText(filepath.Join(destPath, baseName))

		tabs.Select(ExtractTabRank)
		setInfo("Archive loaded into Extract panel.")
	}
}

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
		fullPath = state.archivePath + " :: " + filepath.Join(state.archiveRelPath, target)
	}

	w.Clipboard().SetContent(fullPath)
	setInfo("Selected path copied to clipboard.")
}

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

	checksumFileEntry.SetText(filepath.Join(state.currentPath, target))
	tabs.Select(ChecksumTabRank)
	setInfo("Selected file loaded into Checksum panel.")
}

func addFilesToArchive(archivePath string, relPath string, items []clipboardItem, w fyne.Window, isSolid bool, onSuccess func()) {
	tempDir, err := os.MkdirTemp("", "7gl-stage-*")
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	targetDir := tempDir
	if relPath != "" {
		targetDir = filepath.Join(tempDir, relPath)
		err = os.MkdirAll(targetDir, 0755)
		if err != nil {
			os.RemoveAll(tempDir)
			dialog.ShowError(err, w)
			return
		}
	}

	for _, item := range items {
		destName := filepath.Base(item.Path)
		destPath := filepath.Join(targetDir, destName)
		err = os.Symlink(item.Path, destPath)
		if err != nil {
			err = copyFileOrDir(item.Path, destPath)
			if err != nil {
				os.RemoveAll(tempDir)
				dialog.ShowError(err, w)
				return
			}
		}
	}

	var args []string
	if relPath != "" {
		parts := strings.Split(relPath, "/")
		topFolder := parts[0]
		args = []string{"a", archivePath, topFolder}
	} else {
		args = []string{"a", archivePath}
		for _, item := range items {
			args = append(args, filepath.Base(item.Path))
		}
	}

	cleanupAndRun := func() {
		tabs.Select(StatusTabRank)
		startOperation(args, "Adding to Archive", tempDir, w, func() {
			os.RemoveAll(tempDir)
			if onSuccess != nil {
				onSuccess()
			}
		})
	}

	if isSolid {
		dialog.ShowConfirm(
			"Modify Solid Archive?",
			"Modifying a solid archive: this operation may take longer as 7-Zip must decompress and re-compress solid blocks. Proceed?",
			func(confirmed bool) {
				if confirmed {
					cleanupAndRun()
				} else {
					os.RemoveAll(tempDir)
				}
			},
			w,
		)
	} else {
		cleanupAndRun()
	}
}

func deleteFromArchive(archivePath string, relPaths []string, w fyne.Window, onSuccess func()) {
	args := []string{"d", archivePath}
	args = append(args, relPaths...)
	tabs.Select(StatusTabRank)
	startOperation(args, "Deleting from Archive", "", w, onSuccess)
}

func copyFileOrDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	err = os.MkdirAll(dst, info.Mode())
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if err := copyFileOrDir(s, d); err != nil {
			return err
		}
	}
	return nil
}

func moveFileOrDir(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	err = copyFileOrDir(src, dst)
	if err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func showClipboardDialog(w fyne.Window) {
	clipboardMu.Lock()
	// Create a local copy to avoid holding the global clipboard lock during UI initialization and rendering
	localItems := make([]clipboardItem, len(globalClipboard))
	copy(localItems, globalClipboard)
	clipboardMu.Unlock()

	var list *widget.List
	list = widget.NewList(
		func() int {
			return len(localItems)
		},
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

			opLbl.SetText(strings.ToUpper(item.Op))
			if item.Op == "cut" {
				opLbl.Importance = widget.DangerImportance
			} else {
				opLbl.Importance = widget.SuccessImportance
			}

			if item.IsDir {
				typeLbl.SetText("[Dir]")
			} else {
				typeLbl.SetText("[File]")
			}

			pathLbl.SetText(truncateDisplayPath(item.Path, 50))

			delBtn.OnTapped = func() {
				clipboardMu.Lock()
				if id < len(localItems) {
					localItems = append(localItems[:id], localItems[id+1:]...)
				}
				globalClipboard = make([]clipboardItem, len(localItems))
				copy(globalClipboard, localItems)
				clipboardMu.Unlock()
				list.Refresh()
			}
		},
	)

	clearBtn := widget.NewButton("Clear All", func() {
		clipboardMu.Lock()
		localItems = nil
		globalClipboard = nil
		clipboardMu.Unlock()
		list.Refresh()
	})
	clearBtn.Importance = widget.DangerImportance

	clearOnSuccessCheck := widget.NewCheck("Clear clipboard on successful paste", func(checked bool) {
		clipboardClearOnSuccess = checked
	})
	clearOnSuccessCheck.SetChecked(clipboardClearOnSuccess)

	content := container.NewBorder(
		container.NewVBox(clearOnSuccessCheck, widget.NewSeparator()),
		container.NewHBox(layout.NewSpacer(), clearBtn),
		nil,
		nil,
		list,
	)

	d := dialog.NewCustom("Custom Clipboard", "Close", content, w)
	d.Resize(fyne.NewSize(600, 400))
	d.Show()
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
