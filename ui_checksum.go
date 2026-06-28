package main

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
)

// hashRow groups the widgets representing a single checksum option.
type hashRow struct {
	name    string
	check   *widget.Check
	entry   *widget.Entry
	copyBtn *widget.Button
}

// buildChecksumTab constructs the entire Checksum tab view.
func buildChecksumTab(w fyne.Window) fyne.CanvasObject {
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

	// File path entry (disabled/read-only for user, updated via browse)
	fileEntry := widget.NewEntry()
	fileEntry.Disable()
	fileEntry.PlaceHolder = "Select a file to calculate checksums..."

	browseBtn := widget.NewButtonWithIcon("Browse", theme.FolderOpenIcon(), func() {
		// Run in a goroutine so the Fyne UI doesn't freeze while the native dialog is open
		go func() {
			file, err := zenity.SelectFile(
				zenity.Title("Select File for Checksum"),
			)
			if err == nil && file != "" {
				fileEntry.SetText(file)
			}
		}()
	})

	// Use FormLayout to align the start of the entries.
	// Column 1: Checkbox (aligned to the widest element, "BLAKE2sp").
	// Column 2: Entry & copy button container (fills the remaining space).
	rowsForm := container.New(layout.NewFormLayout())
	for _, r := range rows {
		r.check = widget.NewCheck(r.name, nil)
		r.entry = widget.NewEntry()
		r.entry.Disable() // Act as read-only to allow highlighting and text copying
		r.entry.PlaceHolder = "Pending selection..."

		// Closure to safely capture the current row's widgets
		r.copyBtn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func(entry *widget.Entry, name string) func() {
			return func() {
				if entry.Text == "" {
					return
				}
				w.Clipboard().SetContent(entry.Text)
				setInfo(fmt.Sprintf("%s copied to clipboard.", name))
			}
		}(r.entry, r.name))
		r.copyBtn.Disable() // Disabled until calculation completes
		r.copyBtn.Importance = widget.LowImportance

		// Layout the right-side element: Copy Button on the Right, Entry expanding in the center
		rightContainer := container.NewBorder(nil, nil, nil, r.copyBtn, r.entry)

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
	calcBtn := widget.NewButtonWithIcon("Calculate Checksums", theme.ConfirmIcon(), func() { // TODO: even checking a single checkbox causes all the checksum calculations. if this is likely to take significant time for larger files, then optimize.
		dialog.ShowConfirm(
			"Calculate checkums",
			"Calculation time depends on file size. Real-time logging is displayed in the Status tab.\n\nProceed?",
			func(confirmed bool) {
				if confirmed {
					filePath := fileEntry.Text
					if filePath == "" {
						dialog.ShowError(fmt.Errorf("please select a file first"), w)
						return
					}

					// Ensure at least one checkbox is checked
					anyChecked := false
					for _, r := range rows {
						if r.check.Checked {
							anyChecked = true
							break
						}
					}
					if !anyChecked {
						dialog.ShowError(fmt.Errorf("please select at least one checksum method"), w)
						return
					}

					// Reset values and copy states before starting
					for _, r := range rows {
						r.entry.SetText("")
						r.copyBtn.Disable()
						if r.check.Checked {
							r.entry.SetText("Calculating...")
						}
					}

					// Invoke the 7-Zip CLI with -scrc* to process all hashes efficiently in a single pass.
					// Only checked entries are populated when done.
					args := []string{"h", "-scrc*", filePath}

					// On successful execution, parse out results and navigate to the Checksum tab
					onSuccess := func() {
						hashes := parseHashesFromLog()

						for _, r := range rows {
							if r.check.Checked {
								val, exists := hashes[strings.ToUpper(r.name)]
								if exists {
									r.entry.SetText(val)
									r.copyBtn.Enable()
								} else {
									r.entry.SetText("Error: Hash missing from output")
								}
							} else {
								r.entry.SetText("")
							}
						}

						// Switch back to the Checksum tab (index 2) so the user can see the results
						tabs.Select(ChecksumTabRank)
					}

					// Switch to the Status tab (index 3) so the user can see the calculation progress in real-time
					tabs.Select(StatusTabRank)

					// Run calculation asynchronously via operations.go wrapper
					startOperation(args, "Checksums", w, onSuccess)
				}
			},
			w,
		)
	})
	calcBtn.Importance = widget.HighImportance

	return container.NewPadded(container.NewBorder(
			container.NewVBox(
				widget.NewRichTextFromMarkdown("## Checksums"),
				widget.NewSeparator(),
				container.NewBorder(nil, nil, nil, browseBtn, fileEntry),
				widget.NewLabel(""),
			),
			container.NewVBox(
				widget.NewLabel(""),
				container.NewHBox(layout.NewSpacer(), calcBtn),
			),
			nil,
			nil,
			optionsGroup,
		),
	)
}

// parseHashesFromLog scans log outputs and isolates algorithm results.
func parseHashesFromLog() map[string]string {
	hashes := make(map[string]string)

	logMu.Lock()
	// Combine stable log lines and active current log line buffer
	allLines := make([]string, len(logLines))
	copy(allLines, logLines)
	if len(currentLogLine) > 0 {
		allLines = append(allLines, string(currentLogLine))
	}
	logMu.Unlock()

	// Scan backward to pull target checksum markers specifically from the latest run
	for i := len(allLines) - 1; i >= 0; i-- {
		line := allLines[i]
		if strings.Contains(line, "Running:") {
			// Stop scanning when reaching the header boundary of the current execution
			break
		}
		if strings.Contains(line, "for data:") {
			parts := strings.Split(line, "for data:")
			if len(parts) == 2 {
				algo := strings.TrimSpace(parts[0])
				hashVal := strings.TrimSpace(parts[1])

				// Standardize output formats
				if idx := strings.Index(hashVal, "-"); idx != -1 {
					hashVal = hashVal[:idx]
				}
				hashes[strings.ToUpper(algo)] = strings.TrimSpace(hashVal)
			}
		}
	}
	return hashes
}
