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
		infoBar.SetText("Ready.")
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
		statusLog.SetText("No operations running.")
		progressBar.SetValue(0)
	})
}

func main() {
	a := app.New()
	w := a.NewWindow("7-Zip GUI for Linux")
	w.Resize(fyne.NewSize(800, 650))

	// Bottom Info Bar
	infoBar = widget.NewLabel("Ready. Hover or click an option to see its description.")
	infoBar.Alignment = fyne.TextAlignCenter

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
		if isOperationRunning && t.Text != "Status" {
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

// --- DEPENDENCY CHECK ---

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
	// Browse Folder
	srcEntry := widget.NewEntry()
	srcEntry.PlaceHolder = "Select a file or folder to compress..."
	browseBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if uri != nil {
				srcEntry.SetText(uri.Path())
				setInfo("Selected path to archive: " + uri.Path())
			}
		}, w)
	})

	// Format
	formatSelect := widget.NewSelect([]string{"7z", "xz", "bzip2", "gzip", "tar", "zip", "wim"}, nil)
	formatSelect.SetSelected("7z")
	formatSelect.OnChanged = func(s string) { setInfo("Archive format: Determines the container and algorithms.") }

	// Level
	levelSelect := widget.NewSelect([]string{"Store", "Fastest", "Fast", "Normal", "Maximum", "Ultra"}, nil)
	levelSelect.SetSelected("Normal")
	levelSelect.OnChanged = func(s string) {
		setInfo("Compression Level: Higher levels offer better compression but use more memory.")
	}

	// Dictionary, Word, Block Sizes
	dictSelect := widget.NewSelect([]string{"64 KB", "1 MB", "16 MB", "32 MB", "64 MB", "128 MB"}, nil)
	dictSelect.SetSelected("16 MB")
	wordSelect := widget.NewSelect([]string{"8", "16", "32", "64", "128", "273"}, nil)
	wordSelect.SetSelected("64")
	wordSelect.OnChanged = func(s string) {
		setInfo("Word size (fast bytes) determines the length of patterns to match; increasing it can improve compression on structured files but slows down compression speed.")
	}
	blockSelect := widget.NewSelect([]string{"Non-solid", "1 MB", "16 MB", "64 MB", "256 MB", "4 GB", "Solid"}, nil)
	blockSelect.SetSelected("Solid")
	blockSelect.OnChanged = func(s string) {
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
	threadSelect.OnChanged = func(s string) { setInfo(fmt.Sprintf("CPU Threads: Total available = %d", numCPU)) }

	// Simulated calculation based on dict size
	memCompLabel := widget.NewLabel("~150 MB")
	memDecompLabel := widget.NewLabel("~20 MB")
	dictSelect.OnChanged = func(s string) {
		memCompLabel.SetText("Depends on Dict & Threads")
		setInfo("Dictionary Size: How much data is analyzed in memory for repetitions. The larger it is, higher the compression ratios and more the RAM required. Generally, 32MB-64MB is sufficient for most files, while 512MB+ is recommended for massive archives. Should be less than or equal to the total size of the files being compressed.")
	}

	// Update Mode
	updateSelect := widget.NewSelect([]string{"Add and replace files", "Update and add files", "Freshen existing files", "Synchronize files"}, nil)
	updateSelect.SetSelected("Add and replace files")
	/* show this info OnChanged:
	Add and Replace (Default): Adds all specified files to the archive. If a file exists, it overwrites it, regardless of whether it is newer or older than the archived version.
	Update and Add: Adds new files and only updates files in the archive that are older than the corresponding files on the disk.
	Freshen: Only updates files that already exist in the archive. It does not add new files to the archive.
	Synchronize: Updates older files, adds new files, and deletes files from the archive that are no longer present on the disk.
	*/

	// SFX
	sfxCheck := widget.NewCheck("Create SFX archive", func(b bool) { setInfo("SFX: Creates a self-extracting executable.") })
	sharedCheck := widget.NewCheck("Compress shared files", nil)

	// Split
	splitEntry := widget.NewEntry()
	splitEntry.PlaceHolder = "e.g., 10M, 100M, 2G"
	splitEntry.OnChanged = func(s string) {
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

	showPassCheck := widget.NewCheck("Show Password", func(b bool) {
		passEntry.Password = !b
		confirmEntry.Password = !b
		passEntry.Refresh()
		confirmEntry.Refresh()
	})
	showPassCheck.Disable()

	encNameCheck := widget.NewCheck("Encrypt file names", nil)
	encNameCheck.Disable()

	encCheck.OnChanged = func(b bool) {
		if b {
			passEntry.Enable()
			confirmEntry.Enable()
			showPassCheck.Enable()
			encNameCheck.Enable()
		} else {
			passEntry.Disable()
			confirmEntry.Disable()
			showPassCheck.Disable()
			encNameCheck.Disable()
		}
		setInfo("Encryption: Protect your archive with AES-256 password encryption.")
	}

	// Buttons
	archiveBtn := widget.NewButtonWithIcon("Archive", theme.ConfirmIcon(), func() {
		if srcEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("please select a source file/folder"), w)
			return
		}
		if encCheck.Checked && passEntry.Text != confirmEntry.Text {
			dialog.ShowError(fmt.Errorf("passwords do not match"), w)
			return
		}

		// Map options to 7z CLI
		args := build7zArgs(srcEntry.Text, formatSelect.Selected, levelSelect.Selected, threadSelect.Selected, sfxCheck.Checked, encCheck.Checked, passEntry.Text, encNameCheck.Checked, splitEntry.Text)
		tabs.SelectIndex(2) // Switch to Status tab
		startOperation(args, "Compressing")
	})
	archiveBtn.Importance = widget.HighImportance

	// Form Layout
	form := widget.NewForm(
		widget.NewFormItem("Source:", container.NewBorder(nil, nil, nil, browseBtn, srcEntry)),
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

	return container.NewVScroll(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		container.NewHBox(layout.NewSpacer(), widget.NewButton("Cancel", func() { srcEntry.SetText("") }), archiveBtn),
	)))
}

