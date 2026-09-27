package tabs

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ncruces/zenity"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
	"github.com/Softorage/7z-GUI-Linux/internal/engine"
	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/ui/components"
)

var ExtractSrcEntry *widget.Entry
var ExtractDestEntry *widget.Entry

// getArchiveBaseName computes a clean folder name by stripping container and multi-volume suffixes.
func getArchiveBaseName(archivePath string) string {
	name := filepath.Base(archivePath)
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}

	// Compound tarball extensions (.tar.gz, .tar.bz2, etc.)
	for _, tarExt := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".tbz", ".txz"} {
		if sys.HasSuffixFold(name, tarExt) {
			return name[:len(name)-len(tarExt)]
		}
	}

	// Numeric split extensions (.7z.001, .zip.001, .001)
	if sys.ClassifyArchiveVolume(name) != sys.VolumeTypeNone && len(ext) >= 3 && ext[0] == '.' {
		stem := name[:len(name)-len(ext)]
		innerExt := filepath.Ext(stem)
		if innerExt != "" && sys.IsArchiveExtension(stem) {
			return stem[:len(stem)-len(innerExt)]
		}
		return stem
	}

	// RAR multi-volume parts (.part1.rar, .part01.rar)
	if sys.HasSuffixFold(ext, ".rar") {
		stem := name[:len(name)-len(ext)]
		partIdx := strings.LastIndex(strings.ToLower(stem), ".part")
		if partIdx != -1 && partIdx > 0 {
			return stem[:partIdx]
		}
		return stem
	}

	return strings.TrimSuffix(name, ext)
}

