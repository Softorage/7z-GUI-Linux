package components

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// PromptConflict prompts the user to resolve a file name collision using a buffered response channel.
func PromptConflict(w fyne.Window, filename string) domain.ConflictAction {
	ch := make(chan domain.ConflictAction, 1)
	fyne.Do(func() {
		var d dialog.Dialog
		replaceBtn := widget.NewButton("Replace", func() { ch <- domain.ActionReplace; d.Hide() })
		replaceAllBtn := widget.NewButton("Replace All", func() { ch <- domain.ActionReplaceAll; d.Hide() })
		renameBtn := widget.NewButton("Rename (Auto)", func() { ch <- domain.ActionRename; d.Hide() })
		renameAllBtn := widget.NewButton("Rename All (Auto)", func() { ch <- domain.ActionRenameAll; d.Hide() })
		skipBtn := widget.NewButton("Skip", func() { ch <- domain.ActionSkip; d.Hide() })
		skipAllBtn := widget.NewButton("Skip All", func() { ch <- domain.ActionSkipAll; d.Hide() })

		content := container.NewVBox(
			widget.NewLabel(fmt.Sprintf("An item named '%s' already exists at the destination.", filename)),
			widget.NewLabel("What would you like to do?"),
			widget.NewSeparator(),
			container.NewGridWithColumns(3, replaceBtn, renameBtn, skipBtn, replaceAllBtn, renameAllBtn, skipAllBtn),
		)

		d = dialog.NewCustom("File Conflict", "Cancel", content, w)
		d.SetOnClosed(func() {
			select {
			case ch <- domain.ActionCancel:
			default:
			}
		})
		d.Show()
	})
	return <-ch
}

// PromptTypeConflict displays a destructive dialog when source and target types collide (file vs folder).
func PromptTypeConflict(w fyne.Window, filename string, srcIsDir, dstIsDir bool) domain.ConflictAction {
	ch := make(chan domain.ConflictAction, 1)
	fyne.Do(func() {
		var d dialog.Dialog

		srcType, dstType := "a file", "a file"
		if srcIsDir {
			srcType = "a directory"
		}
		if dstIsDir {
			dstType = "a directory"
		}

		replaceBtn := widget.NewButton("Replace (Delete Existing)", func() { ch <- domain.ActionReplace; d.Hide() })
		replaceBtn.Importance = widget.DangerImportance
		renameBtn := widget.NewButton("Rename (Auto)", func() { ch <- domain.ActionRename; d.Hide() })
		renameAllBtn := widget.NewButton("Rename All (Auto)", func() { ch <- domain.ActionRenameAll; d.Hide() })
		skipBtn := widget.NewButton("Skip", func() { ch <- domain.ActionSkip; d.Hide() })
		skipAllBtn := widget.NewButton("Skip All", func() { ch <- domain.ActionSkipAll; d.Hide() })

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
			case ch <- domain.ActionCancel:
			default:
			}
		})
		d.Show()
	})
	return <-ch
}