func buildExtractTab(w fyne.Window) fyne.CanvasObject {
	srcEntry := widget.NewEntry()
	destEntry := widget.NewEntry()

	srcBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		dialog.ShowFileOpen(func(uri fyne.URIReadCloser, err error) {
			if uri != nil {
				srcEntry.SetText(uri.URI().Path())
			}
		}, w)
	})

	destBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if uri != nil {
				destEntry.SetText(uri.Path())
			}
		}, w)
	})

	extractBtn := widget.NewButtonWithIcon("Extract", theme.DownloadIcon(), func() {
		if srcEntry.Text == "" || destEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("select both archive and destination"), w)
			return
		}

		args := []string{"x", srcEntry.Text, "-o" + destEntry.Text, "-bsp1", "-y"}
		tabs.SelectIndex(2)
		startOperation(args, "Extracting")
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
	progressBar = widget.NewProgressBar()

	pauseBtn = widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), func() {
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

func build7zArgs(src, format, level, threads string, sfx, enc bool, pass string, encName bool, split string) []string {
	// Destination filename logic
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	ext := "." + format
	if sfx {
		ext = ".exe"
	}
	dest := filepath.Join(filepath.Dir(src), base+ext)

	// -bsp1 enables progress output to stdout, -t Map format
	args := []string{"a", dest, src, "-bsp1", "-t" + format}

	// Map level (-mx0 to -mx9), -mmt Map threads
	lvlMap := map[string]string{"Store": "0", "Fastest": "1", "Fast": "3", "Normal": "5", "Maximum": "7", "Ultra": "9"}
	args = append(args, "-mx="+lvlMap[level], "-mmt="+threads)

	// Map Split
	if split != "" {
		args = append(args, "-v"+split)
	}
	if sfx {
		args = append(args, "-sfx")
	}
	if enc && pass != "" {
		args = append(args, "-p"+pass)
		if encName {
			args = append(args, "-mhe=on")
		}
	}

	return args
}

func startOperation(args []string, mode string) {
	currentCmd = exec.Command("7z", args...)

	stdout, err := currentCmd.StdoutPipe()
	if err != nil {
		setFinalStatus(fmt.Sprintf("Error connecting to output: %v", err))
		return
	}

	// Lock UI functionality
	isOperationRunning = true
	isPaused = false
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
		isOperationRunning = false
		pauseBtn.Disable()
		cancelBtn.Disable()
		setFinalStatus(fmt.Sprintf("Failed to start 7-Zip: %v", err))
		return
	}

	// Shared state updated by byte-reader and read by 1-second ticker
	var currentPercent float64
	ticker := time.NewTicker(1 * time.Second)

	// UI Update Routine (Once per second)
	go func() {
		for range ticker.C {
			if !isOperationRunning {
				return
			}
			if isPaused {
				continue
			}

			// Refresh Fyne widgets
			progressBar.SetValue(currentPercent / 100.0)
			elapsed := time.Since(startTime).Round(time.Second)
			statusLog.SetText(fmt.Sprintf("Status: Running\nElapsed Time: %s\nProgress: %.0f%%", elapsed, currentPercent))
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
						currentPercent = val
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
		isOperationRunning = false
		pauseBtn.Disable()
		cancelBtn.Disable()

		if err != nil {
			if err.Error() == "signal: killed" {
				setFinalStatus("Operation was cancelled by user.")
			} else {
				setFinalStatus(fmt.Sprintf("Finished with errors: %v", err))
			}
		} else {
			progressBar.SetValue(1.0)
			setFinalStatus("Operation completed successfully!")
			setInfo("Done.")
		}
		currentCmd = nil
	}()
}
