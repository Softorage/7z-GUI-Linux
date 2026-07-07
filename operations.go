package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// updateConsoleLog updates the log text and positions the entry cursor at the very row, forcing Fyne's native scroll viewport to go to the bottom.
func updateConsoleLog(text string) {
	logTextMu.Lock()
	if text == lastLogText {
		logTextMu.Unlock()
		return
	}
	lastLogText = text
	logTextMu.Unlock()

	// Safely queue UI update on Fyne's main event thread
	fyne.Do(func() {
		consoleLog.SetText(text)
		lines := strings.Split(text, "\n")
		if len(lines) > 0 {
			consoleLog.CursorRow = len(lines) - 1
		}
		consoleLog.Refresh()
	})
}

// getArchiveDestination calculates the full target path for the archive.
// Used in ui_compress and the build7zArgs arguments.
func getArchiveDestination(sources []string, format string, customName string, sfx bool) string {
	if len(sources) == 0 {
		return ""
	}

	extMap := map[string]string{
		"7z":    ".7z",
		"xz":    ".xz",
		"bzip2": ".bz2",
		"gzip":  ".gz",
		"tar":   ".tar",
		"zip":   ".zip",
		"wim":   ".wim",
	}

	ext, ok := extMap[format]
	if !ok {
		ext = "." + format
	}

	// Only 7z truly supports SFX cleanly via standard 7-Zip module
	if sfx && format == "7z" {
		ext = ".exe"
	}

	firstSrc := sources[0]
	dir := filepath.Dir(firstSrc)

	var filename string
	if customName != "" {
		filename = customName
		// Ensure custom name has correct extension if not already present
		if !strings.HasSuffix(strings.ToLower(filename), ext) {
			filename += ext
		}
	} else {
		var base string
		if len(sources) == 1 {
			base = strings.TrimSuffix(filepath.Base(firstSrc), filepath.Ext(firstSrc))
		} else {
			// If packaging multiple items, standard UI logic sets the parent directory's folder name as default
			base = filepath.Base(dir)
			if base == "." || base == "/" || base == string(filepath.Separator) {
				base = "archive"
			}
		}
		filename = base + ext
	}

	return filepath.Join(dir, filename)
}

