package tabs

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
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
)

// Any UI manipulation (like .SetText(), .SetValue(), .Refresh()) that is triggered inside a background goroutine (go func()) or a background timer (time.AfterFunc) must be wrapped in fyne.Do

var CompressSrcEntry *widget.Entry

func BuildCompressTab(w fyne.Window) fyne.CanvasObject {
	var selectedSources []string

	// Backward compatibility global entry (hidden listener)
	CompressSrcEntry = widget.NewEntry()

	// Custom Archive Name Checkbox and Entry
	customNameEntry := widget.NewEntry()
	customNameEntry.PlaceHolder = "Enter custom archive name (without extension)"
	customNameEntry.Disable()

	customNameCheck := widget.NewCheck("Custom Name", nil)

	// Format Select
	formatSelect := widget.NewSelect([]string{"7z", "xz", "bzip2", "gzip", "tar", "zip", "wim"}, nil)
	formatSelect.SetSelected("7z")

	// SFX Checkbox
	sfxCheck := widget.NewCheck("Create SFX archive", nil)

	// Output Path Preview Label
	archivePreviewLabel := widget.NewLabel("No source files selected")
	archivePreviewLabel.Wrapping = fyne.TextWrapWord

	// Central helper to update output preview text
	updateArchivePreview := func() {
		if len(selectedSources) == 0 {
			archivePreviewLabel.SetText("No source files selected")
			return
		}

		format := formatSelect.Selected
		sfx := sfxCheck.Checked
		customChecked := customNameCheck.Checked
		customText := strings.TrimSpace(customNameEntry.Text)

		customName := ""

		if customChecked && customText != "" {
			customName = customText
		}

		// Retrieve consolidated target destination from shared package-level helper
		destPath := engine.GetArchiveDestination(selectedSources, format, customName, sfx)
		archivePreviewLabel.SetText(destPath)

		if sys.HasDuplicateFilenames(selectedSources) {
			appstate.SetInfo("Notice: Duplicate filenames detected. Full relative paths will be preserved (-spf2) to prevent conflicts.")
		}
	}

	// Declare list variable beforehand so its functions can reference it
	var sourceList *widget.List

	// Instantiate the standardized, performant list widget
	sourceList = widget.NewList(
		func() int {
			return len(selectedSources)
		},
		func() fyne.CanvasObject {
			icon := widget.NewIcon(theme.FileIcon())
			lbl := widget.NewLabel("Template path placeholder")
			lbl.TextStyle = fyne.TextStyle{Bold: false}

			deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			deleteBtn.Importance = widget.LowImportance

			// Layout keeps details visually segmented and pins delete buttons to the right
			return container.NewBorder(nil, nil, icon, deleteBtn, lbl)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(selectedSources) {
				return
			}
			path := selectedSources[id]

			borderContainer := o.(*fyne.Container)

			// Robust Type Assertion Scan: Matches elements correctly across current/future Fyne layout engines
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

			// Format text with display truncation
			labelWidget.SetText(sys.TruncateDisplayPath(path, 55))

			// Detect directories vs files to select matching system theme icons [1]
			isDir := false
			if fInfo, err := os.Stat(path); err == nil {
				isDir = fInfo.IsDir()
			}
			iconResource := theme.FileIcon()
			if isDir {
				iconResource = theme.FolderIcon()
			}
			iconWidget.SetResource(iconResource)

			// Individual row item removal handler
			btnWidget.OnTapped = func() {
				for i, s := range selectedSources {
					if s == path {
						selectedSources = append(selectedSources[:i], selectedSources[i+1:]...)
						break
					}
				}
				sourceList.Refresh()
				// We call the state updates on main thread to trigger views
				fyne.Do(func() {
					if len(selectedSources) == 0 {
						sourceList.Hide()
					}
					updateArchivePreview()
				})
			}
		},
	)

	// Clean Selection override: prevents selection highlight styles from sticking on clicked items
	sourceList.OnSelected = func(id widget.ListItemID) {
		sourceList.Unselect(id)
	}

	// Empty State Placeholder
	listPlaceholder := widget.NewLabel("No files or folders selected. Use the buttons on the right to add elements.")
	listPlaceholder.Alignment = fyne.TextAlignCenter

	// Stack lists and placeholders cleanly together
	listStack := container.NewStack(sourceList, listPlaceholder)

	// Main list updating coordinator
	refreshSourceList := func() {
		if len(selectedSources) == 0 {
			sourceList.Hide()
			listPlaceholder.Show()
		} else {
			sourceList.Show()
			listPlaceholder.Hide()
			sourceList.Refresh()
		}
		listStack.Refresh()
		updateArchivePreview()
	}

	// Intercept and parse incoming values directed into standard compressSrcEntry strings
	CompressSrcEntry.OnChanged = func(val string) {
		if val == "" {
			return
		}
		paths := strings.Split(val, "\n")
		hasChanges := false
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p != "" {
				exists := false
				for _, s := range selectedSources {
					if s == p {
						exists = true
						break
					}
				}
				if !exists {
					selectedSources = append(selectedSources, p)
					hasChanges = true
				}
			}
		}
		if hasChanges {
			CompressSrcEntry.SetText("") // Clear listening value safely to prevent recurrences
			refreshSourceList()
		}
	}

	// Action buttons
	browseFileBtn := widget.NewButtonWithIcon("Add Files", theme.FileIcon(), func() {
		go func() {
			files, err := zenity.SelectFileMultiple(
				zenity.Title("Select Files"),
				// Decide: We can filter extensions like this
				// zenity.FileFilter{Name: "Archives", Patterns: []string{"*.zip", "*.7z", "*.rar", "*.tar.gz"}},
			)
			if err == nil && len(files) > 0 {
				fyne.Do(func() {
					for _, f := range files {
						exists := false
						for _, s := range selectedSources {
							if s == f {
								exists = true
								break
							}
						}
						if !exists {
							selectedSources = append(selectedSources, f)
						}
					}
					refreshSourceList()
				})
			}
		}()
	})

	browseFolderBtn := widget.NewButtonWithIcon("Add Folder", theme.FolderIcon(), func() {
		go func() {
			folders, err := zenity.SelectFileMultiple(
				zenity.Title("Select Folders"),
				zenity.Directory(),
			)
			if err == nil && len(folders) > 0 {
				fyne.Do(func() {
					for _, f := range folders {
						exists := false
						for _, s := range selectedSources {
							if s == f {
								exists = true
								break
							}
						}
						if !exists {
							selectedSources = append(selectedSources, f)
						}
					}
					refreshSourceList()
				})
			}
		}()
	})

	clearBtn := widget.NewButtonWithIcon("Clear All", theme.ContentClearIcon(), func() {
		selectedSources = nil
		refreshSourceList()
	})
	clearBtn.Importance = widget.LowImportance

	browseBtns := container.NewVBox(browseFileBtn, browseFolderBtn, clearBtn)

	// Combine components into modern outer forms
	srcContainer := container.NewBorder(nil, nil, nil, browseBtns, listStack)

	customNameCheck.OnChanged = func(checked bool) {
		if checked {
			customNameEntry.Enable()
			appstate.SetInfo("Specify a custom name for the resulting archive, without extension.")
		} else {
			customNameEntry.Disable()
			appstate.SetInfo("Using default archive name.")
		}
		updateArchivePreview()
	}

	customNameEntry.OnChanged = func(_ string) {
		updateArchivePreview()
	}

	// Group the Custom Name Checkbox and text entry on a single line
	customNameContainer := container.NewBorder(nil, nil, customNameCheck, nil, customNameEntry)

	// Level Select
	levelSelect := widget.NewSelect([]string{"Store", "Fastest", "Fast", "Normal", "Maximum", "Ultra"}, nil)
	levelSelect.SetSelected("Normal")
	levelSelect.OnChanged = func(_ string) {
		appstate.SetInfo("Compression Level: Higher levels offer better compression but use more memory.")
	}

	// Compression Method Select
	methodSelect := widget.NewSelect([]string{"LZMA2", "LZMA", "PPMd", "BZip2", "Deflate", "Copy"}, nil)
	methodSelect.SetSelected("LZMA2")
	methodSelect.OnChanged = func(_ string) {
		appstate.SetInfo("Compression Method: Core algorithm used to compress data. LZMA2 is standard for 7z.")
	}

	// Dictionary, Word, Block Sizes
	dictSelect := widget.NewSelect([]string{"64 KB", "1 MB", "16 MB", "32 MB", "64 MB", "128 MB"}, nil)
	dictSelect.SetSelected("16 MB")
	wordSelect := widget.NewSelect([]string{"8", "16", "32", "64", "128", "273"}, nil)
	wordSelect.SetSelected("64")
	wordSelect.OnChanged = func(_ string) {
		appstate.SetInfo("Word size (fast bytes) determines the length of patterns to match; increasing it can improve compression on structured files but slows down compression speed.")
	}
	blockSelect := widget.NewSelect([]string{"Non-solid", "1 MB", "16 MB", "64 MB", "256 MB", "4 GB", "Solid"}, nil)
	blockSelect.SetSelected("Solid")
	blockSelect.OnChanged = func(_ string) {
		appstate.SetInfo("Determines how many files are compressed together. To extract one file, 7-Zip must decompress all files in the solid block. Under 'Non-Solid', each file is compressed separately resulting in fast extraction, but lower compression. Using a smaller solid block size (64 to 512 MB) is advisable when you need to frequently extract individual files from a large archive.")
	}

	// CPU Threads
	numCPU := runtime.NumCPU()
	threads := []string{}
	for i := 1; i <= numCPU; i++ {
		threads = append(threads, strconv.Itoa(i))
	}
	threadSelect := widget.NewSelect(threads, nil)
	threadSelect.SetSelected(strconv.Itoa(numCPU))
	threadSelect.OnChanged = func(_ string) { appstate.SetInfo(fmt.Sprintf("CPU Threads: Total available = %d", numCPU)) }

	/*
		// Simulated calculation based on dict size
		memCompLabel := widget.NewLabel("~150 MB")
		memDecompLabel := widget.NewLabel("~20 MB")
		dictSelect.OnChanged = func(_ string) {
			memCompLabel.SetText("Depends on Dict & Threads")
			memDecompLabel.SetText("Depends on Dict")
			setInfo("Dictionary Size: How much data is analyzed in memory for repetitions. The larger it is, higher the compression ratios and more the RAM required. Generally, 32MB-64MB is sufficient for most files, while 512MB+ is recommended for massive archives. Should be less than or equal to the total size of the files being compressed.")
		}
	*/

	// Update Mode
	updateSelect := widget.NewSelect([]string{"Add and replace files", "Update and add files", "Freshen existing files", "Synchronize files"}, nil)
	updateSelect.SetSelected("Add and replace files")
	updateSelect.OnChanged = func(s string) {
		switch s {
		case "Add and replace files":
			appstate.SetInfo("Add and Replace (Default): Adds all specified files to the archive. Overwrites if they already exist.")
		case "Update and add files":
			appstate.SetInfo("Update and Add: Adds new files and only updates files in the archive that are older.")
		case "Freshen existing files":
			appstate.SetInfo("Freshen: Only updates files that already exist in the archive. Does not add new files.")
		case "Synchronize files":
			appstate.SetInfo("Synchronize: Updates older files, adds new files, and deletes files from the archive that are no longer present on the disk.")
		}
	}

	sfxCheck.OnChanged = func(_ bool) {
		appstate.SetInfo("SFX: Creates a self-extracting executable in .exe format.")
		updateArchivePreview()
	}

	sharedCheck := widget.NewCheck("Compress shared files", nil)
	sharedCheck.OnChanged = func(_ bool) {
		appstate.SetInfo("Actively detects and groups identical or similar files together before compression and treats them as a single block of data. This allows to find repeating patterns across different files, leading to better results. Adding or extracting a single file from the middle of a large solid archive is slower.")
	}

	// Split
	splitEntry := widget.NewEntry()
	splitEntry.PlaceHolder = "e.g., 10M, 100M, 2G"
	splitEntry.OnChanged = func(_ string) {
		appstate.SetInfo("Choose to split the archive in chunks of specified size.")
	}

	// Encryption Options
	encCheck := widget.NewCheck("Enable Encryption", nil)
	passEntry := widget.NewPasswordEntry()
	passEntry.PlaceHolder = "Enter Password"
	passEntry.Disable()
	confirmEntry := widget.NewPasswordEntry()
	confirmEntry.PlaceHolder = "Confirm Password"
	confirmEntry.Disable()

	showPassCheck := widget.NewCheck("Show Password", nil)
	showPassCheck.Disable()
	encNameCheck := widget.NewCheck("Encrypt file names", nil)
	encNameCheck.Disable()

	showPassCheck.OnChanged = func(b bool) {
		passEntry.Password = !b
		confirmEntry.Password = !b
		passEntry.Refresh()
		confirmEntry.Refresh()
	}

	encCheck.OnChanged = func(b bool) {
		if b {
			passEntry.Enable()
			confirmEntry.Enable()
			showPassCheck.Enable()
			// Only allow name encryption if the format is 7z
			if formatSelect.Selected == "7z" {
				encNameCheck.Enable()
			}
		} else {
			passEntry.Disable()
			confirmEntry.Disable()
			showPassCheck.Disable()
			encNameCheck.Disable()
		}
		appstate.SetInfo("Encryption: Protect your archive with AES-256 password encryption.")
	}

	// Dynamic UI Toggle based on Archive Format
	formatSelect.OnChanged = func(s string) {
		appstate.SetInfo(fmt.Sprintf("Archive format set to: %s", s))

		// Reset all fields to Enabled as a baseline
		levelSelect.Enable()
		methodSelect.Enable()
		dictSelect.Enable()
		wordSelect.Enable()
		blockSelect.Enable()
		updateSelect.Enable()
		sfxCheck.Enable()
		sharedCheck.Enable()
		splitEntry.Enable()
		encCheck.Enable()
		if encCheck.Checked {
			passEntry.Enable()
			confirmEntry.Enable()
			encNameCheck.Enable()
		}

		/*
		* Keeping two separate switches for better readability:
		*   1 for compression method, and
		*   1 for others
		 */
		// Update Method options based on format
		switch s {
		case "7z":
			methodSelect.Options = []string{"LZMA2", "LZMA", "PPMd", "BZip2", "Deflate", "Copy"}
			methodSelect.SetSelected("LZMA2")
		case "zip":
			methodSelect.Options = []string{"Deflate", "Deflate64", "BZip2", "LZMA", "PPMd", "Copy"}
			methodSelect.SetSelected("Deflate")
		case "wim":
			methodSelect.Options = []string{"LZX", "LZMS", "Copy"}
			methodSelect.SetSelected("LZX")
		case "tar":
			methodSelect.Options = []string{"Copy"}
			methodSelect.SetSelected("Copy")
			methodSelect.Disable()
		case "gzip":
			methodSelect.Options = []string{"Deflate"}
			methodSelect.SetSelected("Deflate")
			methodSelect.Disable()
		case "bzip2":
			methodSelect.Options = []string{"BZip2"}
			methodSelect.SetSelected("BZip2")
			methodSelect.Disable()
		case "xz":
			methodSelect.Options = []string{"LZMA2"}
			methodSelect.SetSelected("LZMA2")
			methodSelect.Disable()
		}

		// Selectively disable based on format limitations
		switch s {
		case "zip":
			blockSelect.Disable() // ZIP does not support Solid blocks
			sfxCheck.Disable()
			sfxCheck.SetChecked(false)
			sharedCheck.Disable()
			sharedCheck.SetChecked(false)
			encNameCheck.Disable() // ZIP does not support encrypting file names
			encNameCheck.SetChecked(false)
		case "tar":
			// Tar doesn't compress or encrypt
			levelSelect.Disable()
			dictSelect.Disable()
			wordSelect.Disable()
			blockSelect.Disable()
			sfxCheck.Disable()
			sfxCheck.SetChecked(false)
			sharedCheck.Disable()
			sharedCheck.SetChecked(false)
			splitEntry.Disable()
			splitEntry.SetText("")
			encCheck.Disable()
			encCheck.SetChecked(false)
		case "gzip", "bzip2", "xz":
			// Single stream compressors
			blockSelect.Disable()
			sfxCheck.Disable()
			sfxCheck.SetChecked(false)
			sharedCheck.Disable()
			sharedCheck.SetChecked(false)
			splitEntry.Disable()
			splitEntry.SetText("")
			encCheck.Disable()
			encCheck.SetChecked(false)
			updateSelect.Disable() // Cannot update a stream easily
		case "wim":
			wordSelect.Disable()
			blockSelect.Disable()
			sfxCheck.Disable()
			sfxCheck.SetChecked(false)
			sharedCheck.Disable()
			sharedCheck.SetChecked(false)
			encCheck.Disable()
			encCheck.SetChecked(false)
		}

		updateArchivePreview()
	}

	// Execution Actions
	archiveBtn := widget.NewButtonWithIcon("Archive", theme.ConfirmIcon(), func() {
		appstate.StateMu.RLock()
		running := appstate.IsOperationRunning
		appstate.StateMu.RUnlock()
		if running {
			dialog.ShowError(fmt.Errorf("an operation is already running"), w)
			return
		}

		if len(selectedSources) == 0 {
			dialog.ShowError(fmt.Errorf("please select source files/folders"), w)
			return
		}

		// Ensure custom name is provided if the checkbox is checked
		if customNameCheck.Checked && strings.TrimSpace(customNameEntry.Text) == "" {
			dialog.ShowError(fmt.Errorf("please enter a custom archive name or uncheck the box"), w)
			return
		}

		// Catch folder selection on single-stream formats
		isSingleStream := formatSelect.Selected == "gzip" || formatSelect.Selected == "bzip2" || formatSelect.Selected == "xz"
		if isSingleStream {
			if len(selectedSources) > 1 {
				dialog.ShowError(fmt.Errorf("%s can only compress a single file. Please choose '7z' or 'zip' for multi-file operations", formatSelect.Selected), w)
				return
			}
			fInfo, err := os.Stat(selectedSources[0])
			if err == nil && fInfo.IsDir() {
				dialog.ShowError(fmt.Errorf("%s cannot compress directories directly. Please use 'tar' first or choose '7z'/'zip'", formatSelect.Selected), w)
				return
			}
		}

		if encCheck.Checked && passEntry.Text != confirmEntry.Text {
			dialog.ShowError(fmt.Errorf("passwords under encryption settings do not match"), w)
			return
		}

		// Extract custom name if checked
		customName := ""
		if customNameCheck.Checked {
			customName = strings.TrimSpace(customNameEntry.Text)
		}

		// Map options to 7z CLI
		args := engine.Build7zArgs(
			selectedSources,
			customName,
			formatSelect.Selected,
			levelSelect.Selected,
			methodSelect.Selected,
			dictSelect.Selected,
			wordSelect.Selected,
			blockSelect.Selected,
			threadSelect.Selected,
			updateSelect.Selected,
			sfxCheck.Checked,
			sharedCheck.Checked,
			splitEntry.Text,
			encCheck.Checked,
			passEntry.Text,
			encNameCheck.Checked,
		)

		// Safe fallback: append -spf2 if duplicate names exist across different directories
		if sys.HasDuplicateFilenames(selectedSources) {
			args = append(args, "-spf2")
		}

		// Switch to Status tab
		if appstate.Tabs != nil {
			appstate.Tabs.Select(domain.StatusTabRank)
		}
		// Passing arguments as args, title as "Compressing", window context as w, and nil for onSuccess callback
		engine.StartOperation(args, "Compressing", "", w, nil)
	})
	archiveBtn.Importance = widget.HighImportance

	// Initialize the Empty list or view state
	refreshSourceList()

	// Form Construction
	form := widget.NewForm(
		widget.NewFormItem("Source:", srcContainer),
		widget.NewFormItem("", customNameContainer),
		widget.NewFormItem("Output:", archivePreviewLabel),
		widget.NewFormItem("Archive format:", formatSelect),
		widget.NewFormItem("Compression level:", levelSelect),
		widget.NewFormItem("Compression method:", methodSelect),
		widget.NewFormItem("Dictionary size:", dictSelect),
		widget.NewFormItem("Word size:", wordSelect),
		widget.NewFormItem("Solid Block size:", blockSelect),
		widget.NewFormItem("CPU Threads:", threadSelect),
		/*
			widget.NewFormItem("Memory Compressing:", memCompLabel),
			widget.NewFormItem("Memory Decompressing:", memDecompLabel),
		*/
		widget.NewFormItem("Update mode:", updateSelect),
		widget.NewFormItem("Options:", container.NewVBox(sfxCheck, sharedCheck)),
		widget.NewFormItem("Split to volumes:", splitEntry),
		widget.NewFormItem("Encryption Options  →", encCheck),
		widget.NewFormItem("Password:", passEntry),
		widget.NewFormItem("Confirm:", confirmEntry),
		widget.NewFormItem("Enc. Settings:", container.NewHBox(showPassCheck, encNameCheck)),
	)

	return container.NewPadded(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Compress"),
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			container.NewHBox(
				layout.NewSpacer(),
				widget.NewButton("Cancel", func() {
					selectedSources = nil
					refreshSourceList()
				}),
				archiveBtn,
			),
		),
		nil,
		nil,
		container.NewVScroll(form),
	))
}