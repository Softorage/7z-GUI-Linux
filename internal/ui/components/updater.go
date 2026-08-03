package components

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Softorage/7z-GUI-Linux/internal/sys"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

func ShowUpdateDialog(w fyne.Window, a fyne.App, rel sys.GithubRelease) {
	relURL, err := url.Parse(rel.HTMLURL)
	if err != nil {
		relURL, _ = url.Parse("https://github.com/Softorage/7z-GUI-Linux/releases/latest")
	}

	heading := widget.NewLabelWithStyle("New Update Available!", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	verInfo := widget.NewLabel(fmt.Sprintf("Installed: %s  ➜  Latest: %s", version.Version, rel.TagName))
	verInfo.Alignment = fyne.TextAlignCenter

	contentObjects := []fyne.CanvasObject{heading, verInfo}

	if notes := strings.TrimSpace(rel.Body); notes != "" {
		notesHeader := widget.NewLabelWithStyle("Release Notes:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

		//notesText := widget.NewRichTextFromMarkdown(notes)
		notesText := widget.NewRichText(&widget.TextSegment{
			Text: notes,
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNamePlaceHolder, // e.g., ColorNamePrimary, ColorNamePlaceHolder, ColorNameWarning, ColorNameError
			},
		})
		notesText.Wrapping = fyne.TextWrapWord

		notesBox := container.NewGridWrap(fyne.NewSize(440, 160), container.NewPadded(container.NewVScroll(notesText)))
		contentObjects = append(contentObjects, widget.NewSeparator(), notesHeader, notesBox)
	}

	dialog.ShowCustomConfirm(
		"Software Update",
		"Download Update ↗",
		"Later",
		container.NewVBox(contentObjects...),
		func(confirm bool) {
			if confirm {
				a.OpenURL(relURL)
			}
		},
		w,
	)
}