func build7zArgs(src []string, customName string, format string, level string, method string, dictSize string, wordSize, blockSize string, threads, update string, sfx bool, shared bool, split string, enc bool, pass string, encName bool) []string {

	// Call unified helper to get the target destination
	dest := getArchiveDestination(src, format, customName, sfx)

	// Determine if the format supports multi-file archiving/updating features
	updatableArchiveFormat := format != "tar" && format != "gzip" && format != "bzip2" && format != "xz"

	// Determine command line action (a = Add, u = Update)
	cmdAction := "a"
	var updateSwitches []string

	if updatableArchiveFormat {
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

	// Build base parameters: command first, then output path, then all sources dynamically
	args := []string{cmdAction, dest}
	args = append(args, src...)
	args = append(args, "-bsp1", "-t"+format)
	args = append(args, updateSwitches...)

	// Only apply compression settings if the format supports it (tar does not)
	if format != "tar" {
		lvlMap := map[string]string{"Store": "0", "Fastest": "1", "Fast": "3", "Normal": "5", "Maximum": "7", "Ultra": "9"}
		args = append(args, "-mx="+lvlMap[level])

		// Only apply Method, Dictionary, and Word Size if we are compressing
		if level != "Store" {
			// Apply Compression Method
			if method != "" {
				if format == "zip" {
					args = append(args, "-mm="+method)
				} else if format == "7z" || format == "wim" {
					// 7z uses -m0 switch to assign a generic method
					args = append(args, "-m0="+method)
				}
			}

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
	if split != "" && updatableArchiveFormat {
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

// startOperation executes a 7-Zip command with context-based cancellation and custom working directories.
func startOperation(args []string, mode string, workingDir string, w fyne.Window, onSuccess func()) {
	fileName := "Unknown"
	if len(args) > 1 {
		fileName = filepath.Base(args[1]) // Get just the filename from path;
		// TODO: doesn't work well with checksum command
	}

	stateMu.Lock()
	// Prevent launching a new operation if one is already running
	if isOperationRunning {
		stateMu.Unlock()
		return
	}

	logEntry := operationLog{
		ID:        len(historyData),
		File:      fileName,
		OpType:    mode,
		Status:    "Running...",
		Timestamp: time.Now().Format("15:04:05"),
	}
	historyData = append([]operationLog{logEntry}, historyData...) // Prepend to show newest first
	entryIndex := 0                                                // Since we prepend, it's always at the top
	stateMu.Unlock()

	fyne.Do(func() {
		historyList.Refresh()
	})

	// Setup context for safe and immediate process termination
	cancelMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	currentCancel = cancel
	cancelMu.Unlock()

	// Use CommandContext instead of raw Command to handle system-level process group cleanups
	cmd := exec.CommandContext(ctx, root7zCmd, args...)
	if workingDir != "" {
		cmd.Dir = workingDir // Prevent absolute folder tree nesting
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		fyne.Do(func() {
			setFinalStatus(fmt.Sprintf("Error connecting to output: %v", err))
		})
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		fyne.Do(func() {
			setFinalStatus(fmt.Sprintf("Error connecting to stderr: %v", err))
		})
		return
	}

	// It is now safe to share cmd globally
	stateMu.Lock()
	currentCmd = cmd
	stateMu.Unlock()

	// Initialize the console log text for this run
	logMu.Lock()
	if len(logLines) > 0 {
		logLines = append(logLines, "") // Break apart separate runs visually
	}
	commandStr := fmt.Sprintf("Running: %s %s", root7zCmd, strings.Join(args, " "))
	if workingDir != "" {
		commandStr += fmt.Sprintf(" (Dir: %s)", workingDir)
	}
	logLines = append(logLines, "========================================", commandStr)
	currentLogLine = currentLogLine[:0]
	logMu.Unlock()
	updateConsoleLog(getFullLogText())

	// Lock UI functionality safely
	stateMu.Lock()
	isOperationRunning = true
	isPaused = false
	currentPercent = 0
	stateMu.Unlock()

	fyne.Do(func() {
		progressBar.SetValue(0.0)
		// Set buttons active
		pauseBtn.SetText("Pause")
		pauseBtn.SetIcon(theme.MediaPauseIcon())
		pauseBtn.Enable()
		cancelBtn.Enable()
		setInfo(fmt.Sprintf("%s started...", mode))
	})
	startTime := time.Now()

	err = cmd.Start()
	if err != nil {
		cancel()
		stateMu.Lock()
		isOperationRunning = false
		currentCmd = nil
		stateMu.Unlock()
		fyne.Do(func() {
			pauseBtn.Disable()
			cancelBtn.Disable()
			setFinalStatus(fmt.Sprintf("Failed to start 7-Zip: %v", err))
		})
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	doneChan := make(chan struct{}) // Used to cleanly teardown UI routine

	// Stderr Reader Routine
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				processLogByte(buf[0])
			}
			if err != nil {
				break
			}
		}
	}()

	// UI Update Routine (Once per second)
	go func() {
		re := regexp.MustCompile(`(\d+)%`)
		for {
			select {
			case <-ticker.C:
				stateMu.RLock()
				running := isOperationRunning
				paused := isPaused
				stateMu.RUnlock()

				// Update the UI Log
				updateConsoleLog(getFullLogText())

				if !running {
					return
				}
				if paused {
					continue
				}

				// Safely read the current log line to extract the latest percentage marker
				logMu.Lock()
				activeLine := string(currentLogLine)
				logMu.Unlock()

				// Parse progress dynamically on the 1-second interval
				matches := re.FindStringSubmatch(activeLine)
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					stateMu.Lock()
					currentPercent = val
					stateMu.Unlock()
				}

				stateMu.RLock()
				pct := currentPercent
				stateMu.RUnlock()

				// Refresh Fyne widgets
				fyne.Do(func() {
					progressBar.SetValue(pct / 100.0)
					elapsed := time.Since(startTime).Round(time.Second)
					statusLog.SetText(fmt.Sprintf("Status: Running\nElapsed Time: %s\n", elapsed))
				})
			case <-doneChan:
				// Ensure final state is drawn before routine exits
				updateConsoleLog(getFullLogText())
				return
			}
		}
	}()

	// Sub-process I/O Reader Routine (Populates log bytes only; decoupled from math operations)
	go func() {
		defer ticker.Stop()
		defer close(doneChan) // Terminate UI update ticker routine safely
		buf := make([]byte, 1)

		// 7-zip relies heavily on \b (backspaces) and \r to rewrite lines visually.
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				processLogByte(buf[0])
			}
			if readErr != nil {
				break
			}
		}

		err = cmd.Wait()

		// Final Log UI Update (catches the very last fragments)
		updateConsoleLog(getFullLogText())

		// Update History Status and reset operation states in a single atomic lock
		stateMu.Lock()
		isOperationRunning = false
		currentCmd = nil
		stateMu.Unlock()

		cancelMu.Lock()
		cancel() // Clean up context resources
		cancelMu.Unlock()

		finalStatus := "Completed"
		if err != nil {
			if ctx.Err() == context.Canceled || err.Error() == "signal: killed" {
				finalStatus = "Cancelled"
			} else {
				finalStatus = "Error"
			}
		}
		stateMu.Lock()
		if len(historyData) > entryIndex {
			historyData[entryIndex].Status = finalStatus
		}
		stateMu.Unlock()

		fyne.Do(func() {
			historyList.Refresh()
			pauseBtn.Disable()
			cancelBtn.Disable()

			if err != nil {
				progressBar.SetValue(0.0)
				if ctx.Err() == context.Canceled || err.Error() == "signal: killed" {
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

				// Execute the onSuccess callback upon completion, if requested
				if onSuccess != nil {
					onSuccess()
				}
			}
		})
	}()
}

// truncateDisplayPath truncates a string with leading ellipsis if it exceeds maxLen
func truncateDisplayPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	if maxLen <= 3 {
		return path
	}
	return "..." + path[len(path)-(maxLen-3):]
}

// isArchiveExtension returns true if the given path has a supported archive extension.
func isArchiveExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".7z" || ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".bz2" || ext == ".xz" || ext == ".wim" || ext == ".rar"
}

// isSingleFileArchive returns true if the archive format can only pack a single file directly.
// TODO: Same logic as isSingleStream in ui_compress. Consider DRYing it.
func isSingleFileArchive(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".gz" || ext == ".bz2" || ext == ".xz"
}
