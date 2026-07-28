package main

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
)

var extractSrcEntry *widget.Entry
var extractDestEntry *widget.Entry

func buildExtractTab(w fyne.Window) fyne.CanvasObject {
	var selectedArchives []string

	// Backward compatibility global entry (hidden listener)
	extractSrcEntry = widget.NewEntry()

	destEntry := widget.NewEntry()
	extractDestEntry = destEntry

	autoOpenCheck := widget.NewCheck("Auto-open folder after extraction", nil)
	autoOpenCheck.OnChanged = func(_ bool) {
		setInfo("Automatically open the destination folder when extraction finishes.")
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
			baseName := strings.TrimSuffix(filepath.Base(selectedArchives[0]), filepath.Ext(selectedArchives[0]))
			destEntry.SetText(filepath.Join(parentPath, baseName))
		} else {
			destEntry.SetText(parentPath)
		}
	}

	createSubfolderCheck.OnChanged = func(_ bool) {
		setInfo("Extract into a new folder named after the archive.")
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

			labelWidget.SetText(truncateDisplayPath(path, 55))
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

	extractSrcEntry.OnChanged = func(val string) {
		if val == "" {
			return
		}
		paths := strings.Split(val, "\n")
		hasChanges := false
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p != "" {
				exists := false
				for _, s := range selectedArchives {
					if s == p {
						exists = true
						break
					}
				}
				if !exists {
					selectedArchives = append(selectedArchives, p)
					hasChanges = true
				}
			}
		}
		if hasChanges {
			extractSrcEntry.SetText("") // Clear listening value safely to prevent recurrences
			refreshArchiveList()
		}
	}

	browseFileBtn := widget.NewButtonWithIcon("Add Archives", theme.FileIcon(), func() {
		go func() {
			files, err := zenity.SelectFileMultiple(
				zenity.Title("Select Archives"),
				zenity.FileFilters{
					{Name: "Supported Archives", Patterns: []string{"*.zip", "*.7z", "*.rar", "*.tar.gz", "*.tar", "*.gz", "*.bz2", "*.xz", "*.wim"}},
				},
			)
			if err == nil && len(files) > 0 {
				fyne.Do(func() {
					for _, f := range files {
						exists := false
						for _, s := range selectedArchives {
							if s == f {
								exists = true
								break
							}
						}
						if !exists {
							selectedArchives = append(selectedArchives, f)
						}
					}
					refreshArchiveList()
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
			setInfo("All extraction tasks complete.")
			return
		}

		src := selectedArchives[idx]
		var dest string
		if len(selectedArchives) > 1 && createSubfolderCheck.Checked {
			baseName := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
			dest = filepath.Join(destEntry.Text, baseName)
		} else if len(selectedArchives) == 1 && createSubfolderCheck.Checked {
			dest = destEntry.Text
		} else {
			dest = destEntry.Text
		}

		go func() {
			setInfo(fmt.Sprintf("Checking %s...", filepath.Base(src)))
			isProtected := isPasswordProtected(src)

			fyne.Do(func() {
				title := fmt.Sprintf("Extracting (%d/%d): %s", idx+1, len(selectedArchives), filepath.Base(src))
				onFinish := func() {
					extractNext(idx + 1)
				}

				if isProtected {
					promptArchivePassword(w, src, "Extract", func(pwd string) {
						args := []string{"x", src, "-o" + dest, "-bsp1", "-y", "-p" + pwd}
						tabs.Select(StatusTabRank)
						startOperation(args, title, "", w, onFinish)
					}, func() {
						setInfo(fmt.Sprintf("Extraction of %s skipped.", filepath.Base(src)))
						extractNext(idx + 1)
					})
				} else {
					args := []string{"x", src, "-o" + dest, "-bsp1", "-y"}
					tabs.Select(StatusTabRank)
					startOperation(args, title, "", w, onFinish)
				}
			})
		}()
	}

	extractBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()
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

// isPasswordProtected tests if the archive requires a password for extraction.
func isPasswordProtected(archive string) bool {
	// Execute '7z l' (List) with a dummy password. This is fast and will reveal
	// if the file is encrypted without extracting anything.
	cmd := exec.Command(root7zCmd, "l", "-slt", archive, "-pDummyPassword_123456789")
	out, err := cmd.CombinedOutput()

	outStr := string(out)
	lowerOut := strings.ToLower(outStr)

	if err != nil {
		// If the header itself is encrypted, 7-zip will fail to list files
		// and output an error mentioning "wrong password" or "encrypted".
		if strings.Contains(lowerOut, "wrong password") ||
			strings.Contains(lowerOut, "encrypted archive") ||
			strings.Contains(lowerOut, "error in encrypted file") {
			return true
		}
	}

	// For archives where headers are NOT encrypted but the files inside are,
	// 7-zip will successfully list the contents. We check for the 'Encrypted = +' flag.
	if strings.Contains(outStr, "\nEncrypted = +") {
		return true
	}

	return false
}
