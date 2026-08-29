package components

import (
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// PromptArchivePassword displays input dialog prompting user for password when entering encrypted archives.
func PromptArchivePassword(w fyne.Window, archivePath, confirmLabel string, onSuccess func(string), onCancel func()) {
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
