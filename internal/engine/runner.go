package engine

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

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

// updateConsoleLog updates the log text and positions the entry cursor at the very row, forcing Fyne's native scroll viewport to go to the bottom.
func UpdateConsoleLog(text string) {
	appstate.LogTextMu.Lock()
	if text == appstate.LastLogText {
		appstate.LogTextMu.Unlock()
		return
	}
	appstate.LastLogText = text
	appstate.LogTextMu.Unlock()

	// Safely queue UI update on Fyne's main event thread
	fyne.Do(func() {
		if appstate.ConsoleLog != nil {
			appstate.ConsoleLog.SetText(text)
			lines := strings.Split(text, "\n")
			if len(lines) > 0 {
				appstate.ConsoleLog.CursorRow = len(lines) - 1
			}
			appstate.ConsoleLog.Refresh()
		}
	})
}

// StartOperation executes a 7-Zip command with context-based cancellation and custom working directories.
func StartOperation(args []string, mode string, workingDir string, w fyne.Window, onSuccess func()) {
	fileName := "Unknown"
	if len(args) > 1 {
		fileName = filepath.Base(args[1]) // Get just the filename from path;
		// TODO: doesn't work well with checksum command
	}

	appstate.StateMu.Lock()
	// Prevent launching a new operation if one is already running
	if appstate.IsOperationRunning {
		appstate.StateMu.Unlock()
		return
	}

	logEntry := domain.OperationLog{
		ID:        len(appstate.HistoryData),
		File:      fileName,
		OpType:    mode,
		Status:    "Running...",
		Timestamp: time.Now().Format("15:04:05"),
	}
	appstate.HistoryData = append([]domain.OperationLog{logEntry}, appstate.HistoryData...) // Prepend to show newest first
	entryIndex := 0                                                // Since we prepend, it's always at the top
	appstate.StateMu.Unlock()

	fyne.Do(func() {
		if appstate.HistoryList != nil {
			appstate.HistoryList.Refresh()
		}
	})

	// Setup context for safe and immediate process termination
	appstate.CancelMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	appstate.CurrentCancel = cancel
	appstate.CancelMu.Unlock()

	// Use CommandContext instead of raw Command to handle system-level process group cleanups
	cmd := exec.CommandContext(ctx, Root7zCmd, args...)
	if workingDir != "" {
		cmd.Dir = workingDir // Prevent absolute folder tree nesting
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		fyne.Do(func() {
			appstate.SetFinalStatus(fmt.Sprintf("Error connecting to output: %v", err))
		})
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		fyne.Do(func() {
			appstate.SetFinalStatus(fmt.Sprintf("Error connecting to stderr: %v", err))
		})
		return
	}

	// It is now safe to share cmd globally
	appstate.StateMu.Lock()
	appstate.CurrentCmd = cmd
	appstate.StateMu.Unlock()

	// Initialize the console log text for this run
	appstate.LogMu.Lock()
	if len(appstate.LogLines) > 0 {
		appstate.LogLines = append(appstate.LogLines, "") // Break apart separate runs visually
	}
	commandStr := fmt.Sprintf("Running: %s %s", Root7zCmd, strings.Join(args, " "))
	if workingDir != "" {
		commandStr += fmt.Sprintf(" (Dir: %s)", workingDir)
	}
	appstate.LogLines = append(appstate.LogLines, "========================================", commandStr)
	appstate.CurrentLogLine = appstate.CurrentLogLine[:0]
	appstate.LogMu.Unlock()
	UpdateConsoleLog(GetFullLogText())

	// Lock UI functionality safely
	appstate.StateMu.Lock()
	appstate.IsOperationRunning = true
	appstate.IsPaused = false
	appstate.CurrentPercent = 0
	appstate.StateMu.Unlock()

	fyne.Do(func() {
		if appstate.ProgressBar != nil {
			appstate.ProgressBar.SetValue(0.0)
		}
		// Set buttons active
		if appstate.PauseBtn != nil {
			appstate.PauseBtn.SetText("Pause")
			appstate.PauseBtn.SetIcon(theme.MediaPauseIcon())
			appstate.PauseBtn.Enable()
		}
		if appstate.CancelBtn != nil {
			appstate.CancelBtn.Enable()
		}
		appstate.SetInfo(fmt.Sprintf("%s started...", mode))
	})
	startTime := time.Now()

	err = cmd.Start()
	if err != nil {
		cancel()
		appstate.StateMu.Lock()
		appstate.IsOperationRunning = false
		appstate.CurrentCmd = nil
		appstate.StateMu.Unlock()
		fyne.Do(func() {
			if appstate.PauseBtn != nil {
				appstate.PauseBtn.Disable()
			}
			if appstate.CancelBtn != nil {
				appstate.CancelBtn.Disable()
			}
			appstate.SetFinalStatus(fmt.Sprintf("Failed to start 7-Zip: %v", err))
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
				ProcessLogByte(buf[0])
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
				appstate.StateMu.RLock()
				running := appstate.IsOperationRunning
				paused := appstate.IsPaused
				appstate.StateMu.RUnlock()

				// Update the UI Log
				UpdateConsoleLog(GetFullLogText())

				if !running {
					return
				}
				if paused {
					continue
				}

				// Safely read the current log line to extract the latest percentage marker
				appstate.LogMu.Lock()
				activeLine := string(appstate.CurrentLogLine)
				appstate.LogMu.Unlock()

				// Parse progress dynamically on the 1-second interval
				matches := re.FindStringSubmatch(activeLine)
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					appstate.StateMu.Lock()
					appstate.CurrentPercent = val
					appstate.StateMu.Unlock()
				}

				appstate.StateMu.RLock()
				pct := appstate.CurrentPercent
				appstate.StateMu.RUnlock()

				// Refresh Fyne widgets
				fyne.Do(func() {
					if appstate.ProgressBar != nil {
						appstate.ProgressBar.SetValue(pct / 100.0)
					}
					elapsed := time.Since(startTime).Round(time.Second)
					if appstate.StatusLog != nil {
						appstate.StatusLog.SetText(fmt.Sprintf("Status: Running\nElapsed Time: %s\n", elapsed))
					}
				})
			case <-doneChan:
				// Ensure final state is drawn before routine exits
				UpdateConsoleLog(GetFullLogText())
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
				ProcessLogByte(buf[0])
			}
			if readErr != nil {
				break
			}
		}

		err = cmd.Wait()

		// Final Log UI Update (catches the very last fragments)
		UpdateConsoleLog(GetFullLogText())

		// Update History Status and reset operation states in a single atomic lock
		appstate.StateMu.Lock()
		appstate.IsOperationRunning = false
		appstate.CurrentCmd = nil
		appstate.StateMu.Unlock()

		appstate.CancelMu.Lock()
		cancel() // Clean up context resources
		appstate.CancelMu.Unlock()

		finalStatus := "Completed"
		if err != nil {
			if ctx.Err() == context.Canceled || err.Error() == "signal: killed" {
				finalStatus = "Cancelled"
			} else {
				finalStatus = "Error"
			}
		}
		appstate.StateMu.Lock()
		if len(appstate.HistoryData) > entryIndex {
			appstate.HistoryData[entryIndex].Status = finalStatus
		}
		appstate.StateMu.Unlock()

		fyne.Do(func() {
			if appstate.HistoryList != nil {
				appstate.HistoryList.Refresh()
			}
			if appstate.PauseBtn != nil {
				appstate.PauseBtn.Disable()
			}
			if appstate.CancelBtn != nil {
				appstate.CancelBtn.Disable()
			}

			if err != nil {
				if appstate.ProgressBar != nil {
					appstate.ProgressBar.SetValue(0.0)
				}
				if ctx.Err() == context.Canceled || err.Error() == "signal: killed" {
					appstate.SetFinalStatus("Operation was cancelled by user.")
					appstate.SetInfo("Cancelled.")
				} else {
					appstate.SetFinalStatus(fmt.Sprintf("Finished with errors: %v", err))
					appstate.SetInfo("Error during operation.")
				}
			} else {
				if appstate.ProgressBar != nil {
					appstate.ProgressBar.SetValue(1.0)
				}
				appstate.SetFinalStatus("Operation completed successfully!")
				appstate.SetInfo("Done.")

				// Execute the onSuccess callback upon completion, if requested
				if onSuccess != nil {
					onSuccess()
				}
			}
		})
	}()
}

