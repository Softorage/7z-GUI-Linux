package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	infoBar     *widget.Label
	tabs        *container.AppTabs
	statusLog   *widget.Label
	progressBar *widget.ProgressBar
	pauseBtn    *widget.Button
	cancelBtn   *widget.Button

	currentCmd         *exec.Cmd
	isPaused           bool
	isOperationRunning bool
	currentPercent     float64
	stateMu            sync.RWMutex // Protects state across goroutines to prevent data races

	// Timers and mutexes to auto-clear UI text after 6 seconds
	infoBarTimer *time.Timer
	infoMu       sync.Mutex

	statusTimer *time.Timer
	statusMu    sync.Mutex
)

// setInfo updates the bottom info bar and sets a 6-second timer to clear it.
func setInfo(text string) {
	infoMu.Lock()
	defer infoMu.Unlock()
	infoBar.SetText(text)

	if infoBarTimer != nil {
		infoBarTimer.Stop()
	}
	infoBarTimer = time.AfterFunc(6*time.Second, func() {
		stateMu.RLock()
		running := isOperationRunning
		paused := isPaused
		stateMu.RUnlock()

		if running {
			if paused {
				infoBar.SetText("Operation paused.")
			} else {
				infoBar.SetText("Operation in progress...")
			}
		} else {
			infoBar.SetText("Ready. Hover or click an option to see its description.")
		}
	})
}

// setFinalStatus updates the status log when an operation ends, clearing it after 6 seconds.
func setFinalStatus(text string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusLog.SetText(text)

	if statusTimer != nil {
		statusTimer.Stop()
	}
	statusTimer = time.AfterFunc(6*time.Second, func() {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()
		if !running {
			statusLog.SetText("No operations running.")
			progressBar.SetValue(0)
		}
	})
}

func main() {
	a := app.New()
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(800, 650))

	// Bottom Info Bar
	infoBar = widget.NewLabel("Ready. Hover or click an option to see its description.")
	infoBar.Alignment = fyne.TextAlignCenter
	infoBar.Wrapping = fyne.TextWrapWord // Properly wraps text instead of resizing window

	// Build Tabs
	compressTab := buildCompressTab(w)
	extractTab := buildExtractTab(w)
	statusTab := buildStatusTab(w)

	tabs = container.NewAppTabs(
		container.NewTabItem("Compress", compressTab),
		container.NewTabItem("Extract", extractTab),
		container.NewTabItem("Status", statusTab),
	)

	// Intercept tab switching to lock user on Status tab during operations
	tabs.OnSelected = func(t *container.TabItem) {
		stateMu.RLock()
		running := isOperationRunning
		stateMu.RUnlock()

		if running && t.Text != "Status" {
			setInfo("Action locked: Operation currently in progress.")
			tabs.SelectIndex(2) // Force back to Status (index 2)
		}
	}

	// Layout Main Window
	w.SetContent(container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), infoBar),
		nil,
		nil,
		tabs,
	))

	// Dependency check
	go checkDependencies(w)

	w.ShowAndRun()
}

func checkDependencies(w fyne.Window) {
	_, err := exec.LookPath("7z")
	if err != nil {
		dialog.ShowConfirm("7-Zip Not Found",
			"The '7z' command was not found.\nWould you like to install it using your system's package manager?\n(This will prompt for root password)",
			func(install bool) {
				if install {
					install7Zip(w)
				} else {
					os.Exit(1)
				}
			}, w)
	}
}

func install7Zip(w fyne.Window) {
	setInfo("Installing p7zip...")
	var cmd *exec.Cmd

	// Basic distro detection
	if _, err := exec.LookPath("apt-get"); err == nil {
		cmd = exec.Command("pkexec", "apt-get", "install", "-y", "p7zip-full")
	} else if _, err := exec.LookPath("dnf"); err == nil {
		cmd = exec.Command("pkexec", "dnf", "install", "-y", "p7zip")
	} else if _, err := exec.LookPath("pacman"); err == nil {
		cmd = exec.Command("pkexec", "pacman", "-S", "--noconfirm", "p7zip")
	} else {
		dialog.ShowError(fmt.Errorf("unsupported package manager"), w)
		return
	}

	err := cmd.Run()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to install 7-Zip: %v", err), w)
	} else {
		dialog.ShowInformation("Success", "7-Zip installed successfully!", w)
		setInfo("7-Zip installation complete.")
	}
}

