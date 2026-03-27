package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
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

	currentCmd  *exec.Cmd
	isPaused    bool
)

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
	infoBar.SetText("Installing p7zip...")
	var cmd *exec.Cmd

	// Basic distro detection
	if _, err := exec.LookPath("apt-get"); err == nil {
		cmd = exec.Command("pkexec", "apt-get", "install", "-y", "p7zip-full")
	} else if _, err := exec.LookPath("dnf"); err == nil {
		cmd = exec.Command("pkexec", "dnf", "install", "-y", "p7zip")
	} else if _, err := exec.LookPath("pacman"); err == nil {
		cmd = exec.Command("pkexec", "pacman", "-S", "--noconfirm", "p7zip")
	} else {
		dialog.ShowError(fmt.Errorf("unsupported package manager. please install p7zip manually"), w)
		return
	}

	err := cmd.Run()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to install 7-Zip: %v", err), w)
	} else {
		dialog.ShowInformation("Success", "7-Zip installed successfully!", w)
		infoBar.SetText("Ready.")
	}
}

// --- TABS ---

func buildCompressTab(w fyne.Window) fyne.CanvasObject {
	// 1. Browse Folder
	srcEntry := widget.NewEntry()
	srcEntry.PlaceHolder = "Select a file or folder to compress..."
	browseBtn := widget.NewButtonWithIcon("", theme.FolderIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if uri != nil {
				srcEntry.SetText(uri.Path())
				infoBar.SetText("Selected path to archive: " + uri.Path())
			}
		}, w)
	})

	// 2. Format
	formatSelect := widget.NewSelect([]string{"7z", "xz", "bzip2", "gzip", "tar", "zip", "wim"}, nil)
		formatSelect.SetSelected("7z")
			formatSelect.OnChanged = func(s string) { infoBar.SetText("Archive format: Determines the container and default compression algorithms.") }

			// 3. Level
			levelSelect := widget.NewSelect([]string{"Store", "Fastest", "Fast", "Normal", "Maximum", "Ultra"}, nil)
			levelSelect.SetSelected("Normal")
			levelSelect.OnChanged = func(s string) { infoBar.SetText("Compression Level: Higher levels offer better compression but take longer and use more memory.") }

			// 4 & 5 & 6. Dictionary, Word, Block Sizes
			dictSelect := widget.NewSelect([]string{"64 KB", "1 MB", "16 MB", "32 MB", "64 MB", "128 MB"}, nil)
			dictSelect.SetSelected("16 MB")
			wordSelect := widget.NewSelect([]string{"8", "16", "32", "64", "128", "273"}, nil)
			wordSelect.SetSelected("64")
			blockSelect := widget.NewSelect([]string{"Non-solid", "1 MB", "16 MB", "64 MB", "256 MB", "4 GB", "Solid"}, nil)
			blockSelect.SetSelected("Solid")

			// 7. CPU Threads
			numCPU := runtime.NumCPU()
			threads :=[]string{}
			for i := 1; i <= numCPU; i++ {
				threads = append(threads, strconv.Itoa(i))
			}
			threadSelect := widget.NewSelect(threads, nil)
			threadSelect.SetSelected(strconv.Itoa(numCPU))
			threadSelect.OnChanged = func(s string) { infoBar.SetText(fmt.Sprintf("CPU Threads: Limits core usage. Total available: %d", numCPU)) }

			// 8 & 9. Memory Usage (Simulated calculation based on dict size)
			memCompLabel := widget.NewLabel("~150 MB")
			memDecompLabel := widget.NewLabel("~20 MB")
			dictSelect.OnChanged = func(s string) {
				memCompLabel.SetText("Depends on Dict & Threads (Simulated Update)")
				infoBar.SetText("Dictionary Size: The larger it is, the more RAM required for compression and decompression.")
			}

			// 10. Update Mode
			updateSelect := widget.NewSelect([]string{"Add and replace files", "Update and add files", "Freshen existing files", "Synchronize files"}, nil)
			updateSelect.SetSelected("Add and replace files")

			// 11, 12. Checkboxes
			sfxCheck := widget.NewCheck("Create SFX archive", func(b bool) { infoBar.SetText("SFX: Creates a self-extracting executable instead of a standard archive.") })
			sharedCheck := widget.NewCheck("Compress shared files", nil)

			// 13. Split
			splitEntry := widget.NewEntry()
			splitEntry.PlaceHolder = "e.g., 10M, 100M, 2G"

			// 14. Encryption Options
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
				infoBar.SetText("Encryption: Protect your archive with AES-256 password encryption.")
			}

			// 15. Buttons
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

		args :=[]string{"x", srcEntry.Text, "-o" + destEntry.Text, "-bsp1", "-y"}
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

	pauseBtn := widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), func() {
		if currentCmd != nil && currentCmd.Process != nil {
			if !isPaused {
				// Send SIGSTOP to pause Linux process
				currentCmd.Process.Signal(syscall.SIGSTOP)
				isPaused = true
				infoBar.SetText("Operation Paused.")
			} else {
				// Send SIGCONT to resume
				currentCmd.Process.Signal(syscall.SIGCONT)
				isPaused = false
				infoBar.SetText("Operation Resumed.")
			}
		}
	})

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		if currentCmd != nil && currentCmd.Process != nil {
			currentCmd.Process.Kill()
			infoBar.SetText("Operation Cancelled.")
		}
	})

	return container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Progress Statistics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
						     progressBar,
					      statusLog,
					      layout.NewSpacer(),
						     container.NewHBox(layout.NewSpacer(), pauseBtn, cancelBtn),
	))
}

