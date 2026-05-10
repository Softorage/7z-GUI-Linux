package main

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
)

func buildCompressTab(w fyne.Window) fyne.CanvasObject {
	// Browse Folder or File
	srcEntry := widget.NewEntry()
	srcEntry.PlaceHolder = "Select a file or folder to compress..."

	browseFileBtn := widget.NewButtonWithIcon("", theme.FileIcon(), func() {
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err == nil && uri != nil {
				srcEntry.SetText(uri.URI().Path())
				setInfo("Selected file to archive: " + uri.URI().Path())
				uri.Close() // Essential for Fyne: Clean up internal file handles.
			}
		}, w)
		windowSize := w.Canvas().Size()
		d.Resize(fyne.NewSize(windowSize.Width*0.8, windowSize.Height*0.8))
		d.Show()
	})

	browseFolderBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				srcEntry.SetText(uri.Path())
				setInfo("Selected folder to archive: " + uri.Path())
			}
		}, w)
		windowSize := w.Canvas().Size()
		d.Resize(fyne.NewSize(windowSize.Width*0.8, windowSize.Height*0.8))
		d.Show()
	})

	browseBtns := container.NewHBox(browseFileBtn, browseFolderBtn)

	// Custom Archive Name Checkbox and Entry
	customNameEntry := widget.NewEntry()
	customNameEntry.PlaceHolder = "Enter custom archive name (without extension)"
	customNameEntry.Disable()

	customNameCheck := widget.NewCheck("Custom Name", nil)
	customNameCheck.OnChanged = func(checked bool) {
		if checked {
			customNameEntry.Enable()
			setInfo("Specify a custom name for the resulting archive.")
		} else {
			customNameEntry.SetText("")
			customNameEntry.Disable()
			setInfo("Using default archive name.")
		}
	}

	// Group the Custom Name Checkbox and text entry on a single line
	customNameContainer := container.NewBorder(nil, nil, customNameCheck, nil, customNameEntry)

	// Declare the widgets, set default state, and the info to display on change
	// Format
	formatSelect := widget.NewSelect([]string{"7z", "xz", "bzip2", "gzip", "tar", "zip", "wim"}, nil)
	formatSelect.SetSelected("7z")
	formatSelect.OnChanged = func(_ string) { setInfo("Archive format: Determines the container and algorithms.") }

	// Level
	levelSelect := widget.NewSelect([]string{"Store", "Fastest", "Fast", "Normal", "Maximum", "Ultra"}, nil)
	levelSelect.SetSelected("Normal")
	levelSelect.OnChanged = func(_ string) {
		setInfo("Compression Level: Higher levels offer better compression but use more memory.")
	}

	// Compression Method
	methodSelect := widget.NewSelect([]string{"LZMA2", "LZMA", "PPMd", "BZip2", "Deflate", "Copy"}, nil)
	methodSelect.SetSelected("LZMA2")
	methodSelect.OnChanged = func(_ string) {
		setInfo("Compression Method: Core algorithm used to compress data. LZMA2 is standard for 7z.")
	}

	// Dictionary, Word, Block Sizes
	dictSelect := widget.NewSelect([]string{"64 KB", "1 MB", "16 MB", "32 MB", "64 MB", "128 MB"}, nil)
	dictSelect.SetSelected("16 MB")
	wordSelect := widget.NewSelect([]string{"8", "16", "32", "64", "128", "273"}, nil)
	wordSelect.SetSelected("64")
	wordSelect.OnChanged = func(_ string) {
		setInfo("Word size (fast bytes) determines the length of patterns to match; increasing it can improve compression on structured files but slows down compression speed.")
	}
	blockSelect := widget.NewSelect([]string{"Non-solid", "1 MB", "16 MB", "64 MB", "256 MB", "4 GB", "Solid"}, nil)
	blockSelect.SetSelected("Solid")
	blockSelect.OnChanged = func(_ string) {
		setInfo("Determines how many files are compressed together. To extract one file, 7-Zip must decompress all files in the solid block. Under 'Non-Solid', each file is compressed separately resulting in fast extraction, but lower compression. Using a smaller solid block size (64 to 512 MB) is advisable when you need to frequently extract individual files from a large archive.")
	}

	// CPU Threads
	numCPU := runtime.NumCPU()
	threads := []string{}
	for i := 1; i <= numCPU; i++ {
		threads = append(threads, strconv.Itoa(i))
	}
	threadSelect := widget.NewSelect(threads, nil)
	threadSelect.SetSelected(strconv.Itoa(numCPU))
	threadSelect.OnChanged = func(_ string) { setInfo(fmt.Sprintf("CPU Threads: Total available = %d", numCPU)) }

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
			setInfo("Add and Replace (Default): Adds all specified files to the archive. Overwrites if they already exist.")
		case "Update and add files":
			setInfo("Update and Add: Adds new files and only updates files in the archive that are older.")
		case "Freshen existing files":
			setInfo("Freshen: Only updates files that already exist in the archive. Does not add new files.")
		case "Synchronize files":
			setInfo("Synchronize: Updates older files, adds new files, and deletes files from the archive that are no longer present on the disk.")
		}
	}

	// SFX
	sfxCheck := widget.NewCheck("Create SFX archive", nil)
	sfxCheck.OnChanged = func(_ bool) { setInfo("SFX: Creates a self-extracting executable in .exe format.") }

	sharedCheck := widget.NewCheck("Compress shared files", nil)
	sharedCheck.OnChanged = func(_ bool) {
		setInfo("Actively detects and groups identical or similar files together before compression and treats them as a single block of data. This allows to find repeating patterns across different files, leading to better results. Adding or extracting a single file from the middle of a large solid archive is slower.")
	}

	// Split
	splitEntry := widget.NewEntry()
	splitEntry.PlaceHolder = "e.g., 10M, 100M, 2G"
	splitEntry.OnChanged = func(_ string) {
		setInfo("Choose to split the archive in chunks of specified size.")
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
		setInfo("Encryption: Protect your archive with AES-256 password encryption.")
	}

	// Dynamic UI Toggle based on Archive Format
	formatSelect.OnChanged = func(s string) {
		setInfo(fmt.Sprintf("Archive format set to: %s", s))

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
	}

	// Buttons
	archiveBtn := widget.NewButtonWithIcon("Archive", theme.ConfirmIcon(), func() {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()
		if running {
			dialog.ShowError(fmt.Errorf("an operation is already running"), w)
			return
		}

		if srcEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("please select a source file/folder"), w)
			return
		}

		// Ensure custom name is provided if the checkbox is checked
		if customNameCheck.Checked && strings.TrimSpace(customNameEntry.Text) == "" {
			dialog.ShowError(fmt.Errorf("please enter a custom archive name or uncheck the box"), w)
			return
		}

		// Catch folder selection on single-stream formats
		fInfo, err := os.Stat(srcEntry.Text)
		if err == nil && fInfo.IsDir() {
			if formatSelect.Selected == "gzip" || formatSelect.Selected == "bzip2" || formatSelect.Selected == "xz" {
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
		args := build7zArgs(
			srcEntry.Text,
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

		// Switch to Status tab
		tabs.SelectIndex(2)
		// Passing arguments as args, title as "Compressing", window context as w, and nil for onSuccess callback
		startOperation(args, "Compressing", w, nil)
	})
	archiveBtn.Importance = widget.HighImportance

	// Form Layout
	form := widget.NewForm(
		widget.NewFormItem("Source:", container.NewBorder(nil, nil, nil, browseBtns, srcEntry)),
		widget.NewFormItem("", customNameContainer),
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
		nil,
		container.NewVBox(
			widget.NewSeparator(),
			container.NewHBox(
				layout.NewSpacer(),
				widget.NewButton("Cancel", func() { srcEntry.SetText("") }),
				archiveBtn,
			),
		),
		nil,
		nil,
		container.NewVScroll(form),
	))
}