// --- TABS ---

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

	// Simulated calculation based on dict size
	memCompLabel := widget.NewLabel("~150 MB")
	memDecompLabel := widget.NewLabel("~20 MB")
	dictSelect.OnChanged = func(_ string) {
		memCompLabel.SetText("Depends on Dict & Threads")
		memDecompLabel.SetText("Depends on Dict")
		setInfo("Dictionary Size: How much data is analyzed in memory for repetitions. The larger it is, higher the compression ratios and more the RAM required. Generally, 32MB-64MB is sufficient for most files, while 512MB+ is recommended for massive archives. Should be less than or equal to the total size of the files being compressed.")
	}

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

		// Catch folder selection on single-stream formats
		fInfo, err := os.Stat(srcEntry.Text)
		if err == nil && fInfo.IsDir() {
			if formatSelect.Selected == "gzip" || formatSelect.Selected == "bzip2" || formatSelect.Selected == "xz" {
				dialog.ShowError(fmt.Errorf("%s cannot compress directories directly. Please use 'tar' first or choose '7z'/'zip'", formatSelect.Selected), w)
				return
			}
		}

		if encCheck.Checked && passEntry.Text != confirmEntry.Text {
			dialog.ShowError(fmt.Errorf("passwords do not match"), w)
			return
		}

		// Map options to 7z CLI
		args := build7zArgs(
			srcEntry.Text,
			formatSelect.Selected,
			levelSelect.Selected,
			threadSelect.Selected,
			updateSelect.Selected,
			sfxCheck.Checked,
			encCheck.Checked,
			passEntry.Text,
			encNameCheck.Checked,
			splitEntry.Text,
			dictSelect.Selected,
			wordSelect.Selected,
			blockSelect.Selected,
			sharedCheck.Checked,
		)

		tabs.SelectIndex(2) // Switch to Status tab
		startOperation(args, "Compressing", w)
	})
	archiveBtn.Importance = widget.HighImportance

	// Form Layout
	form := widget.NewForm(
		widget.NewFormItem("Source:", container.NewBorder(nil, nil, nil, browseBtns, srcEntry)),
		widget.NewFormItem("Archive format:", formatSelect),
		widget.NewFormItem("Compression level:", levelSelect),
		widget.NewFormItem("Dictionary size:", dictSelect),
		widget.NewFormItem("Word size:", wordSelect),
		widget.NewFormItem("Solid Block size:", blockSelect),
		widget.NewFormItem("CPU Threads:", threadSelect),
		widget.NewFormItem("Memory Compressing:", memCompLabel),
		widget.NewFormItem("Memory Decompressing:", memDecompLabel),
		widget.NewFormItem("Update mode:", updateSelect),
		widget.NewFormItem("Options:", container.NewVBox(sfxCheck, sharedCheck)),
		widget.NewFormItem("Split to volumes:", splitEntry),
		widget.NewFormItem("--- Encryption Options ---", encCheck),
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

func buildExtractTab(w fyne.Window) fyne.CanvasObject {
	srcEntry := widget.NewEntry()
	destEntry := widget.NewEntry()

	srcBtn := widget.NewButtonWithIcon("", theme.FileIcon(), func() {
		d := dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
			if err == nil && uri != nil {
				srcEntry.SetText(uri.URI().Path())
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

		args := []string{"x", srcEntry.Text, "-o" + destEntry.Text, "-bsp1", "-y"}
		tabs.SelectIndex(2)
		startOperation(args, "Extracting", w)
	})
	extractBtn.Importance = widget.HighImportance

	form := widget.NewForm(
		widget.NewFormItem("Archive File:", container.NewBorder(nil, nil, nil, srcBtn, srcEntry)),
		widget.NewFormItem("Extract To:", container.NewBorder(nil, nil, nil, destBtn, destEntry)),
	)

	return container.NewPadded(container.NewVBox(
		form,
		layout.NewSpacer(),
		container.NewHBox(layout.NewSpacer(), extractBtn),
	))
}

func buildStatusTab(w fyne.Window) fyne.CanvasObject {
	statusLog = widget.NewLabel("No operations running.")
	statusLog.Wrapping = fyne.TextWrapWord
	progressBar = widget.NewProgressBar()

	pauseBtn = widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), func() {
		stateMu.Lock()
		defer stateMu.Unlock()
		if currentCmd != nil && currentCmd.Process != nil {
			if !isPaused {
				// Send SIGSTOP to pause Linux process
				currentCmd.Process.Signal(syscall.SIGSTOP)
				isPaused = true
				pauseBtn.SetText("Resume")
				pauseBtn.SetIcon(theme.MediaPlayIcon())
				setInfo("Operation Paused.")
				statusLog.SetText("Status: Paused")
			} else {
				// Send SIGCONT to resume
				currentCmd.Process.Signal(syscall.SIGCONT)
				isPaused = false
				pauseBtn.SetText("Pause")
				pauseBtn.SetIcon(theme.MediaPauseIcon())
				setInfo("Operation Resumed.")
			}
		}
	})
	pauseBtn.Disable()

	cancelBtn = widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		stateMu.Lock()
		defer stateMu.Unlock()
		if currentCmd != nil && currentCmd.Process != nil {
			currentCmd.Process.Kill()
			setInfo("Operation Cancelled.")
		}
	})
	cancelBtn.Disable()

	return container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Progress Statistics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		progressBar,
		statusLog,
		layout.NewSpacer(),
		container.NewHBox(layout.NewSpacer(), pauseBtn, cancelBtn),
	))
}

