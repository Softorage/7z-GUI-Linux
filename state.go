package main

import (
	"os/exec"
	"sync"
	"time"

	"fyne.io/fyne/v2/widget"
)

type operationLog struct {
	ID        int
	File      string
	OpType    string // e.g., "Extracting", "Compressing"
	Status    string // e.g., "In Progress", "Completed", "Failed"
	Timestamp string
}

var (
	infoBar     *widget.Label
	tabs        *widget.List
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

	// Operations History
	historyData []operationLog
	historyList *widget.List

	// Console Log State
	logMu          sync.Mutex
	logLines       []string
	currentLogLine []byte
	logCursor      int // Emulates a terminal cursor position to prevent flickering
	consoleLog     *widget.Entry
)

var root7zCmd string = "7z"

// version is passed at build time
var version string = "dev"

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
			infoBar.SetText("Ready. Interact with an option to see its description.")
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

// processLogByte builds the console log stream exactly as a terminal does
func processLogByte(b byte) {
	logMu.Lock()
	defer logMu.Unlock()

	if b == '\n' { // New line
		logLines = append(logLines, string(currentLogLine))
		currentLogLine = currentLogLine[:0]
		logCursor = 0 // Reset cursor for the new line
	} else if b == '\r' { // Carriage return
		// Instead of clearing the slice (which causes UI flickering),
		// we just move the cursor back to the start.
		// Upcoming characters will overwrite the existing ones.
		logCursor = 0
	} else if b == '\b' { // Backspace
		if logCursor > 0 {
			logCursor--
		}
	} else { // Standard character
		if logCursor < len(currentLogLine) {
			currentLogLine[logCursor] = b
		} else {
			currentLogLine = append(currentLogLine, b)
		}
		logCursor++
	}
}
