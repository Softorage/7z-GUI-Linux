package main

import (
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func buildStatusTab(w fyne.Window) fyne.CanvasObject {
	statusLog = widget.NewLabel("No operations running.")
	statusLog.Wrapping = fyne.TextWrapWord

	// Initialize Progress Bar
	progressBar = widget.NewProgressBar()
	progressBar.Min = 0.0
	progressBar.Max = 1.0
	progressBar.SetValue(0.0)

	// Initialize the History List
	historyList = widget.NewList(
		func() int {
			return len(historyData)
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
			data := historyData[id]
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
		cancelMu.Lock()
		if currentCancel != nil {
			currentCancel() // Clean execution cancellation via context
			setInfo("Operation cancelled by user context request.")
		}
		cancelMu.Unlock()
	})
	cancelBtn.Disable()

	// Top section: Current Status
	currentStatus := container.NewVBox(
		widget.NewLabelWithStyle("Current Progress", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		progressBar,
		statusLog,
		container.NewHBox(layout.NewSpacer(), pauseBtn, cancelBtn),
		widget.NewSeparator(),
	)

	// Bottom Section: History and Log Tabs
	historySection := container.NewPadded(historyList)

	// Initialize the Console Log Text Box
	consoleLog = widget.NewMultiLineEntry()
	consoleLog.Wrapping = fyne.TextWrapWord
	consoleLog.TextStyle = fyne.TextStyle{Monospace: true}
	// Note: We leave it enabled so users can select and copy the text

	consoleLog.PlaceHolder = "7GL Console Initialized\n--------------------------------------------\n> Waiting for process output..."

	logSection := container.NewPadded(consoleLog)

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
