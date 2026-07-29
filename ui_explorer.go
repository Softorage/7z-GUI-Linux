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
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// conflictAction enumerates user choices when resolving destination file/folder collisions
// during copy, cut, or extract operations.
type conflictAction int

const (
	actionCancel conflictAction = iota
	actionReplace
	actionReplaceAll
	actionRename
	actionRenameAll
	actionSkip
	actionSkipAll
)

// stageItem represents an entry prepared for staging before committing changes into an archive.
type stageItem struct {
	SrcPath string // Absolute path on disk or temp location
	DstName string // Target file or folder name inside the archive
	IsDir   bool
}

// fileSystemItem represents a normalized view of a file, directory, or symlink on disk.
type fileSystemItem struct {
	Name      string
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
	Modified  string
}

// archiveItem represents raw entry metadata returned by 7-Zip's list output (-slt).
type archiveItem struct {
	Path     string // Relative path within the archive (using forward slashes)
	IsDir    bool
	Size     int64
	Modified string
}

// archiveLevel represents a single layer in the nested archive navigation stack.
// When an archive inside an archive is opened, a temporary working directory is created
// and pushed onto the tab's navigation stack.
type archiveLevel struct {
	displayName     string
	archivePath     string // Local path to the extracted target archive
	archiveRelPath  string // Current directory level within this archive
	archivePassword string
	tempDir         string // Temporary directory managing extracted files for this level
}

// explorerTabState holds the full runtime state for an individual tab in the File Explorer.
type explorerTabState struct {
	currentPath     string
	isArchive       bool
	archivePath     string
	archiveRelPath  string
	archivePassword string
	archiveStack    []archiveLevel   // Navigation stack supporting arbitrary archive-in-archive depth
	archiveItems    []archiveItem    // Full raw archive entry cache
	items           []fileSystemItem // Display items currently visible in the active folder level
	selectedItems   map[string]bool  // Set of item names marked via checkboxes
	showHidden      bool

	badgeLabel *widget.Label
	pathEntry  *widget.Entry
	fileList   *widget.List
	tabItem    *container.TabItem
	cutBtn     *widget.Button
}

// clipboardItem holds details about items copied or cut in the app's custom clipboard,
// supporting both local files and items situated inside virtual archives.
type clipboardItem struct {
	Path        string // Full disk path or internal virtual relative path
	IsDir       bool
	Op          string // "cut" or "copy"
	IsArchive   bool   // Whether the source item is inside an archive
	ArchivePath string // Source archive disk path (if IsArchive is true)
	Password    string // Archive password (if IsArchive is true and encrypted)
}

// favoriteItem represents a user bookmark pointing to a local directory path.
type favoriteItem struct {
	Name string
	Path string
}

var (
	cutOperation  = "cut   " // TODO: no hacky solution
	copyOperation = "copy"

	docTabs             *container.DocTabs
	explorerTabsState   = make(map[*container.TabItem]*explorerTabState)
	explorerTabsStateMu sync.RWMutex // Protects parallel access to tab states across goroutines

	globalClipboard         []clipboardItem
	clipboardClearOnSuccess = true
	clipboardMu             sync.Mutex

	favorites   []favoriteItem
	favoritesMu sync.Mutex
	favList     *widget.List
)

// Helpers for 7-Zip Core Execution Engines

// isTarballExtension checks if a filename uses a double-compression TAR extension.
// 7-Zip treats tarballs (.tar.gz, .tgz, etc.) as two distinct archive layers.
func isTarballExtension(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".tar.xz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tbz2") ||
		strings.HasSuffix(lower, ".tbz") ||
		strings.HasSuffix(lower, ".txz")
}

// extractArchive is the centralized extraction function for any archive format.
// For double-compressed tarballs (e.g., .tar.gz), it pipelines two 7-Zip subprocesses in memory
// using an io.Pipe: uncompressing the outer wrapper to stdout, and streaming it directly into the inner tar extractor.
// Attaches an empty Reader to Stdin to prevent processes from hanging if an archive requires input.
func extractArchive(archivePath, destDir, password string, targets ...string) error {
	if isTarballExtension(archivePath) {
		// Decompress outer stream (e.g., gzip, bzip2, xz) to stdout (-so)
		args1 := []string{"x", archivePath, "-so", "-bso0", "-bsp0"}
		if password != "" {
			args1 = append(args1, "-p"+password)
		}
		cmd1 := exec.Command(root7zCmd, args1...)
		cmd1.Stdin = strings.NewReader("")

		// Decompress TAR stream read from stdin (-si) into destDir
		args2 := append([]string{"x", "-si", "-ttar", "-o" + destDir, "-y"}, targets...)
		cmd2 := exec.Command(root7zCmd, args2...)

		// io.Pipe connects writer (cmd1.Stdout) to reader (cmd2.Stdin) in memory without disk I/O
		pr, pw := io.Pipe()
		cmd1.Stdout = pw
		cmd2.Stdin = pr

		if err := cmd1.Start(); err != nil {
			pr.Close()
			pw.Close()
			return err
		}
		if err := cmd2.Start(); err != nil {
			pr.Close()
			pw.Close()
			return err
		}

		// Asynchronously wait for outer process completion and close pipe writer to send EOF to cmd2
		go func() {
			_ = cmd1.Wait()
			_ = pw.Close()
		}()

		err := cmd2.Wait()
		_ = pr.Close()
		return err
	}

	// Standard single-stage extraction for standard formats (.zip, .7z, .rar, etc.)
	args := append([]string{"x", archivePath, "-o" + destDir, "-y"}, targets...)
	if password != "" {
		args = append(args, "-p"+password)
	}
	cmd := exec.Command(root7zCmd, args...)
	cmd.Stdin = strings.NewReader("")
	return cmd.Run()
}