// processLogByte builds the console log stream exactly as a terminal does
func ProcessLogByte(b byte) {
	appstate.LogMu.Lock()
	defer appstate.LogMu.Unlock()

	switch b {
	case '\n': // New line
		appstate.LogLines = append(appstate.LogLines, string(appstate.CurrentLogLine))
		appstate.CurrentLogLine = appstate.CurrentLogLine[:0]
		appstate.LogCursor = 0 // Reset cursor for the new line
	case '\r': // Carriage return
		// Instead of clearing the slice (which causes UI flickering),
		// we just move the cursor back to the start.
		// Upcoming characters will overwrite the existing ones.
		appstate.LogCursor = 0 // Return to beginning of line (7-Zip progress bars use this)
	case '\b': // Backspace
		if appstate.LogCursor > 0 {
			appstate.LogCursor--
		}
	default: // Standard character
		if appstate.LogCursor < len(appstate.CurrentLogLine) {
			appstate.CurrentLogLine[appstate.LogCursor] = b
		} else {
			appstate.CurrentLogLine = append(appstate.CurrentLogLine, b)
		}
		appstate.LogCursor++
	}
}

// getLogLines returns a thread-safe copy of all completed log lines plus the active line.
func GetLogLines() []string {
	appstate.LogMu.Lock()
	defer appstate.LogMu.Unlock()

	lines := make([]string, len(appstate.LogLines), len(appstate.LogLines)+1)
	copy(lines, appstate.LogLines)
	if len(appstate.CurrentLogLine) > 0 {
		lines = append(lines, string(appstate.CurrentLogLine))
	}
	return lines
}

// getFullLogText returns the assembled console logs thread-safely.
func GetFullLogText() string {
	return strings.Join(GetLogLines(), "\n")
}