func BuildExtractTab(w fyne.Window) fyne.CanvasObject {
	var selectedArchives []string

	// Backward compatibility global entry (hidden listener)
	ExtractSrcEntry = widget.NewEntry()

	destEntry := widget.NewEntry()
	ExtractDestEntry = destEntry

	autoOpenCheck := widget.NewCheck("Auto-open folder after extraction", nil)
	autoOpenCheck.OnChanged = func(_ bool) {
		appstate.SetInfo("Automatically open the destination folder when extraction finishes.")
	}

	createSubfolderCheck := widget.NewCheck("Extract to sub-folder", nil)
	createSubfolderCheck.SetChecked(true)

	// Helper to resolve the top-level path
	updateDestPath := func() {
		if len(selectedArchives) == 0 {
			destEntry.SetText("")
			return
		}
		parentPath := filepath.Dir(selectedArchives[0])
		if len(selectedArchives) == 1 && createSubfolderCheck.Checked {
			baseName := getArchiveBaseName(selectedArchives[0])
			destEntry.SetText(filepath.Join(parentPath, baseName))
		} else {
			destEntry.SetText(parentPath)
		}
	}

	createSubfolderCheck.OnChanged = func(_ bool) {
		appstate.SetInfo("Extract into a new folder named after the archive.")
		// Trigger local update of path destination preview
		updateDestPath()
	}

	var archiveList *widget.List
	archiveList = widget.NewList(
		func() int {
			return len(selectedArchives)
		},
		func() fyne.CanvasObject {
			icon := widget.NewIcon(theme.FileIcon())
			lbl := widget.NewLabel("Template path placeholder")
			lbl.TextStyle = fyne.TextStyle{Bold: false}

			deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			deleteBtn.Importance = widget.LowImportance

			return container.NewBorder(nil, nil, icon, deleteBtn, lbl)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(selectedArchives) {
				return
			}
			path := selectedArchives[id]
			borderContainer := o.(*fyne.Container)

			var iconWidget *widget.Icon
			var labelWidget *widget.Label
			var btnWidget *widget.Button

			for _, obj := range borderContainer.Objects {
				switch typed := obj.(type) {
				case *widget.Icon:
					iconWidget = typed
				case *widget.Label:
					labelWidget = typed
				case *widget.Button:
					btnWidget = typed
				}
			}

			if iconWidget == nil || labelWidget == nil || btnWidget == nil {
				return
			}

			labelWidget.SetText(sys.TruncateDisplayPath(path, 55))
			iconWidget.SetResource(theme.FileIcon())

			btnWidget.OnTapped = func() {
				for i, s := range selectedArchives {
					if s == path {
						selectedArchives = append(selectedArchives[:i], selectedArchives[i+1:]...)
						break
					}
				}
				archiveList.Refresh()
				fyne.Do(func() {
					if len(selectedArchives) == 0 {
						archiveList.Hide()
					}
					updateDestPath()
				})
			}
		},
	)

	archiveList.OnSelected = func(id widget.ListItemID) {
		archiveList.Unselect(id)
	}

	listPlaceholder := widget.NewLabel("No archive files selected. Use the buttons on the right to add archives.")
	listPlaceholder.Alignment = fyne.TextAlignCenter

	listStack := container.NewStack(archiveList, listPlaceholder)

	refreshArchiveList := func() {
		if len(selectedArchives) == 0 {
			archiveList.Hide()
			listPlaceholder.Show()
		} else {
			archiveList.Show()
			listPlaceholder.Hide()
			archiveList.Refresh()
		}
		listStack.Refresh()
		updateDestPath()
	}

	// addArchives normalizes continuation volumes to their primary volume and deduplicates entries.
	addArchives := func(paths []string) {
		if len(paths) == 0 {
			return
		}
		existing := make(map[string]struct{}, len(selectedArchives)+len(paths))
		for _, s := range selectedArchives {
			existing[s] = struct{}{}
		}

		hasChanges := false
		hasContinuation := false

		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			primary := sys.ResolvePrimaryVolume(p)
			if primary != p {
				hasContinuation = true
			}
			if _, found := existing[primary]; !found {
				existing[primary] = struct{}{}
				selectedArchives = append(selectedArchives, primary)
				hasChanges = true
			}
		}
		
		if hasChanges {
			refreshArchiveList()
			if hasContinuation {
				appstate.SetInfo("Continuation volumes were automatically resolved to their primary volume.")
			}
		}
	}

	ExtractSrcEntry.OnChanged = func(val string) {
		if val == "" {
			return
		}
		ExtractSrcEntry.SetText("") // Clear listening value safely to prevent recurrences
		addArchives(strings.Split(val, "\n"))
	}

	browseFileBtn := widget.NewButtonWithIcon("Add Archives", theme.FileIcon(), func() {
		go func() {
			files, err := zenity.SelectFileMultiple(
				zenity.Title("Select Archives"),
				zenity.FileFilters{
					{Name: "Supported Archives", Patterns: []string{
						"*.zip", "*.7z", "*.rar", "*.tar.gz", "*.tar", "*.gz", "*.bz2", "*.xz", "*.wim",
						"*.001", "*.part*.rar", "*.r00", "*.z01",
					}},
					{Name: "All Files", Patterns: []string{"*"}},
				},
			)
			if err == nil && len(files) > 0 {
				fyne.Do(func() {
					addArchives(files)
				})
			}
		}()
	})

	clearBtn := widget.NewButtonWithIcon("Clear All", theme.ContentClearIcon(), func() {
		selectedArchives = nil
		refreshArchiveList()
	})
	clearBtn.Importance = widget.LowImportance

	browseBtns := container.NewVBox(browseFileBtn, clearBtn)
	srcContainer := container.NewBorder(nil, nil, nil, browseBtns, listStack)

	destBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		// Capture the current text while we are still on the main UI thread
		currentPath := destEntry.Text

		// Run in a goroutine to prevent UI blocking
		go func() {
			// Set up default Zenity options
			opts := []zenity.Option{
				zenity.Title("Select Destination"),
				zenity.Directory(), // Tell Zenity to open a folder picker
			}

			// If there is already a path, tell Zenity to start there
			if currentPath != "" {
				// ncruces/zenity requires directory paths to end with a separator
				if !strings.HasSuffix(currentPath, string(filepath.Separator)) {
					currentPath += string(filepath.Separator)
				}
				opts = append(opts, zenity.Filename(currentPath))
			}

			// Pass the options into SelectFile
			folder, err := zenity.SelectFile(opts...)

			if err == nil && folder != "" {
				fyne.Do(func() {
					destEntry.SetText(folder)
				})
			}
		}()
	})

	var extractNext func(idx int)
	extractNext = func(idx int) {
		if idx >= len(selectedArchives) {
			if autoOpenCheck.Checked {
				exec.Command("xdg-open", destEntry.Text).Start()
			}
			appstate.SetInfo("All extraction tasks complete.")
			return
		}

		src := selectedArchives[idx]
		var dest string
		if len(selectedArchives) > 1 && createSubfolderCheck.Checked {
			baseName := getArchiveBaseName(src)
			dest = filepath.Join(destEntry.Text, baseName)
		} else if len(selectedArchives) == 1 && createSubfolderCheck.Checked {
			dest = destEntry.Text
		} else {
			dest = destEntry.Text
		}

		go func() {
			appstate.SetInfo(fmt.Sprintf("Checking %s...", filepath.Base(src)))
			isProtected := engine.IsPasswordProtected(src)

			fyne.Do(func() {
				opMode := fmt.Sprintf("Extracting (%d/%d)", idx+1, len(selectedArchives))
				targetArchive := filepath.Base(src)
				onFinish := func() {
					extractNext(idx + 1)
				}

				if isProtected {
					components.PromptArchivePassword(w, src, "Extract", func(pwd string) {
						args := []string{"x", src, "-o" + dest, "-bsp1", "-y", "-p" + pwd}
						if appstate.Tabs != nil {
							appstate.Tabs.Select(domain.StatusTabRank)
						}
						engine.StartOperation(targetArchive, args, opMode, "", w, onFinish)
					}, func() {
						appstate.SetInfo(fmt.Sprintf("Extraction of %s skipped.", filepath.Base(src)))
						extractNext(idx + 1)
					})
				} else {
					args := []string{"x", src, "-o" + dest, "-bsp1", "-y"}
					if appstate.Tabs != nil {
						appstate.Tabs.Select(domain.StatusTabRank)
					}
					engine.StartOperation(targetArchive, args, opMode, "", w, onFinish)
				}
			})
		}()
	}

	extractBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() {
		appstate.StateMu.RLock()
		running := appstate.IsOperationRunning
		appstate.StateMu.RUnlock()
		if running {
			dialog.ShowError(fmt.Errorf("an operation is already running"), w)
			return
		}

		if len(selectedArchives) == 0 || destEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("select both archive(s) and destination"), w)
			return
		}

		extractNext(0)
	})
	extractBtn.Importance = widget.HighImportance

	refreshArchiveList()

	form := widget.NewForm(
		widget.NewFormItem("Archive Files:", srcContainer),
		widget.NewFormItem("Extract To:", container.NewBorder(nil, nil, nil, destBtn, destEntry)),
		widget.NewFormItem("Options:", container.NewVBox(autoOpenCheck, createSubfolderCheck)),
	)

	return container.NewPadded(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Extract"),
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			container.NewHBox(
				layout.NewSpacer(),
				widget.NewButton("Cancel", func() {
					selectedArchives = nil
					refreshArchiveList()
				}),
				extractBtn,
			),
		),
		nil,
		nil,
		container.NewVScroll(form),
	))
}