// listArchive retrieves detailed metadata from an archive using 7-Zip's SLT flag `-slt`.
// Returns error explicitly if tarball pipeline decompression fails instead of falling back to outer Gzip header.
func listArchive(archivePath, password string) ([]archiveItem, bool, error) {
	var out []byte
	var err error

	if isTarballExtension(archivePath) {
		args1 := []string{"x", archivePath, "-so", "-bso0", "-bsp0"}
		if password != "" {
			args1 = append(args1, "-p"+password)
		}
		cmd1 := exec.Command(root7zCmd, args1...)
		cmd1.Stdin = strings.NewReader("")
		cmd2 := exec.Command(root7zCmd, "l", "-si", "-ttar", "-slt")

		pr, pw := io.Pipe()
		cmd1.Stdout = pw
		cmd2.Stdin = pr

		var outBuf strings.Builder
		cmd2.Stdout = &outBuf

		if err1 := cmd1.Start(); err1 == nil && cmd2.Start() == nil {
			go func() {
				_ = cmd1.Wait()
				_ = pw.Close()
			}()
			err = cmd2.Wait()
			_ = pr.Close()
			if err != nil {
				return nil, false, fmt.Errorf("failed to list tarball archive contents: %w", err)
			}
			return parseSLTOutput(outBuf.String(), archivePath)
		} else {
			_ = pr.Close()
			_ = pw.Close()
			return nil, false, fmt.Errorf("failed to start tarball decompression pipeline")
		}
	}

	args := []string{"l", "-slt", archivePath}
	if password != "" {
		args = append(args, "-p"+password)
	}
	cmd := exec.Command(root7zCmd, args...)
	cmd.Stdin = strings.NewReader("")
	out, err = cmd.Output()
	if err != nil {
		return nil, false, err
	}

	return parseSLTOutput(string(out), archivePath)
}

// parseSLTOutput parses key-value structured text emitted by `7z l -slt`.
// Preserves deliberate leading/trailing whitespace in property values (e.g. filenames).
func parseSLTOutput(outStr, archivePath string) ([]archiveItem, bool, error) {
	lines := strings.Split(outStr, "\n")
	var items []archiveItem
	var currentItem *archiveItem
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
					currentItem = &archiveItem{}
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
	var filtered []archiveItem
	for _, it := range items {
		if it.Path != "" && it.Path != filepath.Base(archivePath) {
			filtered = append(filtered, it)
		}
	}

	return filtered, isSolid, nil
}

// Helper for Memory & System Storage

// getAvailableRAMBytes reads Linux `/proc/meminfo` to calculate available system RAM.
func getAvailableRAMBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 2 * 1024 * 1024 * 1024 // 2GB fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if val, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return val * 1024 // KiB to bytes
				}
			}
		}
	}
	return 2 * 1024 * 1024 * 1024
}

// isTmpfs executes statfs syscall to verify if target path resides in RAM (tmpfs magic number 0x01021994).
func isTmpfs(path string) bool {
	var stat syscall.Statfs_t
	return syscall.Statfs(path, &stat) == nil && uint64(stat.Type) == 0x01021994
}

// selectTempStorage decides whether to stage uncompressed files in RAM (tmpfs) or disk storage.
// Dynamically scales RAM budget up to 49% of available memory (capped at 8GB for high-spec workstations).
func selectTempStorage(requiredBytes uint64) (string, bool) {
	ramBudget := getAvailableRAMBytes() * 49 / 100
	maxRAMBudget := uint64(8 * 1024 * 1024 * 1024)
	if ramBudget > maxRAMBudget {
		ramBudget = maxRAMBudget
	}

	// RAM (tmpfs) if requiredBytes fits within budget
	if requiredBytes <= ramBudget {
		if isTmpfs("/tmp") {
			if dir, err := os.MkdirTemp("/tmp", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}
		if isTmpfs("/dev/shm") {
			if dir, err := os.MkdirTemp("/dev/shm", "7gl-ram-*"); err == nil {
				return dir, true
			}
		}
	}

	// Fallback to disk cache directory
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	nestedCache := filepath.Join(cacheDir, "7-zip-gui", "nested")
	_ = os.MkdirAll(nestedCache, 0755)

	dir, err := os.MkdirTemp(nestedCache, "7gl-disk-*")
	if err != nil {
		dir, _ = os.MkdirTemp("", "7gl-disk-*")
	}
	return dir, false
}

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
		parts = append(parts, lvl.displayName)
	}
	rel := state.archiveRelPath
	if rel == "" {
		rel = "/"
	}
	return strings.Join(parts, " :: ") + " :: " + rel
}

