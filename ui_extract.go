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

func buildExtractTab(w fyne.Window) fyne.CanvasObject {
	srcEntry := widget.NewEntry()
	destEntry := widget.NewEntry()

	autoOpenCheck := widget.NewCheck("Auto-open folder after extraction", nil)
	autoOpenCheck.OnChanged = func(_ bool) {
		setInfo("Automatically open the destination folder when extraction finishes.")
	}

	createSubfolderCheck := widget.NewCheck("Extract to sub-folder", nil)
	createSubfolderCheck.SetChecked(true)
	createSubfolderCheck.OnChanged = func(checked bool) {
		setInfo("Extract into a new folder named after the archive.")
	}

	updateDestBtn := widget.NewButton("Update Destination", func() {
		if srcEntry.Text == "" {
			return
		}
		parentPath := filepath.Dir(srcEntry.Text)
		if createSubfolderCheck.Checked {
			baseName := strings.TrimSuffix(filepath.Base(srcEntry.Text), filepath.Ext(srcEntry.Text))
			destEntry.SetText(filepath.Join(parentPath, baseName))
		} else {
			destEntry.SetText(parentPath)
		}
	})

	srcBtn := widget.NewButtonWithIcon("", theme.FileIcon(), func() {
		// Run in a goroutine so the Fyne UI doesn't freeze while the native dialog is open
		go func() {
			file, err := zenity.SelectFile(
				zenity.Title("Select Archive"),
				// Decide: We can filter extensions like this
				// zenity.FileFilter{Name: "Archives", Patterns: []string{"*.zip", "*.7z", "*.rar", "*.tar.gz"}},
			)
			if err == nil && file != "" {
				fyne.Do(func() {
					srcEntry.SetText(file)

					destPath := filepath.Dir(file)
					if createSubfolderCheck.Checked {
						baseName := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
						destPath = filepath.Join(destPath, baseName)
					}
					destEntry.SetText(destPath)
				})
			}
		}()
	})

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

	extractBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()
		if running {
			dialog.ShowError(fmt.Errorf("an operation is already running"), w)
			return
		}

		if srcEntry.Text == "" || destEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("select both archive and destination"), w)
			return
		}

		src := srcEntry.Text
		dest := destEntry.Text
		autoOpenBool := autoOpenCheck.Checked

		// Keep the blocking process-check inside the background goroutine
		go func() {
			setInfo("Checking archive...")

			var onSuccess func()
			if autoOpenBool {
				onSuccess = func() {
					// Utilizes xdg-open to launch the system's default file manager on Linux
					exec.Command("xdg-open", dest).Start()
				}
			}

			// Synchronous disk/process reading remains safely in background
			isProtected := isPasswordProtected(src)

			fyne.Do(func() {
				if isProtected {
					pwdEntry := widget.NewPasswordEntry()
					pwdEntry.PlaceHolder = "Enter Password"

					items := []*widget.FormItem{
						widget.NewFormItem("Password:", pwdEntry),
					}

					d := dialog.NewForm("Password Required", "Extract", "Cancel", items, func(submit bool) {
						if submit {
							// Append the -p switch with the user's password
							// Note: os/exec handles spaces safely automatically, no manual shell-escaping needed
							args := []string{"x", src, "-o" + dest, "-bsp1", "-y", "-p" + pwdEntry.Text}
							tabs.Select(StatusTabRank)
							startOperation(args, "Extracting", w, onSuccess)
						} else {
							setInfo("Extraction cancelled.")
						}
					}, w)
					windowSize := w.Canvas().Size()
					d.Resize(fyne.NewSize(windowSize.Width*0.8, d.MinSize().Height))
					d.Show()
				} else {
					// Proceed normally if no password is required
					args := []string{"x", src, "-o" + dest, "-bsp1", "-y"}
					tabs.Select(StatusTabRank)
					startOperation(args, "Extracting", w, onSuccess)
				}
			})
		}()
	})
	extractBtn.Importance = widget.HighImportance

	form := widget.NewForm(
		widget.NewFormItem("Archive File:", container.NewBorder(nil, nil, nil, srcBtn, srcEntry)),
		widget.NewFormItem("Extract To:", container.NewBorder(nil, nil, nil, destBtn, destEntry)),
		widget.NewFormItem("Options:", container.NewVBox(autoOpenCheck, container.NewHBox(createSubfolderCheck, updateDestBtn))),
	)

	return container.NewPadded(container.NewVBox(
		widget.NewRichTextFromMarkdown("## Extract"),
		widget.NewSeparator(),
		form,
		layout.NewSpacer(),
		container.NewHBox(layout.NewSpacer(), extractBtn),
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