// --- LOGIC MAPPER & EXECUTION ---

func build7zArgs(src, format string, level string, threads, update string, sfx bool, enc bool, pass string, encName bool, split string, dictSize string, wordSize, blockSize string, shared bool) []string {

	// Standardize extensions for common formats mapped by 7-Zip
	extMap := map[string]string{
		"7z":    ".7z",
		"xz":    ".xz",
		"bzip2": ".bz2",
		"gzip":  ".gz",
		"tar":   ".tar",
		"zip":   ".zip",
		"wim":   ".wim",
	}

	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	ext, ok := extMap[format]
	if !ok {
		ext = "." + format
	}

	// Only 7z truly supports SFX cleanly via standard 7-Zip module
	if sfx && format == "7z" {
		ext = ".exe"
	}
	dest := filepath.Join(filepath.Dir(src), base+ext)

	// Determine command line action (a = Add, u = Update)
	cmdAction := "a"
	var updateSwitches []string

	if format != "tar" && format != "gzip" && format != "bzip2" && format != "xz" {
		if update != "Add and replace files" {
			cmdAction = "u"
			if update == "Freshen existing files" {
				// -uw0 avoids adding new files that are on disk only
				updateSwitches = append(updateSwitches, "-uw0")
			} else if update == "Synchronize files" {
				// -up0 deletes files from the archive that are missing on disk
				updateSwitches = append(updateSwitches, "-up0")
			}
		}
	}

	// -bsp1 enables progress output to stdout, -t Map format
	args := []string{cmdAction, dest, src, "-bsp1", "-t" + format}
	args = append(args, updateSwitches...)

	// Only apply compression level if the format supports it (tar does not)
	if format != "tar" {
		lvlMap := map[string]string{"Store": "0", "Fastest": "1", "Fast": "3", "Normal": "5", "Maximum": "7", "Ultra": "9"}
		args = append(args, "-mx="+lvlMap[level])

		// Dictionary and Word size are only reliably scalable across 7z and xz.
		// Exposing large dictionary values to deflaters (zip/gzip) will prompt "Unsupported Method" errors in 7z CLI
		if format == "7z" || format == "xz" {
			dictMap := map[string]string{
				"64 KB":  "64k",
				"1 MB":   "1m",
				"16 MB":  "16m",
				"32 MB":  "32m",
				"64 MB":  "64m",
				"128 MB": "128m",
			}
			if d, ok := dictMap[dictSize]; ok {
				args = append(args, "-md="+d)
			}

			if wordSize != "" {
				args = append(args, "-mfb="+wordSize)
			}
		}
	}

	// Solid Block Size and Solid Sorting (-mqs) is primarily a 7z concept
	if format == "7z" {
		blockMap := map[string]string{
			"Non-solid": "off",
			"1 MB":      "1m",
			"16 MB":     "16m",
			"64 MB":     "64m",
			"256 MB":    "256m",
			"4 GB":      "4g",
			"Solid":     "on",
		}
		if b, ok := blockMap[blockSize]; ok {
			args = append(args, "-ms="+b)
		}

		if shared {
			// Instructs solid mode to sort by extension, intelligently matching similar files into the same data blocks
			args = append(args, "-mqs=on")
		}
	}

	// Compress shared files allows the system to read files opened/locked for writing by other applications
	if shared {
		args = append(args, "-ssw")
	}

	// Map threads (generally accepted across the board, ignored if unsupported)
	args = append(args, "-mmt="+threads)

	// Map Split (Not supported by tar/gzip/bzip2/xz)
	if split != "" && format != "tar" && format != "gzip" && format != "bzip2" && format != "xz" {
		args = append(args, "-v"+split)
	}

	if sfx && format == "7z" {
		args = append(args, "-sfx")
	}

	// Map Encryption (Only supported by 7z and zip)
	if enc && pass != "" && (format == "7z" || format == "zip") {
		args = append(args, "-p"+pass)

		// Only 7z supports header/filename encryption natively this way
		if encName && format == "7z" {
			args = append(args, "-mhe=on")
		}
	}

	return args
}

