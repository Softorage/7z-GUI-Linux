package tabs

import (
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	appstate "github.com/Softorage/7z-GUI-Linux/internal/app"
	"github.com/Softorage/7z-GUI-Linux/internal/version"
)

func BuildStatusTab(w fyne.Window) fyne.CanvasObject {
	appstate.StatusLog = widget.NewLabel("No operations running.")
	appstate.StatusLog.Wrapping = fyne.TextWrapWord

	// Initialize Progress Bar
	appstate.ProgressBar = widget.NewProgressBar()
	appstate.ProgressBar.Min = 0.0
	appstate.ProgressBar.Max = 1.0
	appstate.ProgressBar.SetValue(0.0)

	// Initialize the History List
	appstate.HistoryList = widget.NewList(
		func() int {
			return len(appstate.HistoryData)
		},
		func() fyne.CanvasObject {
			// Template for each row: [Time] Operation: FileName - Status
			return container.NewHBox(
				widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}), // Time
				widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),      // Type
				widget.NewLabel(""), // File
				layout.NewSpacer(),
				widget.NewLabel(""), // Status
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			data := appstate.HistoryData[id]
			objs := item.(*fyne.Container).Objects
			objs[0].(*widget.Label).SetText("[" + data.Timestamp + "]")
			objs[1].(*widget.Label).SetText(data.OpType + ":")
			objs[2].(*widget.Label).SetText(data.File)

			statusLbl := objs[4].(*widget.Label)
			statusLbl.SetText(data.Status)

			// Color coding status
			if data.Status == "Completed" || data.Status == "Ready" {
				statusLbl.Importance = widget.SuccessImportance
			} else if data.Status == "Error" || data.Status == "Cancelled" {
				statusLbl.Importance = widget.DangerImportance
			} else {
				statusLbl.Importance = widget.WarningImportance
			}
			statusLbl.Refresh()
		},
	)

	appstate.PauseBtn = widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), func() {
		appstate.StateMu.Lock()
		defer appstate.StateMu.Unlock()
		if appstate.CurrentCmd != nil && appstate.CurrentCmd.Process != nil {
			if !appstate.IsPaused {
				// Send SIGSTOP to pause Linux process
				appstate.CurrentCmd.Process.Signal(syscall.SIGSTOP)
				appstate.IsPaused = true
				appstate.PauseBtn.SetText("Resume")
				appstate.PauseBtn.SetIcon(theme.MediaPlayIcon())
				appstate.SetInfo("Operation Paused.")
				appstate.StatusLog.SetText("Status: Paused")
			} else {
				// Send SIGCONT to resume
				appstate.CurrentCmd.Process.Signal(syscall.SIGCONT)
				appstate.IsPaused = false
				appstate.PauseBtn.SetText("Pause")
				appstate.PauseBtn.SetIcon(theme.MediaPauseIcon())
				appstate.SetInfo("Operation Resumed.")
			}
		}
	})
	appstate.PauseBtn.Disable()

	appstate.CancelBtn = widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		appstate.CancelMu.Lock()
		if appstate.CurrentCancel != nil {
			appstate.CurrentCancel() // Clean execution cancellation via context
			appstate.SetInfo("Operation cancelled by user context request.")
		}
		appstate.CancelMu.Unlock()
	})
	appstate.CancelBtn.Disable()

	// Top section: Current Status
	currentStatus := container.NewVBox(
		widget.NewLabelWithStyle("Current Progress", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		appstate.ProgressBar,
		appstate.StatusLog,
		container.NewHBox(layout.NewSpacer(), appstate.PauseBtn, appstate.CancelBtn),
		widget.NewSeparator(),
	)

	// Bottom Section: History and Log Tabs
	historySection := container.NewPadded(appstate.HistoryList)

	// Initialize the Console Log Text Box
	appstate.ConsoleLog = widget.NewMultiLineEntry()
	appstate.ConsoleLog.Wrapping = fyne.TextWrapWord
	appstate.ConsoleLog.TextStyle = fyne.TextStyle{Monospace: true}
	// Note: We leave it enabled so users can select and copy the text

	appstate.ConsoleLog.PlaceHolder = "7GL Console Initialized\n--------------------------------------------" + version.SponsorEditionText + "\n> Waiting for process output..."

	logSection := container.NewPadded(appstate.ConsoleLog)

	bottomTabs := container.NewPadded(container.NewAppTabs(
		container.NewTabItem("Operation History", historySection),
		container.NewTabItem("Log", logSection),
	))

	// Use Border layout to ensure the 'split' container expands to fill the window
	return container.NewPadded(
		container.NewBorder(
			container.NewVBox(
				widget.NewRichTextFromMarkdown("## Status"),
				widget.NewSeparator(),
				currentStatus,
			),
			nil,
			nil,
			nil,
			bottomTabs,
		),
	)
}