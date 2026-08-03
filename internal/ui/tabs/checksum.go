package tabs

import (
	"fmt"
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
)

var ChecksumFileEntry *widget.Entry

// hashRow groups the widgets representing a single checksum option.
type hashRow struct {
	name     string
	check    *widget.Check
	richText *widget.RichText
	copyBtn  *widget.Button
	hashVal  string
}

func (r *hashRow) setHashText(text string) {
	r.hashVal = text
	r.richText.Segments = []widget.RichTextSegment{
		&widget.TextSegment{
			Text: text,
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNamePlaceHolder,
			},
		},
	}
	r.richText.Refresh()
}

// BuildChecksumTab constructs the entire Checksum tab view.
func BuildChecksumTab(w fyne.Window) fyne.CanvasObject {
	// Initialize standard 7-Zip supported hashing algorithms.
	rows := []*hashRow{
		{name: "CRC32"},
		{name: "CRC64"},
		{name: "SHA1"},
		{name: "SHA256"},
		{name: "BLAKE2sp"},
		{name: "MD5"},
		{name: "XXH64"},
		{name: "SHA384"},
		{name: "SHA512"},
		{name: "SHA3-256"},
	}

	// File path entry
	fileEntry := widget.NewEntry()
	fileEntry.PlaceHolder = "Select a file to calculate checksums..."

	ChecksumFileEntry = fileEntry

	browseBtn := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		// Run in a goroutine so the Fyne UI doesn't freeze while the native dialog is open
		go func() {
			file, err := zenity.SelectFile(
				zenity.Title("Select File for Checksum"),
			)
			if err == nil && file != "" {
				fyne.Do(func() {
					fileEntry.SetText(file)
				})
			}
		}()
	})

	// Use FormLayout to align the start of the entries.
	// Column 1: Checkbox (aligned to the widest element, "BLAKE2sp").
	// Column 2: Entry & copy button container (fills the remaining space).
	rowsForm := container.New(layout.NewFormLayout())
	for _, r := range rows {
		r.check = widget.NewCheck(r.name, nil)
		r.richText = widget.NewRichText(&widget.TextSegment{
			Text: "Pending selection...",
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNamePlaceHolder,
			},
		})
		r.richText.Wrapping = fyne.TextWrapBreak

		// Closure to safely capture the current row's widgets
		r.copyBtn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func(row *hashRow) func() {
			return func() {
				if row.hashVal == "" || row.hashVal == "Pending selection..." || row.hashVal == "Calculating..." || strings.HasPrefix(row.hashVal, "Error:") {
					return
				}
				w.Clipboard().SetContent(row.hashVal)
				appstate.SetInfo(fmt.Sprintf("%s copied to clipboard.", row.name))
			}
		}(r))
		r.copyBtn.Disable() // Disabled until calculation completes
		r.copyBtn.Importance = widget.LowImportance

		// Layout the right-side element: Copy Button on the Right, Entry expanding in the center
		rightContainer := container.NewBorder(nil, nil, nil, r.copyBtn, r.richText)

		// FormLayout pairs: Column 1, Column 2
		rowsForm.Add(r.check)
		rowsForm.Add(rightContainer)
	}

	// Select and Deselect utilities for improved UX
	selectAllBtn := widget.NewButton("Select All", func() {
		for _, r := range rows {
			r.check.SetChecked(true)
		}
	})
	selectAllBtn.Importance = widget.LowImportance

	deselectAllBtn := widget.NewButton("Deselect All", func() {
		for _, r := range rows {
			r.check.SetChecked(false)
		}
	})
	deselectAllBtn.Importance = widget.LowImportance

	actionRow := container.NewHBox(
		layout.NewSpacer(),
		selectAllBtn,
		deselectAllBtn,
	)

	optionsGroup := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Checksum Options", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel("Choose hashing algorithms to calculate"),
			widget.NewSeparator(),
			actionRow,
		),
		nil,
		nil,
		nil,
		container.NewVScroll(rowsForm),
	)

	// Main execution handler
	calcBtn := widget.NewButtonWithIcon("Calculate Checksums", theme.ConfirmIcon(), func() {
		dialog.ShowConfirm(
			"Calculate checksums",
			"Calculation time depends on file size. Real-time logging is displayed in the Status tab.\n\nProceed?",
			func(confirmed bool) {
				if !confirmed {
					return
				}

				filePath := fileEntry.Text
				if filePath == "" {
					dialog.ShowError(fmt.Errorf("please select a file first"), w)
					return
				}

				// Collect selected checksum methods and reset UI state
				var selected []string
				for _, r := range rows {
					r.setHashText("")
					r.copyBtn.Disable()
					if r.check.Checked {
						r.setHashText("Calculating...")
						selected = append(selected, r.name)
					}
				}

				// Ensure at least one checkbox is checked
				if len(selected) == 0 {
					dialog.ShowError(fmt.Errorf("please select at least one checksum method"), w)
					return
				}

				// If exactly 1 method is selected, request only that algorithm to bypass calculating the rest.
				// If multiple methods are selected, use "*" to calculate all hashes in a single I/O pass. This avoids sequential commands which would trigger multiple redundant disk-reads of large files.
				scrcArg := "-scrc*"
				if len(selected) == 1 {
					scrcArg = "-scrc" + selected[0]
				}

				args := []string{"h", scrcArg, filePath}

				// On successful execution, parse out results and navigate to the Checksum tab
				onSuccess := func() {
					hashes := engine.ParseHashesFromLog(engine.GetLogLines())

					for _, r := range rows {
						if r.check.Checked {
							val, exists := hashes[strings.ToUpper(r.name)]
							if exists {
								r.setHashText(val)
								r.copyBtn.Enable()
							} else {
								r.setHashText("Error: Hash missing from output")
							}
						} else {
							r.setHashText("Pending selection...")
						}
					}

					// Switch back to the Checksum tab so the user can see the results
					if appstate.Tabs != nil {
						appstate.Tabs.Select(domain.ChecksumTabRank)
					}
				}

				// Switch to the Status tab so the user can see progress in real-time
				if appstate.Tabs != nil {
					appstate.Tabs.Select(domain.StatusTabRank)
				}

				// Run calculation asynchronously via operations.go wrapper
				engine.StartOperation(args, "Checksums", "", w, onSuccess)
			},
			w,
		)
	})
	calcBtn.Importance = widget.HighImportance

	return container.NewPadded(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Checksums"),
			widget.NewSeparator(),
			container.NewPadded(container.NewBorder(nil, nil, nil, browseBtn, fileEntry)),
			widget.NewLabel(""),
		),
		container.NewVBox(
			widget.NewLabel(""),
			container.NewHBox(layout.NewSpacer(), calcBtn),
		),
		nil,
		nil,
		optionsGroup,
	))
}