func startOperation(args []string, mode string, w fyne.Window) {
	currentCmd = exec.Command("7z", args...)

	stdout, err := currentCmd.StdoutPipe()
	if err != nil {
		setFinalStatus(fmt.Sprintf("Error connecting to output: %v", err))
		return
	}

	// Lock UI functionality safely
	stateMu.Lock()
	isOperationRunning = true
	isPaused = false
	currentPercent = 0
	stateMu.Unlock()

	progressBar.SetValue(0)

	// Set buttons active
	pauseBtn.SetText("Pause")
	pauseBtn.SetIcon(theme.MediaPauseIcon())
	pauseBtn.Enable()
	cancelBtn.Enable()

	setInfo(fmt.Sprintf("%s started...", mode))
	startTime := time.Now()

	err = currentCmd.Start()
	if err != nil {
		stateMu.Lock()
		isOperationRunning = false
		currentCmd = nil
		stateMu.Unlock()

		pauseBtn.Disable()
		cancelBtn.Disable()
		setFinalStatus(fmt.Sprintf("Failed to start 7-Zip: %v", err))
		return
	}

	ticker := time.NewTicker(1 * time.Second)

	// UI Update Routine (Once per second)
	go func() {
		for range ticker.C {
			stateMu.RLock()
			running := isOperationRunning
			paused := isPaused
			pct := currentPercent
			stateMu.RUnlock()

			if !running {
				return
			}
			if paused {
				continue
			}

			// Refresh Fyne widgets
			progressBar.SetValue(pct / 100.0)
			elapsed := time.Since(startTime).Round(time.Second)
			statusLog.SetText(fmt.Sprintf("Status: Running\nElapsed Time: %s\nProgress: %.0f%%", elapsed, pct))
		}
	}()

	// Sub-process I/O Reader Routine (Parses stdout byte-by-byte for exact progress)
	go func() {
		defer ticker.Stop()
		re := regexp.MustCompile(`(\d+)%`)
		buf := make([]byte, 1)
		var currentLine []byte

		// 7-zip relies heavily on \b (backspaces) and \r to rewrite lines visually.
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				b := buf[0]
				if b == '\r' || b == '\n' || b == '\b' {
					str := string(currentLine)
					matches := re.FindStringSubmatch(str)
					if len(matches) > 1 {
						val, _ := strconv.ParseFloat(matches[1], 64)
						stateMu.Lock()
						currentPercent = val
						stateMu.Unlock()
					}
					currentLine = currentLine[:0]
				} else {
					currentLine = append(currentLine, b)
				}
			}
			if readErr != nil {
				break
			}
		}

		err = currentCmd.Wait()

		// Unlock the UI
		stateMu.Lock()
		isOperationRunning = false
		currentCmd = nil
		stateMu.Unlock()

		pauseBtn.Disable()
		cancelBtn.Disable()

		if err != nil {
			if err.Error() == "signal: killed" {
				setFinalStatus("Operation was cancelled by user.")
				setInfo("Cancelled.")
			} else {
				setFinalStatus(fmt.Sprintf("Finished with errors: %v", err))
				setInfo("Error during operation.")
			}
		} else {
			progressBar.SetValue(1.0)
			setFinalStatus("Operation completed successfully!")
			setInfo("Done.")
		}
	}()
}
