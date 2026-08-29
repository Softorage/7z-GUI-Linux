package app

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

const (
	MaxLogLines       = 1000
	LogBatchTrimSize  = 200
	MaxLogLineLength  = 4096
	MaxHistoryRecords = 100
)

var (
	InfoBar     *widget.Label
	Tabs        *widget.List
	StatusLog   *widget.Label
	ProgressBar *widget.ProgressBar
	PauseBtn    *widget.Button
	CancelBtn   *widget.Button

	CurrentCmd         *exec.Cmd
	IsPaused           bool
	IsOperationRunning bool
	CurrentPercent     float64
	StateMu            sync.RWMutex

	// Timers and mutexes to auto-clear UI text after 6 seconds
	InfoBarTimer *time.Timer
	InfoMu       sync.Mutex

	StatusTimer *time.Timer
	StatusMu    sync.Mutex

	// Operations History
	HistoryData []domain.OperationLog
	HistoryList *widget.List

	// Console Log State
	LogMu           sync.Mutex
	LogLines        []string
	CurrentLogLine  []byte
	LogCursor       int
	ConsoleLog      *widget.Entry
	LastLogText     string
	LogTextMu       sync.Mutex
	LogGeneration   uint64
	LastRenderedGen uint64

	CurrentCancel context.CancelFunc
	CancelMu      sync.Mutex
)

// TODO: this needs to add the string (with timestamp) to logs. account for any operation that may be running so as to not mix these with operation's logs. change the timer to 3sec once logging is implemented.
// TODO: if multiple setinfo are called when earlier is still there, then they should stack.
// setInfo updates the bottom info bar and sets a 6-second timer to clear it.
func SetInfo(text string) {
	InfoMu.Lock()
	defer InfoMu.Unlock()

	fyne.Do(func() {
		if InfoBar != nil {
			InfoBar.SetText(text)
		}
	})

	if InfoBarTimer != nil {
		InfoBarTimer.Stop()
	}
	InfoBarTimer = time.AfterFunc(6*time.Second, func() {
		StateMu.RLock()
		running := IsOperationRunning
		paused := IsPaused
		StateMu.RUnlock()

		fyne.Do(func() {
			if InfoBar != nil {
				if running {
					if paused {
						InfoBar.SetText("Operation paused.")
					} else {
						InfoBar.SetText("Operation in progress...")
					}
				} else {
					InfoBar.SetText("Ready. Interact with an option to see its description.")
				}
			}
		})
	})
}

// setFinalStatus updates the status log when an operation ends, clearing it after 6 seconds.
func SetFinalStatus(text string) {
	StatusMu.Lock()
	defer StatusMu.Unlock()

	fyne.Do(func() {
		if StatusLog != nil {
			StatusLog.SetText(text)
		}
	})

	if StatusTimer != nil {
		StatusTimer.Stop()
	}
	StatusTimer = time.AfterFunc(6*time.Second, func() {
		StateMu.RLock()
		running := IsOperationRunning
		StateMu.RUnlock()
		if !running {
			fyne.Do(func() {
				if StatusLog != nil {
					StatusLog.SetText("No operations running.")
				}
				if ProgressBar != nil {
					ProgressBar.SetValue(0)
				}
			})
		}
	})
}