// --- LOGIC MAPPER & EXECUTION ---

func build7zArgs(src, format, level, threads string, sfx, enc bool, pass string, encName bool, split string)[]string {
	// Destination filename logic
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	ext := "." + format
	if sfx {
		ext = ".exe"
	}
	dest := filepath.Join(filepath.Dir(src), base+ext)

	// -bsp1 enables progress output to stdout
	args :=[]string{"a", dest, src, "-bsp1"}

	// Map format
	args = append(args, "-t"+format)

	// Map level (-mx0 to -mx9)
	lvlMap := map[string]string{"Store": "0", "Fastest": "1", "Fast": "3", "Normal": "5", "Maximum": "7", "Ultra": "9"}
	args = append(args, "-mx="+lvlMap[level])

	// Map threads
	args = append(args, "-mmt="+threads)

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

func startOperation(args[]string, mode string) {
	currentCmd = exec.Command("7z", args...)

	stdout, err := currentCmd.StdoutPipe()
	if err != nil {
		statusLog.SetText(fmt.Sprintf("Error: %v", err))
		return
	}

	progressBar.SetValue(0)
	isPaused = false
	infoBar.SetText(fmt.Sprintf("%s started...", mode))
	startTime := time.Now()

	err = currentCmd.Start()
	if err != nil {
		statusLog.SetText(fmt.Sprintf("Failed to start 7-Zip: %v", err))
		return
	}

	// Goroutine to parse progress (-bsp1 outputs progress percentages safely)
	go func() {
		reader := bufio.NewReader(stdout)
		re := regexp.MustCompile(`(\d+)%`)

		for {
			line, err := reader.ReadString('\n') // Or parse character stream
			if err != nil {
				if err == io.EOF {
					break
				}
				continue
			}

			// Simple progress parsing regex
			match := re.FindStringSubmatch(line)
			if len(match) > 1 {
				percent, _ := strconv.ParseFloat(match[1], 64)
				progressBar.SetValue(percent / 100.0)

				elapsed := time.Since(startTime).Round(time.Second)
				statusLog.SetText(fmt.Sprintf("Status: Running\nElapsed Time: %s\nProgress: %v%%", elapsed, percent))
			}
		}

		err = currentCmd.Wait()
		if err != nil {
			if err.Error() == "signal: killed" {
				statusLog.SetText("Operation was cancelled by user.")
			} else {
				statusLog.SetText(fmt.Sprintf("Finished with errors: %v", err))
			}
		} else {
			progressBar.SetValue(1.0)
			statusLog.SetText("Operation completed successfully!")
			infoBar.SetText("Ready.")
		}
		currentCmd = nil
	}()
}