// isTempDirPinned returns true if any item currently in the global clipboard
// resides within the given temporary directory path.
func isTempDirPinned(tempDir string) bool {
	if tempDir == "" {
		return false
	}
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	for _, cb := range globalClipboard {
		if cb.IsArchive && strings.HasPrefix(cb.ArchivePath, tempDir) {
			return true
		}
	}
	return false
}

// cleanupTempLevel deletes the level's temporary directory unless it is pinned in the clipboard.
func cleanupTempLevel(lvl archiveLevel) {
	if lvl.tempDir != "" && !isTempDirPinned(lvl.tempDir) {
		_ = os.RemoveAll(lvl.tempDir)
	}
}

// cleanupTemp removes temporary working folders created during nested archive exploration,
// provided the paths aren't currently pinned in the global clipboard.
func (state *explorerTabState) cleanupTemp() {
	var remainingStack []archiveLevel
	for _, lvl := range state.archiveStack {
		if isTempDirPinned(lvl.tempDir) {
			remainingStack = append(remainingStack, lvl)
		} else if lvl.tempDir != "" {
			_ = os.RemoveAll(lvl.tempDir)
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

		args := []string{"a", parent.archivePath, child.archivePath}
		if parent.archivePassword != "" {
			args = append(args, "-p"+parent.archivePassword)
		}
		cmd := exec.Command(root7zCmd, args...)
		cmd.Stdin = strings.NewReader("")
		_ = cmd.Run()
	}
}

// Main UI Construction

// buildExplorerTab creates and sets up the primary Explorer tab view, including sidebar favorites and dynamic tabs.
func buildExplorerTab(w fyne.Window) fyne.CanvasObject {
	favorites = getInitialFavorites()

	favList = widget.NewList(
		func() int {
			favoritesMu.Lock()
			defer favoritesMu.Unlock()
			return len(favorites)
		},
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			favoritesMu.Lock()
			defer favoritesMu.Unlock()
			if id < len(favorites) {
				o.(*widget.Label).SetText(favorites[id].Name)
			}
		},
	)

	var selectedFavIndex = -1

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

	favList.OnUnselected = func(id widget.ListItemID) { selectedFavIndex = -1 }

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
				favorites = append(favorites, favoriteItem{Name: name, Path: dir})
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
			favoritesMu.Lock()
			if selectedFavIndex >= 0 && selectedFavIndex < len(favorites) {
				favorites[selectedFavIndex].Name = newName
			}
			favoritesMu.Unlock()
			updateFavoritesList()
		}, w)
		d.Resize(fyne.NewSize(450, 180))
		d.Show()
	})
	renameFavBtn.Importance = widget.LowImportance

	favSidebar := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Favorites", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(addFavBtn, removeFavBtn, renameFavBtn)),
		nil, nil, favList,
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

	return container.NewPadded(container.NewBorder(
		container.NewVBox(widget.NewRichTextFromMarkdown("## Explorer"), widget.NewSeparator()),
		nil, nil, nil, split,
	))
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
		setInfo("Current path copied to clipboard.")
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

			name.SetText(truncateDisplayPath(item.Name, 40))

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
					size.SetText(formatSize(item.Size))
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
						state.archiveStack[len(state.archiveStack)-1].archiveRelPath = state.archiveRelPath
					}
				} else {
					state.currentPath = item.Path
				}
				state.refresh(w)
			} else if isArchiveExtension(item.Name) {
				openArchiveLevel(w, state, item)
			}
		}
		lastClickedName = item.Name
		lastClickedTime = now
	}

	state.fileList = list

	// We disable cutBtn when user is in nested archive
	state.cutBtn = widget.NewButtonWithIcon("", theme.ContentCutIcon(), func() { addToClipboard(state, cutOperation) })
	state.cutBtn.Importance = widget.LowImportance

	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() { addToClipboard(state, copyOperation) })
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
			setInfo("Showing hidden files.")
		} else {
			showHiddenFilesBtn.SetIcon(theme.VisibilityOffIcon())
			setInfo("Hiding hidden files.")
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
func openArchiveLevel(w fyne.Window, state *explorerTabState, item fileSystemItem) {
	targetPath := item.Path

	if state.isArchive {
		// Asynchronously extract nested archive inside existing virtual view
		go func() {
			uncompressedSize := uint64(item.Size)
			sizeMB := float64(uncompressedSize) / (1024 * 1024)

			if uncompressedSize > 100*1024*1024 {
				setInfo(fmt.Sprintf("Decompressing nested archive %s (%.1f MB)...", item.Name, sizeMB))
			} else {
				setInfo(fmt.Sprintf("Opening nested archive %s...", item.Name))
			}

			// Allocate temp workspace (RAM tmpfs or disk cache)
			tempDir, isRAM := selectTempStorage(uncompressedSize)
			if isRAM {
				setInfo(fmt.Sprintf("Extracting %s to RAM (tmpfs)...", item.Name))
			}

			if err := extractArchive(state.archivePath, tempDir, state.archivePassword, item.Path); err != nil {
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

			protected := isPasswordProtected(extractedPath)
			openNested := func(pwd string) {
				lvl := archiveLevel{
					displayName:     item.Name,
					archivePath:     extractedPath,
					archiveRelPath:  "",
					archivePassword: pwd,
					tempDir:         tempDir,
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
					promptArchivePassword(w, extractedPath, "Open", openNested, func() {
						os.RemoveAll(tempDir)
						setInfo("Opening password-protected archive cancelled.")
					})
				})
			} else {
				fyne.Do(func() { openNested("") })
			}
		}()
	} else {
		// Opening archive directly from local file system
		go func() {
			protected := isPasswordProtected(targetPath)
			openRoot := func(pwd string) {
				lvl := archiveLevel{
					displayName:     filepath.Base(targetPath),
					archivePath:     targetPath,
					archiveRelPath:  "",
					archivePassword: pwd,
				}
				state.archiveStack = []archiveLevel{lvl}
				state.isArchive = true
				state.archivePath = targetPath
				state.archiveRelPath = ""
				state.archivePassword = pwd
				state.refresh(w)
			}

			if protected {
				fyne.Do(func() {
					promptArchivePassword(w, targetPath, "Open", openRoot, func() {
						setInfo("Opening password-protected archive cancelled.")
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
			all, _, err := listArchive(state.archivePath, state.archivePassword)
			if err != nil {
				fyne.Do(func() { dialog.ShowError(fmt.Errorf("failed to list archive contents: %v", err), w) })
				return
			}

			state.archiveItems = all
			virtualItems := getVirtualItems(all, state.archiveRelPath)

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
					tabTitle = state.archiveStack[len(state.archiveStack)-1].displayName
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
			localItems, err := getLocalItems(state.currentPath, state.showHidden)
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
				state.archiveStack[len(state.archiveStack)-1].archiveRelPath = state.archiveRelPath
			}
		} else {
			if len(state.archiveStack) > 1 {
				// Pop nested archive level off navigation stack
				top := state.archiveStack[len(state.archiveStack)-1]
				state.archiveStack = state.archiveStack[:len(state.archiveStack)-1]

				// Clean up top level temp dir if unpinned
				cleanupTempLevel(top)

				prev := state.archiveStack[len(state.archiveStack)-1]
				state.archivePath = prev.archivePath
				state.archiveRelPath = prev.archiveRelPath
				state.archivePassword = prev.archivePassword
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

// The list widget's callbacks (Length and Update) already handle their own locking
func updateFavoritesList() { favList.Refresh() }

// getInitialFavorites discovers initial bookmarks in the user's home directory.
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
			dirs = append(dirs, favoriteItem{Name: e.Name(), Path: filepath.Join(home, e.Name())})
		}
		if len(dirs) >= 5 {
			break
		}
	}
	return dirs
}

// getLocalItems reads directory entries on disk, sorts folders before files, and evaluates symlinks.
func getLocalItems(dirPath string, showHidden bool) ([]fileSystemItem, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	var dirs, files []fileSystemItem

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

		isSymlink := entry.Type()&os.ModeSymlink != 0
		isDir := entry.IsDir()
		if !isDir && isSymlink {
			// Resolve symlink target type
			if targetInfo, err := os.Stat(fullPath); err == nil {
				isDir = targetInfo.IsDir()
			}
		}

		item := fileSystemItem{
			Name:      name,
			Path:      fullPath,
			IsDir:     isDir,
			IsSymlink: isSymlink,
			Size:      size,
			Modified:  modified,
		}

		if isDir {
			dirs = append(dirs, item)
		} else {
			files = append(files, item)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })

	return append(dirs, files...), nil
}

// getVirtualItems maps raw flat archive entry paths into a virtual folder hierarchy corresponding to currentRelPath level.
func getVirtualItems(all []archiveItem, currentRelPath string) []fileSystemItem {
	prefix := ""
	if currentRelPath != "" {
		prefix = currentRelPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}

	type tempItem struct {
		isDir    bool
		size     int64
		modified string
		path     string
	}

	merged := make(map[string]tempItem)

	for _, item := range all {
		path := item.Path
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			continue
		}

		rel := strings.TrimPrefix(path, prefix)
		if rel == "" {
			continue
		}

		isDir := item.IsDir || strings.HasSuffix(path, "/")
		trimmedRel := strings.TrimSuffix(rel, "/")
		if trimmedRel == "" {
			continue
		}

		parts := strings.Split(trimmedRel, "/")
		name := parts[0]
		if name == "" {
			continue
		}

		if len(parts) > 1 {
			// Sub-path element implies directory container at current level
			existing, exists := merged[name]
			if !exists || !existing.isDir {
				merged[name] = tempItem{isDir: true, size: 0, modified: item.Modified, path: prefix + name}
			}
		} else {
			if isDir {
				merged[name] = tempItem{isDir: true, size: 0, modified: item.Modified, path: prefix + name}
			} else {
				if _, exists := merged[name]; !exists {
					merged[name] = tempItem{isDir: false, size: item.Size, modified: item.Modified, path: item.Path}
				}
			}
		}
	}

	var dirs, files []fileSystemItem
	for name, info := range merged {
		fItem := fileSystemItem{
			Name:     name,
			Path:     info.path,
			IsDir:    info.isDir,
			Size:     info.size,
			Modified: info.modified,
		}
		if info.isDir {
			dirs = append(dirs, fItem)
		} else {
			files = append(files, fItem)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })

	return append(dirs, files...)
}

// extractArchiveItems extracts clipboard entries situated inside virtual archives into temporary disk staging locations.
// Returns a mapping of original virtual paths to their temporary local disk paths, the temporary directory path itself, and any error encountered.
func extractArchiveItems(items []clipboardItem) (map[string]string, string, error) {
	hasArchiveItems := false
	for _, item := range items {
		if item.IsArchive {
			hasArchiveItems = true
			break
		}
	}
	if !hasArchiveItems {
		return nil, "", nil
	}

	tempDir, err := os.MkdirTemp("", "7gl-extract-*")
	if err != nil {
		return nil, "", err
	}

	// Group items by archive path and record passwords so we can batch extract with a single 7-Zip process per archive
	passwords := make(map[string]string)
	groups := make(map[string][]string)
	for _, item := range items {
		if item.IsArchive {
			groups[item.ArchivePath] = append(groups[item.ArchivePath], item.Path)
			if item.Password != "" {
				passwords[item.ArchivePath] = item.Password
			}
		}
	}

	for archivePath, paths := range groups {
		if err := extractArchive(archivePath, tempDir, passwords[archivePath], paths...); err != nil {
			os.RemoveAll(tempDir)
			return nil, "", fmt.Errorf("failed to extract from archive %s: %w", filepath.Base(archivePath), err)
		}
	}

	pathMap := make(map[string]string)
	for _, item := range items {
		if item.IsArchive {
			pathMap[item.Path] = filepath.Join(tempDir, filepath.FromSlash(item.Path))
		}
	}

	return pathMap, tempDir, nil
}

// addToClipboard adds currently selected explorer items to the thread-safe app clipboard.
func addToClipboard(state *explorerTabState, op string) {
	if state.isArchive && len(state.archiveStack) > 1 && op == cutOperation {
		setInfo("Cut operation is disabled in nested archives.")
		return
	}

	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	hasSelection := false
	addedCount, updatedCount := 0, 0
	var lastUpdatedFrom, lastUpdatedTo, lastUpdatedPath string

	for name, selected := range state.selectedItems {
		if !selected {
			continue
		}
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
			if cbItem.Path == item.Path && cbItem.IsArchive == state.isArchive && cbItem.ArchivePath == state.archivePath {
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
	if state.fileList != nil {
		state.fileList.UnselectAll()
		state.fileList.Refresh()
	}
}

type typeConflictInfo struct {
	Name       string
	SrcPath    string
	DstPath    string
	Resolution string
}

type pasteContext struct {
	window          fyne.Window
	hasGlobalAction bool
	globalAction    conflictAction
	usedNames       map[string]bool
	cancelled       bool
	typeConflicts   []typeConflictInfo
	typeConflictsMu sync.Mutex
}

// resolveConflict prompts the user or applies an established batch action (e.g. Skip All, Replace All).
func resolveConflict(ctx *pasteContext, filename string) conflictAction {
	if ctx.cancelled {
		return actionCancel
	}
	if ctx.hasGlobalAction {
		return ctx.globalAction
	}

	action := promptConflict(ctx.window, filename)
	if action == actionCancel {
		ctx.cancelled = true
	} else if action == actionReplaceAll {
		ctx.globalAction = actionReplace
		ctx.hasGlobalAction = true
		return actionReplace
	} else if action == actionSkipAll {
		ctx.globalAction = actionSkip
		ctx.hasGlobalAction = true
		return actionSkip
	} else if action == actionRenameAll {
		ctx.globalAction = actionRename
		ctx.hasGlobalAction = true
		return actionRename
	}
	return action
}

// handlePaste executes paste operation into current local directory or virtual archive target.
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
		if isSingleFileArchive(state.archivePath) {
			dialog.ShowError(fmt.Errorf("%s archives do not support adding multiple items or folders", filepath.Ext(state.archivePath)), w)
			return
		}

		_, isSolid, err := listArchive(state.archivePath, state.archivePassword)
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

		addFilesToArchive(state.archivePath, state.archiveRelPath, state.archivePassword, itemsCopy, w, isSolid, onSuccess)
	} else {
		go func() {
			setInfo("Pasting items...")

			// Extract virtual archive items to a temporary location first
			pathMap, tempDir, err := extractArchiveItems(itemsCopy)
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
						setInfo("Paste operation cancelled by user.")
						break
					}
					errors = append(errors, err)
					continue
				}

				if item.Op == cutOperation {
					if item.IsArchive {
						// For a cutOperation from inside an archive, delete it from the source archive
						cmd := exec.Command(root7zCmd, "d", item.ArchivePath, item.Path)
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
					setInfo("Paste operation stopped.")
				} else {
					setInfo("Paste completed successfully.")
					if clipboardClearOnSuccess {
						clipboardMu.Lock()
						globalClipboard = nil
						clipboardMu.Unlock()
					}
				}

				// Inform the user if any type conflicts were handled during the run
				if len(ctx.typeConflicts) > 0 {
					var sb strings.Builder
					for _, conflict := range ctx.typeConflicts {
						sb.WriteString(fmt.Sprintf("Name: %s, Src: %s, Dst: %s, Resolution: %v\n", conflict.Name, conflict.SrcPath, conflict.DstPath, conflict.Resolution))
					}
					setInfo("Type Conflict: \n" + sb.String()) // TODO: simply log instead of setInfo feedback
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
			if isSingleFileArchive(state.archivePath) {
				dialog.ShowError(fmt.Errorf("%s archives do not support item deletion", filepath.Ext(state.archivePath)), w)
				return
			}

			onSuccess := func() {
				var deletedPaths []string
				for _, t := range targets {
					deletedPaths = append(deletedPaths, filepath.Join(state.archiveRelPath, t))
				}
				removeFromClipboard(deletedPaths, true)

				fyne.Do(func() {
					state.refresh(w)
					setInfo("Successfully deleted from archive.")
				})
			}
			var relPaths []string
			for _, t := range targets {
				relPaths = append(relPaths, filepath.Join(state.archiveRelPath, t))
			}
			deleteFromArchive(state.archivePath, relPaths, state.archivePassword, w, onSuccess)
		} else {
			go func() {
				setInfo("Deleting items...")
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
					removeFromClipboard(deletedPaths, false)
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

	compressSrcEntry.SetText(strings.Join(targets, "\n"))
	tabs.Select(CompressTabRank)
	setInfo("Selected files loaded into Compress panel.")
}

// handleContextExtract shows helpful dialog for virtual archive selection and sends local archives to Extract panel.
func handleContextExtract(state *explorerTabState, w fyne.Window) {
	if state.isArchive {
		dialog.ShowInformation("Browsing Archive", "You are currently browsing through an archive. To extract from an archive, simply copy it and paste to the destination (which can also be another archive).", w)
		return
	} else {
		var targetArchives []string
		for name, selected := range state.selectedItems {
			if selected && isArchiveExtension(name) {
				targetArchives = append(targetArchives, filepath.Join(state.currentPath, name))
			}
		}

		if len(targetArchives) == 0 {
			dialog.ShowInformation("No Archive Selected", "Please select an archive file on disk to extract.", w)
			return
		}

		extractSrcEntry.SetText(strings.Join(targetArchives, "\n"))
		tabs.Select(ExtractTabRank)
		setInfo("Selected archives loaded into Extract panel.")
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
	setInfo("Selected path copied to clipboard.")
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

	checksumFileEntry.SetText(filepath.Join(state.currentPath, target))
	tabs.Select(ChecksumTabRank)
	setInfo("Selected file loaded into Checksum panel.")
}

// addFilesToArchive stages items in temporary directory and runs `7z a -snh` to store physical files.
func addFilesToArchive(archivePath, relPath, password string, items []clipboardItem, w fyne.Window, isSolid bool, onSuccess func()) {
	allEntries, _, err := listArchive(archivePath, password)
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
		pathMap, extractDir, err := extractArchiveItems(items)
		if err != nil {
			fyne.Do(func() { dialog.ShowError(err, w) })
			return
		}

		var itemsToStage []stageItem
		globalAction := actionCancel
		hasGlobalAction := false

		for _, item := range items {
			srcPath := item.Path
			if item.IsArchive {
				srcPath = pathMap[item.Path]
			}

			baseName := filepath.Base(item.Path)
			archiveDstRelPath := filepath.Join(relPath, baseName)
			archiveDstClean := filepath.Clean(archiveDstRelPath)

			var action conflictAction
			if existingPaths[archiveDstClean] {
				if hasGlobalAction {
					action = globalAction
				} else {
					action = promptConflict(w, baseName)
					if action == actionReplaceAll {
						globalAction = actionReplace
						hasGlobalAction = true
						action = actionReplace
					} else if action == actionSkipAll {
						globalAction = actionSkip
						hasGlobalAction = true
						action = actionSkip
					} else if action == actionCancel {
						if extractDir != "" {
							os.RemoveAll(extractDir)
						}
						return
					}
				}
			} else {
				action = actionReplace
			}

			if action == actionSkip {
				continue
			}

			dstName := baseName
			if action == actionRename {
				dstName = getUniqueArchiveDstPath(baseName, relPath, existingPaths)
				existingPaths[filepath.Clean(filepath.Join(relPath, dstName))] = true
			}

			itemsToStage = append(itemsToStage, stageItem{SrcPath: srcPath, DstName: dstName, IsDir: item.IsDir})
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
				startOperation(args, "Adding to Archive", tempDir, w, func() {
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
	startOperation(args, "Deleting from Archive", "", w, func() {
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
		var action conflictAction

		if isTypeConflict {
			// Safe global actions (Rename All or Skip All) require no user interaction and can be automatically applied.
			// Destructive global actions (Replace All) must be ignored, forcing the explicit warnings in promptTypeConflict.
			if ctx.hasGlobalAction && (ctx.globalAction == actionRename || ctx.globalAction == actionSkip) {
				action = ctx.globalAction
			} else {
				action = promptTypeConflict(ctx.window, filepath.Base(dst), info.IsDir(), dstInfo.IsDir())
				if action == actionRenameAll {
					ctx.globalAction = actionRename
					ctx.hasGlobalAction = true
					action = actionRename
				} else if action == actionSkipAll {
					ctx.globalAction = actionSkip
					ctx.hasGlobalAction = true
					action = actionSkip
				}
			}

			// Log how this type mismatch was handled
			resolution := "Cancelled"
			switch action {
			case actionSkip:
				resolution = "Skipped"
			case actionRename:
				tempDst := getUniqueDstPath(dst, ctx.usedNames)
				resolution = fmt.Sprintf("Renamed to '%s'", filepath.Base(tempDst))
			case actionReplace:
				resolution = "Replaced (Existing directory/file deleted)"
			}

			ctx.typeConflictsMu.Lock()
			ctx.typeConflicts = append(ctx.typeConflicts, typeConflictInfo{
				Name:       filepath.Base(dst),
				SrcPath:    src,
				DstPath:    dst,
				Resolution: resolution,
			})
			ctx.typeConflictsMu.Unlock()
		} else {
			action = resolveConflict(ctx, filepath.Base(dst))
		}

		if action == actionSkip {
			return nil
		}
		if action == actionCancel {
			return fmt.Errorf("cancelled by user")
		}
		if action == actionRename {
			dst = getUniqueDstPath(dst, ctx.usedNames)
			ctx.usedNames[filepath.Base(dst)] = true
			dstExists = false
		} else if action == actionReplace {
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

	return copyFile(src, dst)
}

// copyFile copies standard file bytes from src to dst.
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
	clipboardMu.Lock()
	// Create a local copy to avoid holding the global clipboard lock during UI initialization and rendering
	localItems := make([]clipboardItem, len(globalClipboard))
	copy(localItems, globalClipboard)
	clipboardMu.Unlock()

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

			if item.Op == cutOperation {
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
				pathLbl.SetText(fmt.Sprintf("%s :: %s", filepath.Base(item.ArchivePath), truncateDisplayPath(item.Path, 40)))
			} else {
				pathLbl.SetText(truncateDisplayPath(item.Path, 50))
			}

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
		nil, nil, list,
	)

	d := dialog.NewCustom("Custom Clipboard", "Close", content, w)
	d.Resize(fyne.NewSize(600, 400))
	d.Show()
}

// formatSize formats byte values into human-readable strings (B, KB, MB, GB, TB).
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

// promptConflict prompts the user to resolve a file name collision using a buffered response channel.
func promptConflict(w fyne.Window, filename string) conflictAction {
	ch := make(chan conflictAction, 1)
	fyne.Do(func() {
		var d dialog.Dialog
		replaceBtn := widget.NewButton("Replace", func() { ch <- actionReplace; d.Hide() })
		replaceAllBtn := widget.NewButton("Replace All", func() { ch <- actionReplaceAll; d.Hide() })
		renameBtn := widget.NewButton("Rename (Auto)", func() { ch <- actionRename; d.Hide() })
		renameAllBtn := widget.NewButton("Rename All (Auto)", func() { ch <- actionRenameAll; d.Hide() })
		skipBtn := widget.NewButton("Skip", func() { ch <- actionSkip; d.Hide() })
		skipAllBtn := widget.NewButton("Skip All", func() { ch <- actionSkipAll; d.Hide() })

		content := container.NewVBox(
			widget.NewLabel(fmt.Sprintf("An item named '%s' already exists at the destination.", filename)),
			widget.NewLabel("What would you like to do?"),
			widget.NewSeparator(),
			container.NewGridWithColumns(3, replaceBtn, renameBtn, skipBtn, replaceAllBtn, renameAllBtn, skipAllBtn),
		)

		d = dialog.NewCustom("File Conflict", "Cancel", content, w)
		d.SetOnClosed(func() {
			select {
			case ch <- actionCancel:
			default:
			}
		})
		d.Show()
	})
	return <-ch
}

// promptTypeConflict displays a destructive dialog when source and target types collide (file vs folder).
func promptTypeConflict(w fyne.Window, filename string, srcIsDir, dstIsDir bool) conflictAction {
	ch := make(chan conflictAction, 1)
	fyne.Do(func() {
		var d dialog.Dialog

		srcType, dstType := "a file", "a file"
		if srcIsDir {
			srcType = "a directory"
		}
		if dstIsDir {
			dstType = "a directory"
		}

		replaceBtn := widget.NewButton("Replace (Delete Existing)", func() { ch <- actionReplace; d.Hide() })
		replaceBtn.Importance = widget.DangerImportance
		renameBtn := widget.NewButton("Rename (Auto)", func() { ch <- actionRename; d.Hide() })
		renameAllBtn := widget.NewButton("Rename All (Auto)", func() { ch <- actionRenameAll; d.Hide() })
		skipBtn := widget.NewButton("Skip", func() { ch <- actionSkip; d.Hide() })
		skipAllBtn := widget.NewButton("Skip All", func() { ch <- actionSkipAll; d.Hide() })

		content := container.NewVBox(
			widget.NewLabelWithStyle("WARNING: Type Mismatch Conflict!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
			widget.NewLabel(fmt.Sprintf("You are trying to copy %s '%s'.", srcType, filename)),
			widget.NewLabel(fmt.Sprintf("But %s already exists with that name at the destination.", dstType)),
			widget.NewLabel("Replacing it will permanently and recursively DELETE the existing item and all of its contents!"),
			widget.NewSeparator(),
			container.NewGridWithColumns(3, replaceBtn, renameBtn, skipBtn, layout.NewSpacer(), renameAllBtn, skipAllBtn),
		)

		d = dialog.NewCustom("Destructive Type Conflict", "Cancel", content, w)
		d.SetOnClosed(func() {
			select {
			case ch <- actionCancel:
			default:
			}
		})
		d.Show()
	})
	return <-ch
}

// getUniqueDstPath generates auto-incremented non-conflicting local destination path (e.g., file_copy1.txt).
func getUniqueDstPath(path string, usedNames map[string]bool) string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	counter := 1
	newPath := path
	for {
		_, err := os.Lstat(newPath)
		if os.IsNotExist(err) && !usedNames[filepath.Base(newPath)] {
			break
		}
		newPath = filepath.Join(dir, fmt.Sprintf("%s_copy%d%s", name, counter, ext))
		counter++
	}
	return newPath
}

// getUniqueArchiveDstPath generates auto-incremented non-conflicting destination path inside archive.
func getUniqueArchiveDstPath(baseName, relPath string, existingPaths map[string]bool) string {
	ext := filepath.Ext(baseName)
	name := strings.TrimSuffix(baseName, ext)

	counter := 1
	newName := baseName
	for {
		archivePath := filepath.Clean(filepath.Join(relPath, newName))
		if !existingPaths[archivePath] {
			break
		}
		newName = fmt.Sprintf("%s_copy%d%s", name, counter, ext)
		counter++
	}
	return newName
}

// removeFromClipboard removes paths matching deleted target entries from global clipboard state.
func removeFromClipboard(deletedPaths []string, isArchive bool) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()

	var newClipboard []clipboardItem
	for _, cbItem := range globalClipboard {
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
	globalClipboard = newClipboard
}

// promptArchivePassword displays input dialog prompting user for password when entering encrypted archives.
func promptArchivePassword(w fyne.Window, archivePath, confirmLabel string, onSuccess func(string), onCancel func()) {
	if confirmLabel == "" {
		confirmLabel = "OK"
	}
	pwdEntry := widget.NewPasswordEntry()
	pwdEntry.PlaceHolder = "Enter Password"

	d := dialog.NewForm("Password Required for "+filepath.Base(archivePath), confirmLabel, "Cancel", []*widget.FormItem{widget.NewFormItem("Password:", pwdEntry)}, func(submit bool) {
		if submit {
			if onSuccess != nil {
				onSuccess(pwdEntry.Text)
			}
		} else if onCancel != nil {
			onCancel()
		}
	}, w)
	d.Resize(fyne.NewSize(w.Canvas().Size().Width*0.8, d.MinSize().Height))
	d.Show()
}
