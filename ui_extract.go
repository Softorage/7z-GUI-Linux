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
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err == nil && uri != nil {

				srcPath := uri.URI().Path()
				srcEntry.SetText(srcPath)

				// Get the parent folder URI and set it as default value for destination entry
				parentURI, err := storage.Parent(uri.URI())
				if err == nil {

					destPath := parentURI.Path()

					if createSubfolderCheck.Checked {
						baseName := strings.TrimSuffix(uri.URI().Name(), uri.URI().Extension())
						destPath = filepath.Join(destPath, baseName)
					}
					destEntry.SetText(destPath)

				}
				uri.Close()
			}
		}, w)
		windowSize := w.Canvas().Size()
		d.Resize(fyne.NewSize(windowSize.Width*0.8, windowSize.Height*0.8))
		d.Show()
	})

	destBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				destEntry.SetText(uri.Path())
			}
		}, w)
		windowSize := w.Canvas().Size()
		d.Resize(fyne.NewSize(windowSize.Width*0.8, windowSize.Height*0.8))
		d.Show()
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

		// Run check asynchronously to avoid blocking the Fyne UI loop
		go func() {
			setInfo("Checking archive...")

			var onSuccess func()
			if autoOpenBool {
				onSuccess = func() {
					// Utilizes xdg-open to launch the system's default file manager on Linux
					exec.Command("xdg-open", dest).Start()
				}
			}

			if isPasswordProtected(src) {
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
						tabs.SelectIndex(2)
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
				tabs.SelectIndex(2)
				startOperation(args, "Extracting", w, onSuccess)
			}
		}()
	})
	extractBtn.Importance = widget.HighImportance

	form := widget.NewForm(
		widget.NewFormItem("Archive File:", container.NewBorder(nil, nil, nil, srcBtn, srcEntry)),
		widget.NewFormItem("Extract To:", container.NewBorder(nil, nil, nil, destBtn, destEntry)),
		widget.NewFormItem("Options:", container.NewVBox(autoOpenCheck, container.NewHBox(createSubfolderCheck, updateDestBtn))),
	)

	return container.NewPadded(container.NewVBox(
		form,
		layout.NewSpacer(),
		container.NewHBox(layout.NewSpacer(), extractBtn),
	))
}
