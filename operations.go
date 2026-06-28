package main

import (
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

func build7zArgs(src, customName string, format string, level string, method string, dictSize string, wordSize, blockSize string, threads, update string, sfx bool, shared bool, split string, enc bool, pass string, encName bool) []string {

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

	var base string
	if customName != "" {
		base = customName
	} else {
		base = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}

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

func startOperation(args []string, mode string, w fyne.Window, onSuccess func()) {
	fileName := "Unknown"
	if len(args) > 1 {
		fileName = filepath.Base(args[1]) // Get just the filename from path; TODO: doesn't work with checksum command
	}

	stateMu.Lock()
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
	historyList.Refresh()

	currentCmd = exec.Command(root7zCmd, args...)

	stdout, err := currentCmd.StdoutPipe()
	if err != nil {
		setFinalStatus(fmt.Sprintf("Error connecting to output: %v", err))
		return
	}

	// Capture stderr for potential errors
	stderr, err := currentCmd.StderrPipe()
	if err != nil {
		setFinalStatus(fmt.Sprintf("Error connecting to stderr: %v", err))
		return
	}

	// Initialize the console log text for this run
	logMu.Lock()
	if len(logLines) > 0 {
		logLines = append(logLines, "") // Break apart separate runs visually
	}
	commandStr := fmt.Sprintf("Running: %s %s", root7zCmd, strings.Join(args, " "))
	logLines = append(logLines, "========================================", commandStr)
	currentLogLine = currentLogLine[:0]
	logMu.Unlock()
	consoleLog.SetText(strings.Join(logLines, "\n"))

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
		for range ticker.C {
			stateMu.RLock()
			running := isOperationRunning
			paused := isPaused
			pct := currentPercent
			stateMu.RUnlock()

			// Update the UI Log
			logMu.Lock()
			fullLog := strings.Join(logLines, "\n")
			if len(currentLogLine) > 0 {
				fullLog += "\n" + string(currentLogLine)
			}
			logMu.Unlock()
			consoleLog.SetText(fullLog)

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

		// 7-zip relies heavily on \b (backspaces) and \r to rewrite lines visually.
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				b := buf[0]
				// Stream byte to our terminal parser
				processLogByte(b)

				if b == '\r' || b == '\n' || b == '\b' {
					// Safely read the line parsed by processLogByte
					logMu.Lock()
					str := string(currentLogLine)
					logMu.Unlock()

					matches := re.FindStringSubmatch(str)
					if len(matches) > 1 {
						val, _ := strconv.ParseFloat(matches[1], 64)
						stateMu.Lock()
						currentPercent = val
						stateMu.Unlock()
					}
				}
			}
			if readErr != nil {
				break
			}
		}

		err = currentCmd.Wait()

		// Final Log UI Update (catches the very last fragments)
		logMu.Lock()
		fullLog := strings.Join(logLines, "\n")
		if len(currentLogLine) > 0 {
			fullLog += "\n" + string(currentLogLine)
		}
		logMu.Unlock()
		consoleLog.SetText(fullLog)

		// Update History Status
		stateMu.Lock()
		isOperationRunning = false
		currentCmd = nil

		finalStatus := "Completed"
		if err != nil {
			if err.Error() == "signal: killed" {
				finalStatus = "Cancelled"
			} else {
				finalStatus = "Error"
			}
		}
		historyData[entryIndex].Status = finalStatus
		stateMu.Unlock()
		historyList.Refresh()

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

			// Execute the onSuccess callback upon completion, if requested
			if onSuccess != nil {
				onSuccess()
			}
		}
	}()
}
